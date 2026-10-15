package profile

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdminDeleteUser(t *testing.T) {
	const key = "service-secret"
	const userID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	var method, uri, apiKey, authz string
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		uri = r.URL.RequestURI()
		apiKey = r.Header.Get("apikey")
		authz = r.Header.Get("Authorization")
		var err error
		body, err = io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	client := NewAdminClient(server.URL, key, server.Client())
	if err := client.DeleteUser(context.Background(), userID); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodDelete || len(body) != 0 {
		t.Fatalf("method %s body %q", method, body)
	}
	if uri != "/auth/v1/admin/users/"+userID+"?should_soft_delete=false" {
		t.Fatalf("uri %s", uri)
	}
	if apiKey != key || authz != "Bearer "+key {
		t.Fatalf("api %q auth %q", apiKey, authz)
	}

	for _, status := range []int{http.StatusOK, http.StatusNoContent} {
		statusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		err := NewAdminClient(statusServer.URL, key, statusServer.Client()).DeleteUser(context.Background(), userID)
		statusServer.Close()
		if err != nil {
			t.Fatalf("status %d %v", status, err)
		}
	}

	prefixed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uri = r.URL.RequestURI()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(prefixed.Close)
	if err := NewAdminClient(prefixed.URL+"/prefix", key, prefixed.Client()).DeleteUser(context.Background(), "a/b"); err != nil {
		t.Fatal(err)
	}
	if uri != "/prefix/auth/v1/admin/users/a%2Fb?should_soft_delete=false" {
		t.Fatalf("prefixed %s", uri)
	}

	secretBody := "provider-secret-body"
	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, secretBody, http.StatusBadGateway)
	}))
	t.Cleanup(failed.Close)
	err := NewAdminClient(failed.URL, key, failed.Client()).DeleteUser(context.Background(), userID)
	if err == nil || strings.Contains(err.Error(), key) || strings.Contains(err.Error(), secretBody) {
		t.Fatalf("upstream err %v", err)
	}

	blocked := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(blocked.Close)
	slow := blocked.Client()
	slow.Timeout = 20 * time.Millisecond
	err = NewAdminClient(blocked.URL, key, slow).DeleteUser(context.Background(), userID)
	if err == nil || strings.Contains(err.Error(), key) || strings.Contains(err.Error(), secretBody) {
		t.Fatalf("timeout err %v", err)
	}
}
