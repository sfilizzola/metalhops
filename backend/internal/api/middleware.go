package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

type contextKey struct{}

var readRandom = rand.Read

func wrap(logger *slog.Logger, limit func(http.Handler) http.Handler, next http.Handler) http.Handler {
	return accessLog(logger, requestIDs(recoverPanic(logger, limit(next))))
}

func accessLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		status := sw.status
		if status == 0 {
			status = http.StatusOK
		}
		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", sw.Header().Get("X-Request-ID"),
		)
	})
}

func requestID(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}

func requestIDs(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := newRequestID(readRandom)
		if err != nil {
			w.Header().Set("X-Request-ID", "unavailable")
			WriteError(w, http.StatusInternalServerError, "internal_error", "The server could not complete the request.", "unavailable", nil)
			return
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, id)))
	})
}

func newRequestID(read func([]byte) (int, error)) (string, error) {
	buf := make([]byte, 16)
	if _, err := read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func recoverPanic(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			id, _ := r.Context().Value(contextKey{}).(string)
			logger.Error("panic", "request_id", id, "err", rec, "stack", string(debug.Stack()))
			WriteError(w, http.StatusInternalServerError, "internal_error", "The server could not complete the request.", id, nil)
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}
