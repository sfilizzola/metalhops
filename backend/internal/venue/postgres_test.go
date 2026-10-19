package venue

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
)

func TestListGenresOrderedByName(t *testing.T) {
	service := New(testPool(t))
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
	genres, err := New(pool).ListGenres(ctx)
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
	service := New(testPool(t))
	page, err := service.ListVenues(context.Background(), 20, "")
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

	service := New(pool)
	page, err := service.ListVenues(ctx, 20, "")
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

func TestVenueWithTwoGenresCountsOnce(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	punk := genreID(t, pool, "Punk")
	classic := genreID(t, pool, "Classic rock")
	first := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	second := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	insertVenue(t, pool, first, "Alpha", "verified", classic, punk)
	insertVenue(t, pool, second, "Beta", "pending", punk)
	page, err := New(pool).ListVenues(ctx, 1, "")
	if err != nil || len(page.Venues) != 1 || page.Venues[0].ID != first || len(page.Venues[0].Genres) != 2 || page.NextCursor == nil {
		t.Fatalf("page %+v err %v", page, err)
	}
	next, err := New(pool).ListVenues(ctx, 1, *page.NextCursor)
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
	service := New(pool)
	page, err := service.ListVenues(ctx, 1, "")
	if err != nil || len(page.Venues) != 1 || page.Venues[0].ID != first || page.NextCursor == nil {
		t.Fatalf("first %+v err %v", page, err)
	}
	if _, err = pool.Exec(ctx, `delete from venues where id = $1`, first); err != nil {
		t.Fatal(err)
	}
	next, err := service.ListVenues(ctx, 1, *page.NextCursor)
	if err != nil || len(next.Venues) != 1 || next.Venues[0].ID != second || next.NextCursor == nil {
		t.Fatalf("second %+v err %v", next, err)
	}
	last, err := service.ListVenues(ctx, 1, *next.NextCursor)
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
	page, err := New(pool).ListVenues(ctx, 20, "")
	if err != nil || len(page.Venues) != 3 {
		t.Fatal(err)
	}
	if page.Venues[0].ID != low || page.Venues[1].ID != high || page.Venues[2].ID != article {
		t.Fatalf("order %s %s %s", page.Venues[0].ID, page.Venues[1].ID, page.Venues[2].ID)
	}
}

func TestUppercaseVenueIDIsInvalid(t *testing.T) {
	service := New(testPool(t))
	_, err := service.GetVenue(context.Background(), "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA")
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Fields["venue_id"] != "must be a canonical uuid" || errors.Is(err, ErrNotFound) {
		t.Fatalf("err %v", err)
	}
	_, err = service.ListVenues(context.Background(), 0, "")
	if !errors.As(err, &validation) || validation.Fields["limit"] != "must be from 1 to 100" {
		t.Fatalf("limit err %v", err)
	}
	_, err = service.ListVenues(context.Background(), 20, "abc")
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
	if _, err := pool.Exec(ctx, `
		insert into venues (
			id, name, description, location, address, city, country_code, venue_type, verification_status
		) values (
			$1, $2, 'A metal bar', ST_MakePoint(10, 60)::geography,
			'1 Metal Street', 'Oslo', 'DE', 'bar', $3
		)`, id, name, status); err != nil {
		t.Fatal(err)
	}
	for _, genreID := range genreIDs {
		if _, err := pool.Exec(ctx, `insert into venue_genres (venue_id, genre_id) values ($1, $2)`, id, genreID); err != nil {
			t.Fatal(err)
		}
	}
}
