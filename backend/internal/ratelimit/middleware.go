package ratelimit

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type limitedBody struct {
	Error limitedError `json:"error"`
}

type limitedError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func Middleware(l *Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && (r.URL.Path == "/healthz" || r.URL.Path == "/readyz") {
				next.ServeHTTP(w, r)
				return
			}
			retryAfter, ok := l.allow(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
			if ok {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(limitedBody{Error: limitedError{
				Code: "rate_limited", Message: "Too many requests.", RequestID: w.Header().Get("X-Request-ID"),
			}})
		})
	}
}
