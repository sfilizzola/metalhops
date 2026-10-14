package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
)

func TestExecuteReplayAndValidation(t *testing.T) {
	pool := openPool(t)
	userA := insertUser(t, pool, "11111111-1111-4111-8111-111111111111")
	userB := insertUser(t, pool, "22222222-2222-4222-8222-222222222222")
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	var calls int
	req := Request{UserID: userA, Operation: "venue_submission", Key: "same-key1", Command: []byte(`{"name":"a"}`)}

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first, err := Execute(context.Background(), tx, now, req, func(context.Context) (Result, error) {
		calls++
		return Result{Status: 201, Body: []byte(`{"b":1,"a":2}`)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}

	tx, err = pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := Execute(context.Background(), tx, now, req, func(context.Context) (Result, error) {
		calls++
		return Result{Status: 500, Body: []byte(`{"no":true}`)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || first.Status != 201 || string(first.Body) != string(second.Body) {
		t.Fatalf("calls %d first %#v second %#v", calls, first, second)
	}

	var stored string
	var hash string
	if err := pool.QueryRow(context.Background(), `
		select response_body::text, request_hash
		from idempotency_records
		where user_id = $1 and operation = $2 and key = $3`,
		userA, req.Operation, req.Key).Scan(&stored, &hash); err != nil {
		t.Fatal(err)
	}
	if stored != string(first.Body) {
		t.Fatalf("stored %s body %s", stored, first.Body)
	}
	sum := sha256.Sum256(req.Command)
	if hash != hex.EncodeToString(sum[:]) {
		t.Fatalf("hash %s", hash)
	}

	otherUser := req
	otherUser.UserID = userB
	runOnce(t, pool, now, otherUser)
	otherOp := req
	otherOp.Operation = "review_create"
	runOnce(t, pool, now, otherOp)

	wide := req
	wide.Key = "wide-key1"
	wide.Command = make([]byte, 65536)
	for i := range wide.Command {
		wide.Command[i] = 'x'
	}
	runOnce(t, pool, now, wide)

	rolled, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := rolled.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Request{
		{UserID: "BAD", Operation: "venue_submission", Key: "same-key1", Command: []byte("x")},
		{UserID: userA, Operation: "Venue", Key: "same-key1", Command: []byte("x")},
		{UserID: userA, Operation: "venue_submission", Key: "short", Command: []byte("x")},
		{UserID: userA, Operation: "venue_submission", Key: "same-key1", Command: nil},
		{UserID: userA, Operation: "venue_submission", Key: "same-key1", Command: make([]byte, 65537)},
	} {
		_, err := Execute(context.Background(), rolled, now, bad, func(context.Context) (Result, error) {
			t.Fatal("callback")
			return Result{}, nil
		})
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("%#v err %v", bad, err)
		}
	}
}

func runOnce(t *testing.T, pool *pgxpool.Pool, now time.Time, req Request) {
	t.Helper()
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := Execute(context.Background(), tx, now, req, func(context.Context) (Result, error) {
		return Result{Status: 201, Body: []byte(`{"ok":true}`)}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func openPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func insertUser(t *testing.T, pool *pgxpool.Pool, id string) string {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `insert into auth.users (id) values ($1)`, id); err != nil {
		t.Fatal(err)
	}
	return id
}
