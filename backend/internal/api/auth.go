package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/metalhops/metalhops/backend/internal/auth"
)

type Authenticator interface {
	Authenticate(context.Context, string) (auth.Caller, error)
}

type callerKey struct{}

func CallerFrom(ctx context.Context) (auth.Caller, bool) {
	caller, ok := ctx.Value(callerKey{}).(auth.Caller)
	return caller, ok
}

func requireAuth(authenticator Authenticator, logger *slog.Logger, next http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := requestID(r.Context())
		values := r.Header.Values("Authorization")
		if len(values) != 1 || authenticator == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.", id, nil)
			return
		}
		scheme, token, ok := strings.Cut(values[0], " ")
		if !ok || scheme != "Bearer" || token == "" || strings.Contains(token, " ") {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.", id, nil)
			return
		}
		caller, err := authenticator.Authenticate(r.Context(), token)
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrUnauthorized):
				WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.", id, nil)
			case errors.Is(err, auth.ErrProvider):
				logger.Error("authentication", "request_id", id, "err", err)
				WriteError(w, http.StatusBadGateway, "auth_provider_error", "The authentication provider is unavailable.", id, nil)
			default:
				logger.Error("authentication", "request_id", id, "err", err)
				WriteError(w, http.StatusInternalServerError, "internal_error", "The server could not complete the request.", id, nil)
			}
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, caller)))
	})
}
