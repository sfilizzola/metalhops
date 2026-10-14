package api

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Serve runs srv until ctx is canceled or the server fails to start, then
// drains in-flight requests for up to timeout before closing the database
// pool via closePool. closePool always runs before Serve returns.
func Serve(ctx context.Context, srv *http.Server, timeout time.Duration, closePool func()) error {
	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		closePool()
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := srv.Shutdown(shutdownCtx)
	closePool()
	return err
}
