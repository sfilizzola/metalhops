package profile

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

func TestDeleteUser(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	created := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	now := created.Add(10 * time.Minute)
	clock := func() time.Time { return now }
	const userID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	const sessionID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into auth.sessions (id, user_id, created_at) values ($1, $2, $3)`, sessionID, userID, created); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into profiles (id, username, display_name) values ($1, 'metal_fan', 'Metal Fan')`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into idempotency_records (
			user_id, operation, key, request_hash, response_status, response_body, expires_at
		) values ($1, 'delete_test', 'delete-key', repeat('ab', 32), 200, '{}', now() + interval '1 hour')`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into venues (name, location, submitted_by)
		values ('Room', ST_MakePoint(0, 0)::geography, $1)`, userID); err != nil {
		t.Fatal(err)
	}

	var fail bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "provider-secret-body", http.StatusBadGateway)
			return
		}
		if _, err := pool.Exec(ctx, `delete from auth.users where id = $1`, userID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	limiter := ratelimit.NewUserLimiter(clock)
	service := New(pool, limiter, NewAdminClient(server.URL, "service-secret", server.Client()), clock)
	cmd := DeleteCommand{UserID: userID, SessionCreatedAt: created, Confirmation: "DELETE"}
	if err := service.Delete(ctx, cmd); err == nil {
		t.Fatal("bad confirmation")
	}

	now = created.Add(10*time.Minute + time.Nanosecond)
	cmd.Confirmation = "DELETE MY ACCOUNT"
	if err := service.Delete(ctx, cmd); err != ErrRecentAuthenticationRequired {
		t.Fatalf("stale session %v", err)
	}
	now = created.Add(10 * time.Minute)
	fail = true
	if err := service.Delete(ctx, cmd); err != ErrAuthProvider {
		t.Fatalf("provider %v", err)
	}
	var profiles, records, venues int
	var submitted *string
	if err := pool.QueryRow(ctx, `select count(*) from profiles`).Scan(&profiles); err != nil || profiles != 1 {
		t.Fatalf("profiles %d %v", profiles, err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from idempotency_records`).Scan(&records); err != nil || records != 1 {
		t.Fatalf("records %d %v", records, err)
	}
	if err := pool.QueryRow(ctx, `select count(*), max(submitted_by::text) from venues`).Scan(&venues, &submitted); err != nil || venues != 1 || submitted == nil {
		t.Fatalf("venues %d submitted %v %v", venues, submitted, err)
	}
	if left := drain(limiter, userID); left != 60 {
		t.Fatalf("provider failure left %d", left)
	}

	fail = false
	service = New(pool, ratelimit.NewUserLimiter(clock), NewAdminClient(server.URL, "service-secret", server.Client()), clock)
	if err := service.Delete(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from profiles`).Scan(&profiles); err != nil || profiles != 0 {
		t.Fatalf("profiles left %d %v", profiles, err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from idempotency_records`).Scan(&records); err != nil || records != 0 {
		t.Fatalf("records left %d %v", records, err)
	}
	if err := pool.QueryRow(ctx, `select count(*), max(submitted_by::text) from venues`).Scan(&venues, &submitted); err != nil || venues != 1 || submitted != nil {
		t.Fatalf("retained venue %d %v %v", venues, submitted, err)
	}
}

func TestDeleteWithoutProfileAndRateLimit(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	const userID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	const other = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1), ($2)`, userID, other); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := pool.Exec(ctx, `delete from auth.users where id = $1`, userID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	clock := func() time.Time { return now }
	limiter := ratelimit.NewUserLimiter(clock)
	service := New(pool, limiter, NewAdminClient(server.URL, "service-secret", server.Client()), clock)
	err := service.Delete(ctx, DeleteCommand{
		UserID: userID, SessionCreatedAt: now, Confirmation: "DELETE MY ACCOUNT",
	})
	if err != nil {
		t.Fatal(err)
	}

	ownerLimiter := ratelimit.NewUserLimiter(clock)
	owner := New(pool, ownerLimiter, nil, clock)
	username, display := "metal_fan", "Metal Fan"
	if _, err := owner.Patch(ctx, other, Patch{Username: &username, DisplayName: &display}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 59; i++ {
		name := "A"
		if i%2 == 1 {
			name = "B"
		}
		if _, err := owner.Patch(ctx, other, Patch{DisplayName: &name}); err != nil {
			t.Fatalf("warm %d %v", i, err)
		}
	}
	err = owner.Delete(ctx, DeleteCommand{
		UserID: other, SessionCreatedAt: now, Confirmation: "DELETE MY ACCOUNT",
	})
	var limited *RateLimitError
	if !errors.As(err, &limited) || limited.RetryAfter < 1 {
		t.Fatalf("61st %v", err)
	}
}
