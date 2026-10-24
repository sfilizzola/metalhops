package venue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
)

func TestListGenresOrderedByName(t *testing.T) {
	service := New(testPool(t), nil, nil, time.Now)
	genres, err := service.ListGenres(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Black metal", "Classic rock", "Death metal", "Folk metal", "Grunge",
		"Hard rock", "Heavy metal", "Punk", "Thrash metal",
	}
	if len(genres) != len(want) {
		t.Fatalf("genres %d", len(genres))
	}
	for i, name := range want {
		if genres[i].Name != name || genres[i].ID == 0 {
			t.Fatalf("genre %d %+v", i, genres[i])
		}
	}
}

func TestGenreOrderUsesCCollation(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if _, err := pool.Exec(ctx, `insert into genres (name) values ('alpha test'), ('Zulu test')`); err != nil {
		t.Fatal(err)
	}
	genres, err := New(pool, nil, nil, time.Now).ListGenres(ctx)
	if err != nil {
		t.Fatal(err)
	}
	positions := map[string]int{}
	for i, genre := range genres {
		positions[genre.Name] = i
	}
	if positions["Zulu test"] >= positions["alpha test"] {
		t.Fatalf("C order positions %+v", positions)
	}
}

func TestEmptyCatalogue(t *testing.T) {
	service := New(testPool(t), nil, nil, time.Now)
	page, err := service.ListVenues(context.Background(), Query{Limit: 20})
	if err != nil || page.Venues == nil || len(page.Venues) != 0 || page.NextCursor != nil {
		t.Fatalf("page %+v err %v", page, err)
	}
}

func TestCatalogueMembershipAndAggregates(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	punk := genreID(t, pool, "Punk")
	classic := genreID(t, pool, "Classic rock")
	verified := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	pending := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	rejected := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	bare := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	insertVenue(t, pool, verified, "Alpha", "verified", classic, punk)
	insertVenue(t, pool, pending, "Beta", "pending", punk)
	insertVenue(t, pool, rejected, "Gamma", "rejected", punk)
	insertVenue(t, pool, bare, "Delta", "verified")

	service := New(pool, nil, nil, time.Now)
	page, err := service.ListVenues(ctx, Query{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Venues) != 2 || page.NextCursor != nil {
		t.Fatalf("page %+v", page.Venues)
	}
	if page.Venues[0].ID != verified || page.Venues[1].ID != pending {
		t.Fatalf("order %s %s", page.Venues[0].ID, page.Venues[1].ID)
	}
	if page.Venues[0].Latitude != 60 || page.Venues[0].Longitude != 10 {
		t.Fatalf("point %v %v", page.Venues[0].Latitude, page.Venues[0].Longitude)
	}
	if len(page.Venues[0].Genres) != 2 || page.Venues[0].Genres[0].Name != "Classic rock" || page.Venues[0].Genres[1].Name != "Punk" {
		t.Fatalf("genres %+v", page.Venues[0].Genres)
	}
	for _, got := range page.Venues {
		if got.AverageRating != nil || got.ReviewCount != 0 || got.CheckInCount != 0 {
			t.Fatalf("aggregates %+v", got)
		}
		if got.CountryCode != "DE" || got.Address != "1 Metal Street" {
			t.Fatalf("place %+v", got)
		}
	}
	got, err := service.GetVenue(ctx, verified)
	if err != nil || got.ID != verified || got.VerificationStatus != "verified" {
		t.Fatalf("detail %+v %v", got, err)
	}
	for _, hidden := range []string{rejected, bare, "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"} {
		_, err = service.GetVenue(ctx, hidden)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("hidden %s err %v", hidden, err)
		}
	}
}

func TestRatingAggregates(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	punk := genreID(t, pool, "Punk")
	verified := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	pending := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	insertVenue(t, pool, verified, "Alpha", "verified", punk)
	insertVenue(t, pool, pending, "Beta", "pending", punk)

	users := []string{
		"00000000-0000-4000-8000-000000000001",
		"00000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000003",
		"00000000-0000-4000-8000-000000000004",
	}
	for _, id := range users {
		insertUser(t, pool, id, "", nil)
	}

	service := New(pool, nil, nil, time.Now)
	assertAggregate := func(label, venueID string, wantAvg *float64, wantCount int) {
		t.Helper()
		page, err := service.ListVenues(ctx, Query{Limit: 20})
		if err != nil {
			t.Fatalf("%s: list err %v", label, err)
		}
		var listed *Venue
		for i := range page.Venues {
			if page.Venues[i].ID == venueID {
				listed = &page.Venues[i]
			}
		}
		if listed == nil {
			t.Fatalf("%s: venue missing from list", label)
		}
		checkAggregate(t, label+" list", *listed, wantAvg, wantCount)
		got, err := service.GetVenue(ctx, venueID)
		if err != nil {
			t.Fatalf("%s: detail err %v", label, err)
		}
		checkAggregate(t, label+" detail", got, wantAvg, wantCount)
	}

	assertAggregate("no reviews", verified, nil, 0)

	ratings := []int{1, 1, 1, 2}
	for i, rating := range ratings {
		insertReview(t, pool, verified, users[i], rating)
	}
	oneThree := 1.3
	assertAggregate("four reviews", verified, &oneThree, 4)

	if _, err := pool.Exec(ctx, `update reviews set commentary = null, commentary_removed_at = now() where venue_id = $1`, verified); err != nil {
		t.Fatal(err)
	}
	assertAggregate("after commentary removed", verified, &oneThree, 4)

	insertReview(t, pool, pending, users[0], 5)
	assertAggregate("pending with seeded review", pending, nil, 0)

	if _, err := pool.Exec(ctx, `delete from reviews where venue_id = $1`, verified); err != nil {
		t.Fatal(err)
	}
	assertAggregate("after delete", verified, nil, 0)

	insertReview(t, pool, verified, users[0], 5)
	if _, err := pool.Exec(ctx, `update venues set verification_status = 'delisted', delisting_reason = 'duplicate' where id = $1`, verified); err != nil {
		t.Fatal(err)
	}
	page, err := service.ListVenues(ctx, Query{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range page.Venues {
		if v.ID == verified {
			t.Fatalf("delisted venue present in list")
		}
	}
	if _, err := service.GetVenue(ctx, verified); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delisted detail err %v", err)
	}

	if _, err := pool.Exec(ctx, `update venues set verification_status = 'verified', delisting_reason = null where id = $1`, verified); err != nil {
		t.Fatal(err)
	}
	five := 5.0
	assertAggregate("reopened", verified, &five, 1)
}

func checkAggregate(t *testing.T, label string, got Venue, wantAvg *float64, wantCount int) {
	t.Helper()
	switch {
	case wantAvg == nil && got.AverageRating != nil:
		t.Fatalf("%s: average rating %v, want nil", label, *got.AverageRating)
	case wantAvg != nil && (got.AverageRating == nil || *got.AverageRating != *wantAvg):
		t.Fatalf("%s: average rating %v, want %v", label, got.AverageRating, *wantAvg)
	}
	if got.ReviewCount != wantCount {
		t.Fatalf("%s: review count %d, want %d", label, got.ReviewCount, wantCount)
	}
}

func TestCheckInCountAggregates(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	punk := genreID(t, pool, "Punk")
	verified := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	pending := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	insertVenue(t, pool, verified, "Alpha", "verified", punk)
	insertVenue(t, pool, pending, "Beta", "pending", punk)

	users := []string{
		"00000000-0000-4000-8000-000000000001",
		"00000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000003",
		"00000000-0000-4000-8000-000000000004",
	}
	for _, id := range users {
		insertUser(t, pool, id, "", nil)
	}

	service := New(pool, nil, nil, time.Now)
	listed := func(venueID string) *Venue {
		t.Helper()
		page, err := service.ListVenues(ctx, Query{Limit: 20})
		if err != nil {
			t.Fatalf("list err %v", err)
		}
		for i := range page.Venues {
			if page.Venues[i].ID == venueID {
				return &page.Venues[i]
			}
		}
		return nil
	}
	assertCount := func(label, venueID string, want int) {
		t.Helper()
		got := listed(venueID)
		if got == nil {
			t.Fatalf("%s: venue missing from list", label)
		}
		detail, err := service.GetVenue(ctx, venueID)
		if err != nil {
			t.Fatalf("%s: detail err %v", label, err)
		}
		for _, v := range []Venue{*got, detail} {
			if v.CheckInCount != want {
				t.Fatalf("%s: check-in count %d, want %d", label, v.CheckInCount, want)
			}
			checkAggregate(t, label, v, nil, 0)
		}
	}
	assertHidden := func(label, venueID string) {
		t.Helper()
		if listed(venueID) != nil {
			t.Fatalf("%s: venue present in list", label)
		}
		if _, err := service.GetVenue(ctx, venueID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s: detail err %v", label, err)
		}
	}

	assertCount("verified no check-ins", verified, 0)
	assertCount("pending no check-ins", pending, 0)

	for _, user := range users[:3] {
		insertCheckIn(t, pool, verified, user)
	}
	for _, user := range users[2:] {
		insertCheckIn(t, pool, pending, user)
	}
	assertCount("verified three", verified, 3)
	assertCount("pending two", pending, 2)

	if _, err := pool.Exec(ctx, `delete from check_ins where venue_id = $1 and user_id = $2`, verified, users[0]); err != nil {
		t.Fatal(err)
	}
	assertCount("verified after delete", verified, 2)

	if _, err := pool.Exec(ctx, `update venues set verification_status = 'delisted', delisting_reason = 'duplicate' where id = $1`, verified); err != nil {
		t.Fatal(err)
	}
	assertHidden("delisted", verified)
	if _, err := pool.Exec(ctx, `update venues set verification_status = 'verified', delisting_reason = null where id = $1`, verified); err != nil {
		t.Fatal(err)
	}
	assertCount("reopened", verified, 2)

	if _, err := pool.Exec(ctx, `update venues set verification_status = 'rejected', rejection_reason = 'other', rejection_note = 'seed' where id = $1`, pending); err != nil {
		t.Fatal(err)
	}
	assertHidden("rejected", pending)
	if _, err := pool.Exec(ctx, `update venues set verification_status = 'pending', rejection_reason = null, rejection_note = null where id = $1`, pending); err != nil {
		t.Fatal(err)
	}
	assertCount("pending again", pending, 2)

	if _, err := pool.Exec(ctx, `delete from check_ins where venue_id = $1`, verified); err != nil {
		t.Fatal(err)
	}
	assertCount("verified emptied", verified, 0)
}

func insertCheckIn(t *testing.T, pool *pgxpool.Pool, venueID, userID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		insert into check_ins (venue_id, user_id, visited_at, recorded_at)
		values ($1, $2, now(), now())`, venueID, userID); err != nil {
		t.Fatal(err)
	}
}

func insertReview(t *testing.T, pool *pgxpool.Pool, venueID, userID string, rating int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		insert into reviews (venue_id, user_id, rating, created_at, updated_at)
		values ($1, $2, $3, now(), now())`, venueID, userID, rating); err != nil {
		t.Fatal(err)
	}
}

func TestVenueWithTwoGenresCountsOnce(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	punk := genreID(t, pool, "Punk")
	classic := genreID(t, pool, "Classic rock")
	first := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	second := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	insertVenue(t, pool, first, "Alpha", "verified", classic, punk)
	insertVenue(t, pool, second, "Beta", "pending", punk)
	page, err := New(pool, nil, nil, time.Now).ListVenues(ctx, Query{Limit: 1})
	if err != nil || len(page.Venues) != 1 || page.Venues[0].ID != first || len(page.Venues[0].Genres) != 2 || page.NextCursor == nil {
		t.Fatalf("page %+v err %v", page, err)
	}
	next, err := New(pool, nil, nil, time.Now).ListVenues(ctx, Query{Limit: 1, Cursor: *page.NextCursor})
	if err != nil || len(next.Venues) != 1 || next.Venues[0].ID != second || next.NextCursor != nil {
		t.Fatalf("next %+v err %v", next, err)
	}
}

func TestEmptyNameKeyCursorSurvivesDeletion(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	punk := genreID(t, pool, "Punk")
	first := "00000000-0000-4000-8000-000000000001"
	second := "00000000-0000-4000-8000-000000000002"
	third := "00000000-0000-4000-8000-000000000003"
	insertVenue(t, pool, first, "!!!", "verified", punk)
	insertVenue(t, pool, second, "???", "verified", punk)
	insertVenue(t, pool, third, "Alpha", "verified", punk)
	service := New(pool, nil, nil, time.Now)
	page, err := service.ListVenues(ctx, Query{Limit: 1})
	if err != nil || len(page.Venues) != 1 || page.Venues[0].ID != first || page.NextCursor == nil {
		t.Fatalf("first %+v err %v", page, err)
	}
	if _, err = pool.Exec(ctx, `delete from venues where id = $1`, first); err != nil {
		t.Fatal(err)
	}
	next, err := service.ListVenues(ctx, Query{Limit: 1, Cursor: *page.NextCursor})
	if err != nil || len(next.Venues) != 1 || next.Venues[0].ID != second || next.NextCursor == nil {
		t.Fatalf("second %+v err %v", next, err)
	}
	last, err := service.ListVenues(ctx, Query{Limit: 1, Cursor: *next.NextCursor})
	if err != nil || len(last.Venues) != 1 || last.Venues[0].ID != third || last.NextCursor != nil {
		t.Fatalf("third %+v err %v", last, err)
	}
}

func TestNameSortAndTieBreak(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	punk := genreID(t, pool, "Punk")
	low := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	high := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	article := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	insertVenue(t, pool, high, "Bar!", "verified", punk)
	insertVenue(t, pool, low, "bar", "pending", punk)
	insertVenue(t, pool, article, "The Bar", "verified", punk)
	page, err := New(pool, nil, nil, time.Now).ListVenues(ctx, Query{Limit: 20})
	if err != nil || len(page.Venues) != 3 {
		t.Fatal(err)
	}
	if page.Venues[0].ID != low || page.Venues[1].ID != high || page.Venues[2].ID != article {
		t.Fatalf("order %s %s %s", page.Venues[0].ID, page.Venues[1].ID, page.Venues[2].ID)
	}
}

func TestUppercaseVenueIDIsInvalid(t *testing.T) {
	service := New(testPool(t), nil, nil, time.Now)
	_, err := service.GetVenue(context.Background(), "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA")
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Fields["venue_id"] != "must be a canonical uuid" || errors.Is(err, ErrNotFound) {
		t.Fatalf("err %v", err)
	}
	_, err = service.ListVenues(context.Background(), Query{Limit: 0})
	if !errors.As(err, &validation) || validation.Fields["limit"] != "must be from 1 to 100" {
		t.Fatalf("limit err %v", err)
	}
	_, err = service.ListVenues(context.Background(), Query{Limit: 20, Cursor: "abc"})
	if !errors.As(err, &validation) || validation.Fields["cursor"] != "invalid" {
		t.Fatalf("cursor err %v", err)
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

func genreID(t *testing.T, pool *pgxpool.Pool, name string) int {
	t.Helper()
	var id int
	if err := pool.QueryRow(context.Background(), `select id from genres where name = $1`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertVenue(t *testing.T, pool *pgxpool.Pool, id, name, status string, genreIDs ...int) {
	t.Helper()
	ctx := context.Background()
	var rejectionReason, rejectionNote *string
	if status == "rejected" {
		rejectionReason, rejectionNote = ptr("other"), ptr("seed")
	}
	if _, err := pool.Exec(ctx, `
		insert into venues (
			id, name, description, location, address, city, country_code, venue_type,
			verification_status, rejection_reason, rejection_note
		) values (
			$1, $2, 'A metal bar', ST_MakePoint(10, 60)::geography,
			'1 Metal Street', 'Oslo', 'DE', 'bar', $3, $4, $5
		)`, id, name, status, rejectionReason, rejectionNote); err != nil {
		t.Fatal(err)
	}
	for _, genreID := range genreIDs {
		if _, err := pool.Exec(ctx, `insert into venue_genres (venue_id, genre_id) values ($1, $2)`, id, genreID); err != nil {
			t.Fatal(err)
		}
	}
}
