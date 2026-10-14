package api

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeClosesPoolWhenListenFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	closed := false
	srv := &http.Server{Addr: ln.Addr().String(), Handler: http.NewServeMux()}
	err = Serve(context.Background(), srv, time.Second, func() { closed = true })
	if err == nil || !closed {
		t.Fatalf("err %v closed %v", err, closed)
	}
}

func TestServeShutdownTimeoutClosesPool(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	started := make(chan struct{})
	release := make(chan struct{})
	srv := &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			close(started)
			<-release
		}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	closed := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, srv, 50*time.Millisecond, func() { close(closed) })
	}()

	deadline := time.Now().Add(2 * time.Second)
	var conn net.Conn
	for {
		conn, err = net.Dial("tcp", addr)
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start")
	}
	cancel()
	select {
	case err = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return")
	}
	select {
	case <-closed:
	default:
		t.Fatal("pool was not closed")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err %v", err)
	}
	close(release)
}
