package profile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

func TestGetAndPatch(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	userID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, userID); err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.NewUserLimiter(clock)
	service := New(pool, limiter, nil, clock)

	if _, err := service.Get(ctx, userID); err != ErrNotFound {
		t.Fatalf("missing %v", err)
	}
	username := "metal_fan"
	if _, err := service.Patch(ctx, userID, Patch{Username: &username}); err == nil {
		t.Fatal("create without display name")
	}
	var n int
	if err := pool.QueryRow(ctx, `select count(*) from profiles`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows %d %v", n, err)
	}

	display := " Metal Fan "
	created, err := service.Patch(ctx, userID, Patch{Username: &username, DisplayName: &display})
	if err != nil || created.Username != "metal_fan" || created.DisplayName != "Metal Fan" || created.Status != "active" {
		t.Fatalf("create %#v %v", created, err)
	}
	got, err := service.Get(ctx, userID)
	if err != nil || got.Username != created.Username {
		t.Fatalf("get %#v %v", got, err)
	}

	next := "Metal Fan"
	unchanged, err := service.Patch(ctx, userID, Patch{DisplayName: &next})
	if err != nil || !unchanged.UpdatedAt.Equal(created.UpdatedAt) {
		t.Fatalf("unchanged %#v %v", unchanged, err)
	}
	held := make([]*ratelimit.Reservation, 0, 59)
	for i := 0; i < 59; i++ {
		reservation, _, ok := limiter.Reserve(userID)
		if !ok {
			t.Fatalf("no-op spent a token at %d", i)
		}
		held = append(held, reservation)
	}
	if _, _, ok := limiter.Reserve(userID); ok {
		t.Fatal("create did not spend a token")
	}
	for _, reservation := range held {
		reservation.Cancel()
	}

	now = now.Add(time.Minute)
	partial := "Partial"
	updated, err := service.Patch(ctx, userID, Patch{DisplayName: &partial})
	if err != nil || updated.DisplayName != "Partial" || updated.Username != "metal_fan" || !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Fatalf("partial %#v %v", updated, err)
	}
}

func TestPatchKeepsStoredModerationUntilChange(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	userID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into profiles (
			id, username, display_name, status, moderation_reason, moderation_note, created_at, updated_at
		) values ($1, 'keptname', 'Kept', 'rename_required', 'abusive_name', 'note', $2, $2)`, userID, now); err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.NewUserLimiter(clock)
	service := New(pool, limiter, nil, clock)
	username, display := "keptname", "Kept"
	got, err := service.Patch(ctx, userID, Patch{Username: &username, DisplayName: &display})
	if err != nil || got.Status != "rename_required" || !got.UpdatedAt.Equal(now) {
		t.Fatalf("noop %#v %v", got, err)
	}
	var reason, note *string
	if err := pool.QueryRow(ctx, `select moderation_reason, moderation_note from profiles where id = $1`, userID).Scan(&reason, &note); err != nil || reason == nil || *reason != "abusive_name" {
		t.Fatalf("moderation %v %v %v", reason, note, err)
	}

	now = now.Add(time.Minute)
	corrected := "KeptName"
	changed, err := service.Patch(ctx, userID, Patch{Username: &corrected})
	if err != nil || changed.Username != "KeptName" || changed.Status != "active" || !changed.UpdatedAt.Equal(now) {
		t.Fatalf("correction %#v %v", changed, err)
	}
	if err := pool.QueryRow(ctx, `select moderation_reason, moderation_note from profiles where id = $1`, userID).Scan(&reason, &note); err != nil || reason != nil || note != nil {
		t.Fatalf("cleared %v %v %v", reason, note, err)
	}
	if left := drain(limiter, userID); left != 59 {
		t.Fatalf("correction tokens left %d", left)
	}
}

func TestPatchUsernameTakenAndLimit(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	owner := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	other := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1), ($2)`, owner, other); err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.NewUserLimiter(clock)
	service := New(pool, limiter, nil, clock)
	username, display := "metal_fan", "Owner"
	if _, err := service.Patch(ctx, owner, Patch{Username: &username, DisplayName: &display}); err != nil {
		t.Fatal(err)
	}
	taken := "Metal_Fan"
	if _, err := service.Patch(ctx, other, Patch{Username: &taken, DisplayName: &display}); err != ErrUsernameTaken {
		t.Fatalf("taken %v", err)
	}
	if left := drain(limiter, other); left != 60 {
		t.Fatalf("failed write left %d", left)
	}

	bad := "no"
	if _, err := service.Patch(ctx, other, Patch{Username: &bad, DisplayName: &display}); err == nil {
		t.Fatal("invalid username reached SQL")
	}
	var n int
	if err := pool.QueryRow(ctx, `select count(*) from profiles where id = $1`, other).Scan(&n); err != nil || n != 0 {
		t.Fatalf("invalid row %d %v", n, err)
	}

	limited := ratelimit.NewUserLimiter(clock)
	limitedService := New(pool, limited, nil, clock)
	for i := 0; i < 60; i++ {
		name := fmt.Sprintf("Name %d", i)
		if _, err := limitedService.Patch(ctx, owner, Patch{DisplayName: &name}); err != nil {
			t.Fatalf("change %d %v", i, err)
		}
	}
	overflow := "overflow"
	_, err := limitedService.Patch(ctx, owner, Patch{DisplayName: &overflow})
	var limitedErr *RateLimitError
	if !errors.As(err, &limitedErr) || limitedErr.RetryAfter < 1 {
		t.Fatalf("limit %v", err)
	}
}

func TestConcurrentProfileCreate(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	userID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, userID); err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.NewUserLimiter(func() time.Time { return now })
	service := New(pool, limiter, nil, func() time.Time { return now })
	username, display := "raceuser", "Race"
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = service.Patch(ctx, userID, Patch{Username: &username, DisplayName: &display})
		}(i)
	}
	close(start)
	wg.Wait()
	for _, err := range errs {
		if err != nil && !errors.Is(err, ErrUsernameTaken) {
			t.Fatal(err)
		}
		if err != nil && (strings.Contains(err.Error(), "SQLSTATE") || strings.Contains(err.Error(), "duplicate")) {
			t.Fatal(err)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, `select count(*) from profiles`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows %d %v", n, err)
	}
	if left := drain(limiter, userID); left != 59 {
		t.Fatalf("lost reservation restored to %d", left)
	}
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func drain(limiter *ratelimit.UserLimiter, userID string) int {
	n := 0
	for n < 60 {
		reservation, _, ok := limiter.Reserve(userID)
		if !ok {
			break
		}
		reservation.Commit()
		n++
	}
	return n
}
