package venue

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

// Earlier than any container clock, so trigger-set updated_at is always later.
var submitNow = time.Date(2020, 1, 15, 12, 0, 0, 0, time.UTC)

const (
	userA = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	userB = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

type harness struct {
	pool    *pgxpool.Pool
	service *Service
	limiter *ratelimit.UserLimiter
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := testPool(t)
	clock := func() time.Time { return submitNow }
	limiter := ratelimit.NewUserLimiter(clock)
	return &harness{
		pool:    pool,
		service: New(pool, profile.New(pool, limiter, nil, clock), limiter, clock),
		limiter: limiter,
	}
}

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
		value := "spam"
		reason = &value
	}
	if _, err := pool.Exec(ctx, `
		insert into profiles (
			id, username, display_name, status, moderation_reason, suspended_until, created_at, updated_at
		) values ($1, $2, 'Tester', $3, $4, $5, $6, $6)`,
		id, "u"+id[len(id)-12:], status, reason, until, submitNow); err != nil {
		t.Fatal(err)
	}
}

type seed struct {
	id        string
	name      string
	status    string
	submitter *string
	lat, lon  float64
	created   time.Time
	updated   time.Time
	genre     int
}

func seedVenue(t *testing.T, pool *pgxpool.Pool, s seed) {
	t.Helper()
	ctx := context.Background()
	if s.status == "" {
		s.status = "verified"
	}
	if s.created.IsZero() {
		s.created = submitNow.Add(-48 * time.Hour)
	}
	if s.updated.IsZero() {
		s.updated = s.created
	}
	if _, err := pool.Exec(ctx, `
		insert into venues (
			id, name, location, address, city, country_code, venue_type,
			verification_status, submitted_by, created_at, updated_at
		) values (
			$1, $2, ST_SetSRID(ST_MakePoint($3::float8, $4::float8), 4326)::geography,
			'1 Metal Street', 'Oslo', 'NO', 'pub', $5, $6::uuid, $7, $8
		)`, s.id, s.name, s.lon, s.lat, s.status, s.submitter, s.created, s.updated); err != nil {
		t.Fatal(err)
	}
	if s.genre == 0 {
		s.genre = genreID(t, pool, "Punk")
	}
	if _, err := pool.Exec(ctx, `insert into venue_genres (venue_id, genre_id) values ($1, $2)`, s.id, s.genre); err != nil {
		t.Fatal(err)
	}
}

func rawVenue(name string, lat, lon float64, genres ...int) RawSubmission {
	address, city, country, venueType := "1 Metal Street", "Oslo", "NO", "pub"
	ids := make([]int64, len(genres))
	for i, genre := range genres {
		ids[i] = int64(genre)
	}
	return RawSubmission{
		Name: &name, Latitude: &lat, Longitude: &lon, Address: &address, City: &city,
		CountryCode: &country, VenueType: &venueType, GenreIDs: ids,
	}
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

func ptr(s string) *string { return &s }
