package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// Serve runs srv until ctx is canceled or the server fails to start, then
// drains in-flight requests for up to timeout before closing the database
// pool via closePool. closePool always runs before Serve returns.
func Serve(ctx context.Context, srv *http.Server, version string, timeout time.Duration, closePool func()) error {
	listener, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		closePool()
		return err
	}
	address := srv.Addr
	if strings.HasPrefix(address, ":") {
		address = "localhost" + address
	}
	fmt.Fprint(os.Stdout, "🤘 MetalHops backend\n"+version+"\nLive at http://"+address+" — crank it up!\n")

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
	err = srv.Shutdown(shutdownCtx)
	closePool()
	return err
}
