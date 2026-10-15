package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
)

func TestSessionAuthentication(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	const (
		userID    = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		sessionID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		otherUser = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	)
	created := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1), ($2)`, userID, otherUser); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into auth.sessions (id, user_id, created_at) values ($1, $2, $3)`,
		sessionID, userID, created); err != nil {
		t.Fatal(err)
	}

	verifier, token := sessionVerifier(t, pool)
	caller, err := verifier.Authenticate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if caller.UserID != userID || caller.SessionID != sessionID || !caller.SessionCreatedAt.Equal(created) {
		t.Fatalf("caller %#v", caller)
	}

	if _, err := pool.Exec(ctx, `delete from auth.sessions where id = $1`, sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Authenticate(ctx, token); err != ErrUnauthorized {
		t.Fatalf("missing session %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into auth.sessions (id, user_id, created_at) values ($1, $2, $3)`,
		sessionID, otherUser, created); err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Authenticate(ctx, token); err != ErrUnauthorized {
		t.Fatalf("other user %v", err)
	}
}

func TestSessionStorageFailure(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `drop table auth.sessions`); err != nil {
		t.Fatal(err)
	}
	verifier, token := sessionVerifier(t, pool)
	_, err = verifier.Authenticate(ctx, token)
	if err == nil || errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrProvider) {
		t.Fatalf("storage err %v", err)
	}
}

func sessionVerifier(t *testing.T, pool *pgxpool.Pool) (*Verifier, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{rsaJWK("rsa", &key.PublicKey)}})
	}))
	t.Cleanup(server.Close)
	verifier := New(server.URL, "authenticated", pool, server.Client(), func() time.Time { return now })
	token := sign(t, jwt.SigningMethodRS256, key, "rsa", tokenClaims(
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		server.URL+"/auth/v1",
		now,
	))
	return verifier, token
}
