package checkin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

func (h *createHarness) feed(venueID, callerID string) ([]FeedItem, error) {
	return h.service.ListVenueFeed(context.Background(), venueID, callerID)
}

func (h *createHarness) checkInExists(t *testing.T, id string) bool {
	t.Helper()
	var exists bool
	if err := h.pool.QueryRow(context.Background(), `select exists (select 1 from check_ins where id = $1)`, id).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func TestVenueFeed(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertVenue(t, h.pool, pendingVenue, "pending")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	insertUser(t, h.pool, userC, "rename_required", nil)
	users := []string{userA, userB, userC}

	ids := make([]string, 25)
	owners := make([]string, 25)
	for i := range ids {
		owners[i] = users[i%3]
		ids[i] = insertCheckIn(t, h.pool, verifiedVenue, owners[i], t0.Add(-time.Duration(i)*time.Minute))
	}

	items, err := h.feed(verifiedVenue, userA)
	if err != nil || len(items) != 20 {
		t.Fatalf("feed %d %v", len(items), err)
	}
	for i, item := range items {
		if item.ID != ids[i] || !item.RecordedAt.Equal(t0.Add(-time.Duration(i)*time.Minute)) {
			t.Fatalf("item %d %+v want %s", i, item, ids[i])
		}
		want := profile.Identity{Username: "u" + owners[i][len(owners[i])-12:], DisplayName: "Tester"}
		if owners[i] == userC {
			want = profile.Identity{Username: "", DisplayName: "MetalHops User"}
		}
		if item.Checker != want {
			t.Fatalf("item %d checker %+v want %+v", i, item.Checker, want)
		}
	}

	if err := h.service.Delete(context.Background(), owners[0], ids[0]); err != nil {
		t.Fatalf("delete: %v", err)
	}
	items, err = h.feed(verifiedVenue, userA)
	if err != nil || len(items) != 20 || items[0].ID != ids[1] {
		t.Fatalf("after delete %+v %v", items, err)
	}

	empty, err := h.feed(pendingVenue, userA)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty feed %#v %v", empty, err)
	}
}

func TestVenueFeedCallerGate(t *testing.T) {
	h := newCreateHarness(t)
	for id, status := range map[string]string{
		verifiedVenue: "verified", rejectedVenue: "rejected", delistedVenue: "delisted", bareVenue: "bare",
	} {
		insertVenue(t, h.pool, id, status)
	}
	insertUser(t, h.pool, userA, "", nil)
	future := t0.Add(time.Hour)
	insertUser(t, h.pool, userB, "suspended", &future)
	insertUser(t, h.pool, userC, "rename_required", nil)

	if _, err := h.feed(verifiedVenue, userA); !errors.Is(err, profile.ErrProfileRequired) {
		t.Fatalf("no profile err %v", err)
	}
	for _, caller := range []string{userB, userC} {
		if _, err := h.feed(verifiedVenue, caller); err != nil {
			t.Fatalf("%s err %v", caller, err)
		}
	}
	for _, venueID := range []string{rejectedVenue, delistedVenue, bareVenue, unknownVenue} {
		for _, caller := range []string{userA, userB, userC} {
			if _, err := h.feed(venueID, caller); !errors.Is(err, ErrVenueNotFound) {
				t.Fatalf("%s by %s err %v", venueID, caller, err)
			}
		}
	}
}

func TestVenueFeedPlan(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	insertVenue(t, pool, verifiedVenue, "verified")
	if _, err := pool.Exec(ctx, `insert into auth.users (id) select gen_random_uuid() from generate_series(1, 100)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into profiles (id, username, display_name, status, created_at, updated_at)
		select id, 'u' || substr(replace(id::text, '-', ''), 1, 12), 'Tester', 'active', now(), now()
		from auth.users`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		with users as (select id, row_number() over (order by id) - 1 as n from auth.users)
		insert into check_ins (venue_id, user_id, visited_at, recorded_at)
		select $1::uuid, users.id, now() - (i || ' minutes')::interval, now() - (i || ' minutes')::interval
		from generate_series(0, 4999) as i
		join users on users.n = i % 100`, verifiedVenue); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "vacuum analyze check_ins"); err != nil {
		t.Fatal(err)
	}
	if plan := explain(t, pool, feedSQL, verifiedVenue, feedLimit); !strings.Contains(plan, "check_ins_venue_feed_idx") {
		t.Fatalf("feed plan does not use check_ins_venue_feed_idx:\n%s", plan)
	}
}

func TestDelete(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertVenue(t, h.pool, delistedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	ctx := context.Background()

	own := insertCheckIn(t, h.pool, verifiedVenue, userA, t0.Add(-time.Hour))
	if err := h.service.Delete(ctx, userA, own); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if h.checkInExists(t, own) {
		t.Fatal("row remains")
	}
	if left := tokensLeft(h.limiter, userA); left != 59 {
		t.Fatalf("tokens %d", left)
	}
	if err := h.service.Delete(ctx, userA, own); !errors.Is(err, ErrCheckInNotFound) {
		t.Fatalf("second delete err %v", err)
	}

	kept := insertCheckIn(t, h.pool, verifiedVenue, userA, t0.Add(-2*time.Hour))
	if err := h.service.Delete(ctx, userB, kept); !errors.Is(err, ErrCheckInNotFound) {
		t.Fatalf("foreign delete err %v", err)
	}
	if !h.checkInExists(t, kept) {
		t.Fatal("foreign row deleted")
	}

	delisted := insertCheckIn(t, h.pool, delistedVenue, userA, t0)
	if _, err := h.pool.Exec(ctx, `
		update venues set verification_status = 'delisted', delisting_reason = 'duplicate' where id = $1`, delistedVenue); err != nil {
		t.Fatal(err)
	}
	if err := h.service.Delete(ctx, userA, delisted); err != nil {
		t.Fatalf("delisted delete: %v", err)
	}
	if h.checkInExists(t, delisted) {
		t.Fatal("delisted row remains")
	}
	if _, err := h.create(userA, delistedVenue, "delete-key-1", nil); !errors.Is(err, ErrVenueNotFound) {
		t.Fatalf("create on delisted err %v", err)
	}
}

func TestDeleteProfileErrors(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	future := t0.Add(time.Hour)
	insertUser(t, h.pool, userA, "suspended", &future)
	insertUser(t, h.pool, userB, "active", nil)
	insertUser(t, h.pool, userC, "", nil)
	ctx := context.Background()

	owned := insertCheckIn(t, h.pool, verifiedVenue, userA, t0.Add(-time.Hour))
	var suspended *profile.SuspendedError
	if err := h.service.Delete(ctx, userA, owned); !errors.As(err, &suspended) {
		t.Fatalf("suspended owner err %v", err)
	}
	if !h.checkInExists(t, owned) {
		t.Fatal("suspended owner row deleted")
	}

	foreign := insertCheckIn(t, h.pool, verifiedVenue, userB, t0.Add(-time.Hour))
	if err := h.service.Delete(ctx, userA, foreign); !errors.Is(err, ErrCheckInNotFound) {
		t.Fatalf("suspended foreign err %v", err)
	}
	if err := h.service.Delete(ctx, userC, unknownVenue); !errors.Is(err, ErrCheckInNotFound) {
		t.Fatalf("no profile err %v", err)
	}
}

func TestDeleteRateLimited(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	id := insertCheckIn(t, h.pool, verifiedVenue, userA, t0)
	held := make([]*ratelimit.Reservation, 0, 60)
	for i := 0; i < 60; i++ {
		r, _, ok := h.limiter.Reserve(userA)
		if !ok {
			t.Fatalf("drain %d", i)
		}
		held = append(held, r)
	}

	var limited *RateLimitError
	if err := h.service.Delete(context.Background(), userA, id); !errors.As(err, &limited) {
		t.Fatalf("err %v", err)
	}
	if !h.checkInExists(t, id) {
		t.Fatal("row deleted")
	}
}

func TestDeleteInvalid(t *testing.T) {
	h := newCreateHarness(t)
	var validation *ValidationError
	err := h.service.Delete(context.Background(), userA, "abc")
	if !errors.As(err, &validation) || validation.Fields["check_in_id"] != "must be a canonical uuid" {
		t.Fatalf("err %v", err)
	}
}
