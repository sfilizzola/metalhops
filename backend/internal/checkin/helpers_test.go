package checkin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

var t0 = time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)

const (
	verifiedVenue = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	pendingVenue  = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	rejectedVenue = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	delistedVenue = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	bareVenue     = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	unknownVenue  = "ffffffff-ffff-4fff-8fff-ffffffffffff"
	userA         = "11111111-1111-4111-8111-111111111111"
	userB         = "22222222-2222-4222-8222-222222222222"
	userC         = "33333333-3333-4333-8333-333333333333"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func ptr(s string) *string { return &s }

func insertUser(t *testing.T, pool *pgxpool.Pool, id, status string, until *time.Time) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, id); err != nil {
		t.Fatal(err)
	}
	if status == "" {
		return
	}
	var reason *string
	if status != "active" {
		reason = ptr("spam")
	}
	if _, err := pool.Exec(ctx, `
		insert into profiles (
			id, username, display_name, status, moderation_reason, suspended_until, created_at, updated_at
		) values ($1, $2, 'Tester', $3, $4, $5, $6, $6)`,
		id, "u"+id[len(id)-12:], status, reason, until, t0); err != nil {
		t.Fatal(err)
	}
}

// Status "bare" is a verified Venue with no Genre link.
func insertVenue(t *testing.T, pool *pgxpool.Pool, id, status string) {
	t.Helper()
	ctx := context.Background()
	stored := status
	if status == "bare" {
		stored = "verified"
	}
	var rejectionReason, rejectionNote, delistingReason *string
	switch status {
	case "rejected":
		rejectionReason, rejectionNote = ptr("other"), ptr("seed")
	case "delisted":
		delistingReason = ptr("duplicate")
	}
	if _, err := pool.Exec(ctx, `
		insert into venues (
			id, name, description, location, address, city, country_code, venue_type,
			verification_status, rejection_reason, rejection_note, delisting_reason
		) values (
			$1, 'Venue', 'A metal bar', ST_MakePoint(10, 60)::geography,
			'1 Metal Street', 'Oslo', 'DE', 'bar', $2, $3, $4, $5
		)`, id, stored, rejectionReason, rejectionNote, delistingReason); err != nil {
		t.Fatal(err)
	}
	if status == "bare" {
		return
	}
	if _, err := pool.Exec(ctx, `
		insert into venue_genres (venue_id, genre_id) select $1, id from genres where name = 'Punk'`, id); err != nil {
		t.Fatal(err)
	}
}

func insertCheckIn(t *testing.T, pool *pgxpool.Pool, venueID, userID string, visited time.Time) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `
		insert into check_ins (venue_id, user_id, visited_at, recorded_at)
		values ($1, $2, $3, $3)
		returning id::text`, venueID, userID, visited).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func countCheckIns(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `select count(*) from check_ins`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func countIdempotencyRecords(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `select count(*) from idempotency_records`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func tokensLeft(limiter *ratelimit.UserLimiter, userID string) int {
	held := make([]*ratelimit.Reservation, 0, 60)
	for len(held) < 60 {
		reservation, _, ok := limiter.Reserve(userID)
		if !ok {
			break
		}
		held = append(held, reservation)
	}
	for _, reservation := range held {
		reservation.Cancel()
	}
	return len(held)
}

func visitedAtJSON(at time.Time) json.RawMessage {
	return json.RawMessage(`"` + at.UTC().Format(time.RFC3339Nano) + `"`)
}

type createHarness struct {
	pool    *pgxpool.Pool
	now     time.Time
	limiter *ratelimit.UserLimiter
	service *Service
}

func newCreateHarness(t *testing.T) *createHarness {
	t.Helper()
	h := &createHarness{pool: testPool(t), now: t0}
	clock := func() time.Time { return h.now }
	h.limiter = ratelimit.NewUserLimiter(clock)
	h.service = New(h.pool, profile.New(h.pool, h.limiter, nil, clock), h.limiter, clock, time.Hour)
	return h
}

func (h *createHarness) create(userID, venueID, key string, visitedAt json.RawMessage) (CheckIn, error) {
	return h.service.Create(context.Background(), CreateCommand{
		UserID: userID, VenueID: venueID, Key: key, VisitedAt: visitedAt,
	})
}
