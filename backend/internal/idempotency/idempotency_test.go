package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync/atomic"
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

func TestExecuteConflictExpiryAndRollback(t *testing.T) {
	pool := openPool(t)
	user := insertUser(t, pool, "33333333-3333-4333-8333-333333333333")
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	req := Request{UserID: user, Operation: "venue_submission", Key: "same-key1", Command: []byte(`{"name":"a"}`)}
	var calls int
	fn := func(context.Context) (Result, error) {
		calls++
		return Result{Status: 201, Body: []byte(`{"ok":true}`)}, nil
	}
	commitCall(t, pool, now, req, fn)

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	changed := req
	changed.Command = []byte(`{"name":"b"}`)
	_, err = Execute(context.Background(), tx, now, changed, func(context.Context) (Result, error) {
		t.Fatal("callback")
		return Result{}, nil
	})
	if !errors.Is(err, ErrReused) || calls != 1 {
		t.Fatalf("reused err %v calls %d", err, calls)
	}
	_ = tx.Rollback(context.Background())

	conflict, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	status, err := Execute(context.Background(), conflict, now, req, func(context.Context) (Result, error) {
		calls++
		return Result{Status: 409, Body: []byte(`{"no":true}`)}, nil
	})
	if err != nil || status.Status != 201 || calls != 1 {
		t.Fatalf("live replay status %#v err %v calls %d", status, err, calls)
	}
	_ = conflict.Rollback(context.Background())

	fresh := req
	fresh.Key = "status-key"
	tx, err = pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, err := Execute(context.Background(), tx, now, fresh, func(context.Context) (Result, error) {
		calls++
		return Result{Status: 409, Body: []byte(`{"no":true}`)}, nil
	})
	if err != nil || got.Status != 409 {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(context.Background(), `select count(*) from idempotency_records where key = 'status-key'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("409 stored %d", n)
	}
	commitCall(t, pool, now, fresh, fn)

	errKey := req
	errKey.Key = "error-key1"
	tx, err = pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = Execute(context.Background(), tx, now, errKey, func(context.Context) (Result, error) {
		return Result{}, errors.New("boom")
	})
	if err == nil || errors.Is(err, ErrInvalid) {
		t.Fatalf("callback err %v", err)
	}
	_ = tx.Rollback(context.Background())
	if err := pool.QueryRow(context.Background(), `select count(*) from idempotency_records where key = 'error-key1'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("error stored %d", n)
	}

	badJSON := req
	badJSON.Key = "bad-json1"
	tx, err = pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = Execute(context.Background(), tx, now, badJSON, func(context.Context) (Result, error) {
		return Result{Status: 201, Body: []byte("not-json")}, nil
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("json err %v", err)
	}
	_ = tx.Rollback(context.Background())

	if _, err := pool.Exec(context.Background(), `create table probe (n int)`); err != nil {
		t.Fatal(err)
	}
	probe := req
	probe.Key = "probe-key1"
	tx, err = pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(context.Background(), tx, now, probe, func(context.Context) (Result, error) {
		if _, err := tx.Exec(context.Background(), `insert into probe (n) values (1)`); err != nil {
			return Result{}, err
		}
		return Result{Status: 201, Body: []byte(`{"ok":true}`)}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	commitCall(t, pool, now, probe, func(ctx context.Context) (Result, error) {
		if _, err := pool.Exec(ctx, `insert into probe (n) values (1)`); err != nil {
			return Result{}, err
		}
		return Result{Status: 201, Body: []byte(`{"ok":true}`)}, nil
	})
	if err := pool.QueryRow(context.Background(), `select count(*) from probe`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("probe %d", n)
	}

	early := now.Add(24*time.Hour - time.Nanosecond)
	tx, err = pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(context.Background(), tx, early, req, func(context.Context) (Result, error) {
		t.Fatal("early callback")
		return Result{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback(context.Background())
	before := calls
	commitCall(t, pool, now.Add(24*time.Hour), req, fn)
	if calls != before+1 {
		t.Fatalf("expiry calls %d before %d", calls, before)
	}
}

func TestExecuteCleanupBatch(t *testing.T) {
	pool := openPool(t)
	user := insertUser(t, pool, "44444444-4444-4444-8444-444444444444")
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 101; i++ {
		key := fmt.Sprintf("expired-%03d", i)
		if _, err := pool.Exec(context.Background(), `
			insert into idempotency_records (
				user_id, operation, key, request_hash, response_status, response_body, expires_at
			) values ($1, 'cleanup_row', $2, repeat('ab', 32), 200, '{}', $3)`,
			user, key, now.Add(-2*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(context.Background(), `
		insert into idempotency_records (
			user_id, operation, key, request_hash, response_status, response_body, expires_at
		) values ($1, 'venue_submission', 'subject-key', repeat('cd', 32), 200, '{}', $2)`,
		user, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	commitCall(t, pool, now, Request{
		UserID: user, Operation: "venue_submission", Key: "subject-key", Command: []byte(`{"name":"new"}`),
	}, func(context.Context) (Result, error) {
		return Result{Status: 201, Body: []byte(`{"ok":true}`)}, nil
	})
	var expired, live int
	if err := pool.QueryRow(context.Background(), `select count(*) from idempotency_records where expires_at <= $1`, now).Scan(&expired); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `
		select count(*) from idempotency_records
		where user_id = $1 and operation = 'venue_submission' and key = 'subject-key' and expires_at > $2`,
		user, now).Scan(&live); err != nil {
		t.Fatal(err)
	}
	if expired != 1 || live != 1 {
		t.Fatalf("expired %d live %d", expired, live)
	}
}

func TestExecuteDifferentKeysOverlap(t *testing.T) {
	pool := openPool(t)
	user := insertUser(t, pool, "55555555-5555-4555-8555-555555555555")
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	run := func(key string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		_, err = Execute(ctx, tx, now, Request{UserID: user, Operation: "venue_submission", Key: key, Command: []byte(`{"n":1}`)}, func(context.Context) (Result, error) {
			calls.Add(1)
			entered <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return Result{}, ctx.Err()
			}
			return Result{Status: 201, Body: []byte(`{"ok":true}`)}, nil
		})
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	errCh := make(chan error, 2)
	go func() { errCh <- run("overlap-a") }()
	go func() { errCh <- run("overlap-b") }()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("callbacks did not overlap")
		}
	}
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("calls %d", calls.Load())
	}
}

func TestExecuteConcurrentReplay(t *testing.T) {
	pool := openPool(t)
	user := insertUser(t, pool, "66666666-6666-4666-8666-666666666666")
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	req := Request{UserID: user, Operation: "venue_submission", Key: "race-key1", Command: []byte(`{"name":"a"}`)}
	var calls atomic.Int32
	holding := make(chan struct{})
	errCh := make(chan error, 2)
	resCh := make(chan Result, 2)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tx, err := pool.Begin(ctx)
		if err != nil {
			errCh <- err
			return
		}
		defer tx.Rollback(ctx)
		result, err := Execute(ctx, tx, now, req, func(context.Context) (Result, error) {
			calls.Add(1)
			close(holding)
			for {
				var waiting int
				if err := pool.QueryRow(ctx, `select count(*) from pg_locks where locktype = 'advisory' and not granted`).Scan(&waiting); err != nil {
					return Result{}, err
				}
				if waiting >= 1 {
					break
				}
				if ctx.Err() != nil {
					return Result{}, ctx.Err()
				}
			}
			return Result{Status: 201, Body: []byte(`{"ok":true}`)}, nil
		})
		if err != nil {
			errCh <- err
			return
		}
		if err := tx.Commit(ctx); err != nil {
			errCh <- err
			return
		}
		resCh <- result
	}()
	go func() {
		select {
		case <-holding:
		case <-time.After(5 * time.Second):
			errCh <- errors.New("first callback did not start")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tx, err := pool.Begin(ctx)
		if err != nil {
			errCh <- err
			return
		}
		defer tx.Rollback(ctx)
		result, err := Execute(ctx, tx, now, req, func(context.Context) (Result, error) {
			calls.Add(1)
			return Result{Status: 201, Body: []byte(`{"other":true}`)}, nil
		})
		if err != nil {
			errCh <- err
			return
		}
		if err := tx.Commit(ctx); err != nil {
			errCh <- err
			return
		}
		resCh <- result
	}()
	var results []Result
	deadline := time.After(5 * time.Second)
	for len(results) < 2 {
		select {
		case err := <-errCh:
			t.Fatal(err)
		case result := <-resCh:
			results = append(results, result)
		case <-deadline:
			t.Fatal("timed out")
		}
	}
	if calls.Load() != 1 || string(results[0].Body) != string(results[1].Body) {
		t.Fatalf("calls %d results %#v %#v", calls.Load(), results[0], results[1])
	}
}

func TestExecuteUserCascade(t *testing.T) {
	pool := openPool(t)
	user := insertUser(t, pool, "77777777-7777-4777-8777-777777777777")
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	commitCall(t, pool, now, Request{UserID: user, Operation: "venue_submission", Key: "cascade1", Command: []byte(`{"n":1}`)}, func(context.Context) (Result, error) {
		return Result{Status: 201, Body: []byte(`{"ok":true}`)}, nil
	})
	if _, err := pool.Exec(context.Background(), `delete from auth.users where id = $1`, user); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(context.Background(), `select count(*) from idempotency_records where user_id = $1`, user).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("rows %d", n)
	}
}

func commitCall(t *testing.T, pool *pgxpool.Pool, now time.Time, req Request, fn func(context.Context) (Result, error)) Result {
	t.Helper()
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	result, err := Execute(context.Background(), tx, now, req, fn)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestExecuteScopesByUserAndOperation(t *testing.T) {
	pool := openPool(t)
	userA := insertUser(t, pool, "88888888-8888-4888-8888-888888888888")
	userB := insertUser(t, pool, "99999999-9999-4999-8999-999999999999")
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	var calls int
	fn := func(context.Context) (Result, error) {
		calls++
		return Result{Status: 201, Body: []byte(`{"ok":true}`)}, nil
	}
	base := Request{UserID: userA, Operation: "venue_submission", Key: "same-key1", Command: []byte(`{"name":"a"}`)}
	commitCall(t, pool, now, base, fn)
	otherUser := base
	otherUser.UserID = userB
	commitCall(t, pool, now, otherUser, fn)
	otherOp := base
	otherOp.Operation = "review_create"
	commitCall(t, pool, now, otherOp, fn)
	if calls != 3 {
		t.Fatalf("calls %d", calls)
	}
}
