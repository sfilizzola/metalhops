package review

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
)

var reviewNow = time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)

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

func TestListVisibility(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	for id, status := range map[string]string{
		verifiedVenue: "verified", pendingVenue: "pending", rejectedVenue: "rejected",
		delistedVenue: "delisted", bareVenue: "bare",
	} {
		insertVenue(t, pool, id, status)
	}
	insertUser(t, pool, userA, "active", nil)
	insertUser(t, pool, userB, "active", nil)
	older := insertReview(t, pool, verifiedVenue, userA, 4, nil, reviewNow)
	newer := insertReview(t, pool, verifiedVenue, userB, 2, ptr("loud"), reviewNow.Add(time.Minute))
	for _, id := range []string{pendingVenue, rejectedVenue, delistedVenue, bareVenue} {
		insertReview(t, pool, id, userA, 5, nil, reviewNow)
	}
	service := New(pool, nil, nil, time.Now)

	page, err := service.List(ctx, ListQuery{VenueID: verifiedVenue, Limit: 20})
	if err != nil || len(page.Reviews) != 2 || page.NextCursor != nil {
		t.Fatalf("verified %+v %v", page, err)
	}
	if page.Reviews[0].ID != newer || page.Reviews[1].ID != older {
		t.Fatalf("order %s %s", page.Reviews[0].ID, page.Reviews[1].ID)
	}
	first := page.Reviews[0]
	if first.Rating != 2 || first.Commentary == nil || *first.Commentary != "loud" ||
		!first.CreatedAt.Equal(reviewNow.Add(time.Minute)) || first.CreatedAt.Location() != time.UTC || first.UpdatedAt.Location() != time.UTC {
		t.Fatalf("review %+v", first)
	}

	page, err = service.List(ctx, ListQuery{VenueID: pendingVenue, Limit: 20})
	if err != nil || page.Reviews == nil || len(page.Reviews) != 0 || page.NextCursor != nil {
		t.Fatalf("pending %+v %v", page, err)
	}
	for _, id := range []string{rejectedVenue, delistedVenue, bareVenue, unknownVenue} {
		if _, err := service.List(ctx, ListQuery{VenueID: id, Limit: 20}); !errors.Is(err, ErrVenueNotFound) {
			t.Fatalf("hidden %s err %v", id, err)
		}
	}
	var validation *ValidationError
	_, err = service.List(ctx, ListQuery{VenueID: strings.ToUpper(verifiedVenue), Limit: 20})
	if !errors.As(err, &validation) || validation.Fields["venue_id"] != "must be a canonical uuid" {
		t.Fatalf("uppercase err %v", err)
	}
	_, err = service.List(ctx, ListQuery{VenueID: verifiedVenue, Limit: 0})
	if !errors.As(err, &validation) || validation.Fields["limit"] != "must be from 1 to 100" {
		t.Fatalf("limit err %v", err)
	}
}

func TestListPagination(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	insertVenue(t, pool, verifiedVenue, "verified")
	insertVenue(t, pool, pendingVenue, "verified")
	insertUser(t, pool, userA, "active", nil)
	insertUser(t, pool, userB, "active", nil)
	insertUser(t, pool, userC, "active", nil)
	oldest := insertReview(t, pool, verifiedVenue, userA, 3, nil, reviewNow)
	tieA := insertReview(t, pool, verifiedVenue, userB, 4, nil, reviewNow.Add(time.Minute))
	tieB := insertReview(t, pool, verifiedVenue, userC, 5, nil, reviewNow.Add(time.Minute))
	high, low := tieA, tieB
	if low > high {
		high, low = low, high
	}
	service := New(pool, nil, nil, time.Now)

	page, err := service.List(ctx, ListQuery{VenueID: verifiedVenue, Limit: 2})
	if err != nil || len(page.Reviews) != 2 || page.NextCursor == nil {
		t.Fatalf("page 1 %+v %v", page, err)
	}
	if page.Reviews[0].ID != high || page.Reviews[1].ID != low {
		t.Fatalf("page 1 order %s %s", page.Reviews[0].ID, page.Reviews[1].ID)
	}
	next, err := service.List(ctx, ListQuery{VenueID: verifiedVenue, Limit: 2, Cursor: *page.NextCursor})
	if err != nil || len(next.Reviews) != 1 || next.Reviews[0].ID != oldest || next.NextCursor != nil {
		t.Fatalf("page 2 %+v %v", next, err)
	}
	var validation *ValidationError
	_, err = service.List(ctx, ListQuery{VenueID: pendingVenue, Limit: 2, Cursor: *page.NextCursor})
	if !errors.As(err, &validation) || validation.Fields["cursor"] != "invalid" {
		t.Fatalf("foreign cursor err %v", err)
	}
}

func TestListRedactsAuthor(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	insertVenue(t, pool, verifiedVenue, "verified")
	insertUser(t, pool, userA, "active", nil)
	insertUser(t, pool, userB, "suspended", nil)
	insertUser(t, pool, userC, "rename_required", nil)
	active := insertReview(t, pool, verifiedVenue, userA, 4, nil, reviewNow.Add(2*time.Minute))
	insertReview(t, pool, verifiedVenue, userB, 4, nil, reviewNow.Add(time.Minute))
	insertReview(t, pool, verifiedVenue, userC, 4, nil, reviewNow)

	page, err := New(pool, nil, nil, time.Now).List(ctx, ListQuery{VenueID: verifiedVenue, Limit: 20})
	if err != nil || len(page.Reviews) != 3 {
		t.Fatalf("page %+v %v", page, err)
	}
	got := page.Reviews[0].Author
	if page.Reviews[0].ID != active || got.Username == nil || *got.Username != "u"+userA[len(userA)-12:] || got.DisplayName != "Tester" {
		t.Fatalf("active author %+v", got)
	}
	for _, r := range page.Reviews[1:] {
		if r.Author.Username != nil || r.Author.DisplayName != "MetalHops User" {
			t.Fatalf("redacted author %+v", r.Author)
		}
	}
}

func TestGetOwn(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	for id, status := range map[string]string{
		verifiedVenue: "verified", pendingVenue: "pending", delistedVenue: "delisted",
	} {
		insertVenue(t, pool, id, status)
	}
	insertUser(t, pool, userA, "suspended", nil)
	insertUser(t, pool, userB, "active", nil)
	insertUser(t, pool, userC, "", nil)
	id := insertReview(t, pool, verifiedVenue, userA, 4, ptr("cold"), reviewNow)
	insertReview(t, pool, pendingVenue, userA, 4, nil, reviewNow)
	insertReview(t, pool, delistedVenue, userA, 4, nil, reviewNow)
	insertReview(t, pool, verifiedVenue, userC, 4, nil, reviewNow)
	service := New(pool, nil, nil, time.Now)

	own, err := service.GetOwn(ctx, userA, verifiedVenue)
	if err != nil || own.ID != id || own.UserID != userA || own.Rating != 4 || own.Commentary == nil || *own.Commentary != "cold" ||
		own.Username != "u"+userA[len(userA)-12:] || own.DisplayName != "Tester" ||
		!own.CreatedAt.Equal(reviewNow) || own.CreatedAt.Location() != time.UTC || own.UpdatedAt.Location() != time.UTC {
		t.Fatalf("own %+v %v", own, err)
	}
	for _, c := range []struct{ user, venue string }{
		{userB, verifiedVenue},
		{userA, pendingVenue},
		{userA, delistedVenue},
		{userA, unknownVenue},
		{userC, verifiedVenue},
	} {
		if _, err := service.GetOwn(ctx, c.user, c.venue); !errors.Is(err, ErrReviewNotFound) {
			t.Fatalf("%s on %s err %v", c.user, c.venue, err)
		}
	}
	var validation *ValidationError
	_, err = service.GetOwn(ctx, userA, strings.ToUpper(verifiedVenue))
	if !errors.As(err, &validation) || validation.Fields["venue_id"] != "must be a canonical uuid" {
		t.Fatalf("uppercase err %v", err)
	}
}

func TestReviewListIndex(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if _, err := pool.Exec(ctx, `insert into auth.users (id) select gen_random_uuid() from generate_series(1, 500)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into venues (id, name, location, address, city, country_code, verification_status)
		select gen_random_uuid(), 'Venue ' || i, ST_SetSRID(ST_MakePoint((i % 360) - 179.5, ((i / 360) % 170) - 85), 4326)::geography,
		       i || ' Main Street', 'City', 'DE', 'verified'
		from generate_series(1, 1000) as i`); err != nil {
		t.Fatal(err)
	}
	// Pair (i % 500, i / 5) is unique for i in 0..4999, so the (venue, user) key never collides.
	if _, err := pool.Exec(ctx, `
		with users as (select id, row_number() over (order by id) - 1 as n from auth.users),
		     venues_n as (select id, row_number() over (order by id) - 1 as n from venues)
		insert into reviews (venue_id, user_id, rating, created_at, updated_at)
		select venues_n.id, users.id, (i % 5) + 1, now() - (i || ' minutes')::interval, now() - (i || ' minutes')::interval
		from generate_series(0, 4999) as i
		join users on users.n = i % 500
		join venues_n on venues_n.n = i / 5`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "vacuum analyze reviews"); err != nil {
		t.Fatal(err)
	}
	var venueID string
	if err := pool.QueryRow(ctx, `select venue_id::text from reviews limit 1`).Scan(&venueID); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, "explain "+listSQL, venueID, nil, nil, 21)
	if err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line + "\n")
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "reviews_venue_list_idx") {
		t.Fatalf("list plan does not use reviews_venue_list_idx:\n%s", plan.String())
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
		id, "u"+id[len(id)-12:], status, reason, until, reviewNow); err != nil {
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

func insertReview(t *testing.T, pool *pgxpool.Pool, venueID, userID string, rating int, commentary *string, created time.Time) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `
		insert into reviews (venue_id, user_id, rating, commentary, created_at, updated_at)
		values ($1, $2, $3, $4, $5, $5)
		returning id::text`, venueID, userID, rating, commentary, created).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
