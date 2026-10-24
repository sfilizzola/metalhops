package checkin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

func TestCreateNow(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)

	got, err := h.create(userA, verifiedVenue, "now-key-1", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !got.VisitedAt.Equal(t0) || !got.RecordedAt.Equal(t0) {
		t.Fatalf("times %+v", got)
	}
	if got.Venue.Name != "Venue" || got.Venue.City != "Oslo" || got.Venue.CountryCode != "DE" ||
		got.Venue.VenueType == nil || *got.Venue.VenueType != "bar" || got.Venue.VerificationStatus != "verified" {
		t.Fatalf("venue %+v", got.Venue)
	}
	if n := countCheckIns(t, h.pool); n != 1 {
		t.Fatalf("rows %d", n)
	}
	if left := tokensLeft(h.limiter, userA); left != 59 {
		t.Fatalf("tokens %d", left)
	}
}

func TestCreateBackdatedAndClamped(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)

	aged := t0.Add(-24 * time.Hour)
	got, err := h.create(userA, verifiedVenue, "clamp-key-1", visitedAtJSON(aged))
	if err != nil || !got.VisitedAt.Equal(aged) || !got.RecordedAt.Equal(t0) {
		t.Fatalf("24h ago %+v %v", got, err)
	}

	ahead := t0.Add(5 * time.Minute)
	got, err = h.create(userA, verifiedVenue, "clamp-key-2", visitedAtJSON(ahead))
	if err != nil || !got.VisitedAt.Equal(t0) || !got.RecordedAt.Equal(t0) {
		t.Fatalf("5m ahead %+v %v", got, err)
	}

	var validation *ValidationError
	_, err = h.create(userA, verifiedVenue, "clamp-key-3", visitedAtJSON(t0.Add(-24*time.Hour-time.Microsecond)))
	if !errors.As(err, &validation) {
		t.Fatalf("too old err %v", err)
	}
	_, err = h.create(userA, verifiedVenue, "clamp-key-4", visitedAtJSON(t0.Add(5*time.Minute+time.Microsecond)))
	if !errors.As(err, &validation) {
		t.Fatalf("too far ahead err %v", err)
	}
	if n := countCheckIns(t, h.pool); n != 2 {
		t.Fatalf("rows %d", n)
	}
	if left := tokensLeft(h.limiter, userA); left != 58 {
		t.Fatalf("tokens %d", left)
	}
}

func TestCreateEquivalentOffsets(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)

	first, err := h.create(userA, verifiedVenue, "same-key-1", json.RawMessage(`"2026-10-24T09:00:00Z"`))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := h.create(userA, verifiedVenue, "same-key-1", json.RawMessage(`"2026-10-24T11:00:00+02:00"`))
	if err != nil || second.ID != first.ID {
		t.Fatalf("second %+v %v", second, err)
	}
	if n := countCheckIns(t, h.pool); n != 1 {
		t.Fatalf("rows %d", n)
	}
}

func TestCreateReplay(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)

	first, err := h.create(userA, verifiedVenue, "replay-key-1", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	tokens := tokensLeft(h.limiter, userA)

	second, err := h.create(userA, verifiedVenue, "replay-key-1", nil)
	if err != nil || second.ID != first.ID || !second.VisitedAt.Equal(first.VisitedAt) {
		t.Fatalf("replay %+v %v", second, err)
	}
	if n := countCheckIns(t, h.pool); n != 1 {
		t.Fatalf("rows %d", n)
	}
	if left := tokensLeft(h.limiter, userA); left != tokens {
		t.Fatalf("tokens %d want %d", left, tokens)
	}

	ctx := context.Background()
	if _, err := h.pool.Exec(ctx, `delete from check_ins where id = $1`, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(ctx, `
		update venues set verification_status = 'delisted', delisting_reason = 'duplicate' where id = $1`, verifiedVenue); err != nil {
		t.Fatal(err)
	}
	third, err := h.create(userA, verifiedVenue, "replay-key-1", nil)
	if err != nil || third.ID != first.ID {
		t.Fatalf("replay after delist %+v %v", third, err)
	}
}

func TestCreateReplayAfterWindow(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)

	visited := t0.Add(-23 * time.Hour)
	first, err := h.create(userA, verifiedVenue, "back-key-1", visitedAtJSON(visited))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	h.now = h.now.Add(2 * time.Hour)
	second, err := h.create(userA, verifiedVenue, "back-key-1", visitedAtJSON(visited))
	if err != nil || second.ID != first.ID {
		t.Fatalf("replay %+v %v", second, err)
	}
}

func TestCreateKeyReused(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)

	if _, err := h.create(userA, verifiedVenue, "reuse-key-1", nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err := h.create(userA, verifiedVenue, "reuse-key-1", visitedAtJSON(t0.Add(-time.Hour)))
	if !errors.Is(err, idempotency.ErrReused) {
		t.Fatalf("err %v", err)
	}
}

func TestSpacing(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	seeded := insertCheckIn(t, h.pool, verifiedVenue, userA, t0)

	h.now = t0.Add(time.Hour)
	_, err := h.create(userA, verifiedVenue, "spacing-key-1", nil)
	var spacing *SpacingError
	if !errors.As(err, &spacing) || spacing.CheckInID != seeded || !spacing.VisitedAt.Equal(t0) {
		t.Fatalf("spacing err %+v", err)
	}
	if n := countIdempotencyRecords(t, h.pool); n != 0 {
		t.Fatalf("idempotency rows %d", n)
	}
	if left := tokensLeft(h.limiter, userA); left != 60 {
		t.Fatalf("tokens %d", left)
	}

	h.now = t0.Add(time.Hour + time.Microsecond)
	if _, err := h.create(userA, verifiedVenue, "spacing-key-1", nil); err != nil {
		t.Fatalf("after boundary: %v", err)
	}
	if n := countCheckIns(t, h.pool); n != 2 {
		t.Fatalf("rows %d", n)
	}
}

func TestSpacingExplicitRetry(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertCheckIn(t, h.pool, verifiedVenue, userA, t0)

	conflicting := t0.Add(-30 * time.Minute)
	var spacing *SpacingError
	if _, err := h.create(userA, verifiedVenue, "retry-key-1", visitedAtJSON(conflicting)); !errors.As(err, &spacing) {
		t.Fatalf("first conflict err %v", err)
	}
	if _, err := h.create(userA, verifiedVenue, "retry-key-1", visitedAtJSON(conflicting)); !errors.As(err, &spacing) {
		t.Fatalf("second conflict err %v", err)
	}
	far := t0.Add(-2 * time.Hour)
	if _, err := h.create(userA, verifiedVenue, "retry-key-1", visitedAtJSON(far)); err != nil {
		t.Fatalf("far insert: %v", err)
	}
	if n := countCheckIns(t, h.pool); n != 2 {
		t.Fatalf("rows %d", n)
	}
}

func TestSpacingNearestAndTie(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertVenue(t, h.pool, pendingVenue, "pending")
	insertUser(t, h.pool, userA, "active", nil)

	insertCheckIn(t, h.pool, verifiedVenue, userA, t0.Add(-50*time.Minute))
	nearer := insertCheckIn(t, h.pool, verifiedVenue, userA, t0.Add(20*time.Minute))

	var spacing *SpacingError
	_, err := h.create(userA, verifiedVenue, "nearest-key-1", nil)
	if !errors.As(err, &spacing) || spacing.CheckInID != nearer {
		t.Fatalf("nearest %+v", err)
	}

	ctx := context.Background()
	if _, err := h.pool.Exec(ctx, `
		insert into check_ins (id, venue_id, user_id, visited_at, recorded_at)
		values ('00000000-0000-4000-8000-0000000000a2', $1, $2, $3, $3)`,
		pendingVenue, userA, t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(ctx, `
		insert into check_ins (id, venue_id, user_id, visited_at, recorded_at)
		values ('00000000-0000-4000-8000-0000000000a1', $1, $2, $3, $3)`,
		pendingVenue, userA, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, err = h.create(userA, pendingVenue, "tie-key-1", nil)
	if !errors.As(err, &spacing) || spacing.CheckInID != "00000000-0000-4000-8000-0000000000a1" {
		t.Fatalf("tie %+v", err)
	}
}

func TestSpacingIsPerUser(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	insertCheckIn(t, h.pool, verifiedVenue, userB, t0)

	if _, err := h.create(userA, verifiedVenue, "per-user-key-1", nil); err != nil {
		t.Fatalf("create: %v", err)
	}
}

func TestSpacingSurvivesDelisting(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	seeded := insertCheckIn(t, h.pool, verifiedVenue, userA, t0)

	ctx := context.Background()
	if _, err := h.pool.Exec(ctx, `
		update venues set verification_status = 'delisted', delisting_reason = 'duplicate' where id = $1`, verifiedVenue); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(ctx, `
		update venues set verification_status = 'verified', delisting_reason = null, delisting_note = null where id = $1`, verifiedVenue); err != nil {
		t.Fatal(err)
	}

	h.now = t0.Add(time.Hour)
	var spacing *SpacingError
	_, err := h.create(userA, verifiedVenue, "survive-key-1", nil)
	if !errors.As(err, &spacing) || spacing.CheckInID != seeded {
		t.Fatalf("spacing %+v", err)
	}
}

func TestCreateConcurrent(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)

	keys := []string{"race-key-a", "race-key-b"}
	errs := make([]error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i, key := range keys {
		go func(i int, key string) {
			defer wg.Done()
			_, errs[i] = h.create(userA, verifiedVenue, key, nil)
		}(i, key)
	}
	wg.Wait()

	successes, spacingErrs := 0, 0
	for _, err := range errs {
		var spacing *SpacingError
		switch {
		case err == nil:
			successes++
		case errors.As(err, &spacing):
			spacingErrs++
		}
	}
	if successes != 1 || spacingErrs != 1 {
		t.Fatalf("outcomes %v", errs)
	}
	if n := countCheckIns(t, h.pool); n != 1 {
		t.Fatalf("rows %d", n)
	}
	if left := tokensLeft(h.limiter, userA); left != 59 {
		t.Fatalf("tokens %d", left)
	}

	results := make([]CheckIn, 2)
	errs2 := make([]error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			results[i], errs2[i] = h.create(userB, verifiedVenue, "race-key-c", nil)
		}(i)
	}
	wg.Wait()
	if errs2[0] != nil || errs2[1] != nil || results[0].ID != results[1].ID {
		t.Fatalf("results %+v errs %v", results, errs2)
	}
	if n := countCheckIns(t, h.pool); n != 2 {
		t.Fatalf("rows %d", n)
	}
}

func TestCreateVisibility(t *testing.T) {
	h := newCreateHarness(t)
	for id, status := range map[string]string{
		verifiedVenue: "verified", pendingVenue: "pending", rejectedVenue: "rejected",
		delistedVenue: "delisted", bareVenue: "bare",
	} {
		insertVenue(t, h.pool, id, status)
	}
	insertUser(t, h.pool, userA, "active", nil)
	future := t0.Add(time.Hour)
	insertUser(t, h.pool, userC, "suspended", &future)

	got, err := h.create(userA, pendingVenue, "visibility-key-1", nil)
	if err != nil || got.Venue.VerificationStatus != "pending" || got.Venue.VenueType == nil {
		t.Fatalf("pending %+v %v", got, err)
	}
	for _, id := range []string{rejectedVenue, delistedVenue, bareVenue, unknownVenue} {
		if _, err := h.create(userA, id, "visibility-key-2", nil); !errors.Is(err, ErrVenueNotFound) {
			t.Fatalf("%s err %v", id, err)
		}
	}
	if _, err := h.create(userC, rejectedVenue, "visibility-key-3", nil); !errors.Is(err, ErrVenueNotFound) {
		t.Fatalf("suspended on rejected err %v", err)
	}
	var suspended *profile.SuspendedError
	if _, err := h.create(userC, verifiedVenue, "visibility-key-4", nil); !errors.As(err, &suspended) {
		t.Fatalf("suspended on verified err %v", err)
	}
}

func TestCreateProfileErrors(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "", nil)
	insertUser(t, h.pool, userB, "rename_required", nil)
	expired := t0.Add(-time.Hour)
	insertUser(t, h.pool, userC, "suspended", &expired)

	if _, err := h.create(userA, verifiedVenue, "profile-key-1", nil); !errors.Is(err, profile.ErrProfileRequired) {
		t.Fatalf("no profile err %v", err)
	}
	if _, err := h.create(userB, verifiedVenue, "profile-key-2", nil); !errors.Is(err, profile.ErrRenameRequired) {
		t.Fatalf("rename err %v", err)
	}
	if _, err := h.create(userC, verifiedVenue, "profile-key-3", nil); err != nil {
		t.Fatalf("expired suspension: %v", err)
	}
}

func TestCreateRateLimited(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	held := make([]*ratelimit.Reservation, 0, 60)
	for i := 0; i < 60; i++ {
		r, _, ok := h.limiter.Reserve(userA)
		if !ok {
			t.Fatalf("drain %d", i)
		}
		held = append(held, r)
	}

	var limited *RateLimitError
	if _, err := h.create(userA, verifiedVenue, "ratelimit-key-1", nil); !errors.As(err, &limited) {
		t.Fatalf("err %v", err)
	}
	if n := countCheckIns(t, h.pool); n != 0 {
		t.Fatalf("rows %d", n)
	}
}

func TestCreateInvalid(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)

	var validation *ValidationError
	_, err := h.create(userA, strings.ToUpper(verifiedVenue), "invalid-key-1", nil)
	if !errors.As(err, &validation) || validation.Fields["venue_id"] != "must be a canonical uuid" {
		t.Fatalf("uppercase err %v", err)
	}
	_, err = h.create(userA, verifiedVenue, "short", nil)
	if !errors.Is(err, idempotency.ErrInvalid) {
		t.Fatalf("short key err %v", err)
	}
}
