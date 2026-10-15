package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJWKSCache(t *testing.T) {
	oldKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	newKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	var fetches atomic.Int32
	var status int
	var body any
	status = http.StatusOK
	body = map[string]any{"keys": []map[string]string{rsaJWK("old", &oldKey.PublicKey)}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		mu.Lock()
		code, payload := status, body
		mu.Unlock()
		w.WriteHeader(code)
		if payload != nil {
			_ = json.NewEncoder(w).Encode(payload)
		}
	}))
	t.Cleanup(server.Close)
	set := func(code int, payload any) {
		mu.Lock()
		status, body = code, payload
		mu.Unlock()
	}
	verifier := New(server.URL, "authenticated", nil, server.Client(), func() time.Time { return now })
	issuer := server.URL + "/auth/v1"
	sub := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	sessionID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	oldToken := sign(t, jwt.SigningMethodRS256, oldKey, "old", tokenClaims(sub, sessionID, issuer, now))
	newToken := sign(t, jwt.SigningMethodRS256, newKey, "new", tokenClaims(sub, sessionID, issuer, now))

	if _, _, err := verifier.identity(context.Background(), oldToken); err != nil {
		t.Fatal(err)
	}
	if _, _, err := verifier.identity(context.Background(), oldToken); err != nil {
		t.Fatal(err)
	}
	if fetches.Load() != 1 {
		t.Fatalf("cached fetches %d", fetches.Load())
	}

	now = now.Add(10 * time.Minute)
	if _, _, err := verifier.identity(context.Background(), oldToken); err != nil {
		t.Fatal(err)
	}
	if fetches.Load() != 2 {
		t.Fatalf("expiry fetches %d", fetches.Load())
	}

	set(http.StatusOK, map[string]any{"keys": []map[string]string{rsaJWK("new", &newKey.PublicKey)}})
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := verifier.identity(context.Background(), newToken)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if fetches.Load() != 3 {
		t.Fatalf("rotation fetches %d", fetches.Load())
	}

	set(http.StatusOK, map[string]any{"keys": []map[string]string{
		{"kty": "RSA", "alg": "RS256", "kid": "dup"},
		{"kty": "RSA", "alg": "RS256", "kid": "dup"},
	}})
	now = now.Add(10 * time.Minute)
	if _, _, err := verifier.identity(context.Background(), newToken); !errors.Is(err, ErrProvider) {
		t.Fatalf("malformed refresh %v", err)
	}
	now = now.Add(-10 * time.Minute)
	if _, _, err := verifier.identity(context.Background(), newToken); err != nil {
		t.Fatalf("malformed refresh replaced keys: %v", err)
	}

	now = now.Add(10 * time.Minute)
	set(http.StatusInternalServerError, nil)
	if _, _, err := verifier.identity(context.Background(), newToken); !errors.Is(err, ErrProvider) {
		t.Fatalf("failed refresh %v", err)
	}
	set(http.StatusOK, map[string]any{"keys": []map[string]string{rsaJWK("other", &oldKey.PublicKey)}})
	if _, _, err := verifier.identity(context.Background(), newToken); err != ErrUnauthorized {
		t.Fatalf("missing key %v", err)
	}
}
