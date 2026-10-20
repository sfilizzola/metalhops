package venue

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func vid(n int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
}

type place struct {
	id        string
	name      string
	address   string
	city      string
	country   string
	venueType string
	lon       float64
	lat       float64
	genres    []int
}

func insertPlace(t *testing.T, pool *pgxpool.Pool, p place) {
	t.Helper()
	ctx := context.Background()
	if p.address == "" {
		p.address = "1 Main Street"
	}
	if p.city == "" {
		p.city = "Oslo"
	}
	if p.country == "" {
		p.country = "NO"
	}
	if p.venueType == "" {
		p.venueType = "bar"
	}
	if len(p.genres) == 0 {
		p.genres = []int{genreID(t, pool, "Punk")}
	}
	if _, err := pool.Exec(ctx, `
		insert into venues (id, name, location, address, city, country_code, venue_type, verification_status)
		values ($1, $2, ST_SetSRID(ST_MakePoint($3, $4), 4326)::geography, $5, $6, $7, $8, 'verified')`,
		p.id, p.name, p.lon, p.lat, p.address, p.city, p.country, p.venueType); err != nil {
		t.Fatal(err)
	}
	for _, genre := range p.genres {
		if _, err := pool.Exec(ctx, `insert into venue_genres (venue_id, genre_id) values ($1, $2)`, p.id, genre); err != nil {
			t.Fatal(err)
		}
	}
}

func discover(t *testing.T, service *Service, raw RawQuery) Page {
	t.Helper()
	if raw.Limit == 0 {
		raw.Limit = 100
	}
	q, err := ParseQuery(raw)
	if err != nil {
		t.Fatalf("parse %+v err %v", raw, err)
	}
	page, err := service.ListVenues(context.Background(), q)
	if err != nil {
		t.Fatalf("list %+v err %v", raw, err)
	}
	return page
}

func pageIDs(page Page) []string {
	ids := []string{}
	for _, got := range page.Venues {
		ids = append(ids, got.ID)
	}
	return ids
}

func assertIDs(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func distanceFromOrigin(t *testing.T, pool *pgxpool.Pool, id string) float64 {
	t.Helper()
	var distance float64
	if err := pool.QueryRow(context.Background(), `
		select ST_Distance(location, ST_SetSRID(ST_MakePoint(0, 0), 4326)::geography)
		from venues where id = $1`, id).Scan(&distance); err != nil {
		t.Fatal(err)
	}
	return distance
}

func TestDiscoveryFilters(t *testing.T) {
	pool := testPool(t)
	punk := genreID(t, pool, "Punk")
	classic := genreID(t, pool, "Classic rock")
	heavy := genreID(t, pool, "Heavy metal")
	insertPlace(t, pool, place{id: vid(1), name: "Iron Pub", city: "Oslo", country: "NO", venueType: "pub", lon: 10.75, lat: 59.91, genres: []int{punk}})
	insertPlace(t, pool, place{id: vid(2), name: "Rust Bar", city: "Bergen", country: "NO", venueType: "bar", lon: 5.32, lat: 60.39, genres: []int{classic}})
	insertPlace(t, pool, place{id: vid(3), name: "Metal Club", city: "Berlin", country: "DE", venueType: "club", lon: 13.40, lat: 52.52, genres: []int{punk, classic}})
	insertPlace(t, pool, place{id: vid(4), name: "Black Hole", city: "Oslo", country: "NO", venueType: "bar", lon: 10.76, lat: 59.92, genres: []int{heavy}})
	service := New(pool, nil, nil, time.Now)
	g := strconv.Itoa

	assertIDs(t, pageIDs(discover(t, service, RawQuery{})), vid(4), vid(1), vid(3), vid(2))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{GenreIDs: []string{g(punk)}})), vid(1), vid(3))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{GenreIDs: []string{g(punk), g(heavy)}})), vid(4), vid(1), vid(3))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{GenreIDs: []string{"999999"}})))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{City: "oslo"})), vid(4), vid(1))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{CountryCode: "DE"})), vid(3))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{VenueType: "pub"})), vid(1))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{City: "Oslo", VenueType: "bar"})), vid(4))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{GenreIDs: []string{g(classic)}, CountryCode: "NO"})), vid(2))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{Query: "metal"})), vid(3))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{Latitude: "59.91", Longitude: "10.75", RadiusKM: "5"})), vid(1), vid(4))
}

func TestRadiusBoundary(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	punk := genreID(t, pool, "Punk")
	for i, meters := range []float64{999.5, 1000, 1000.5} {
		if _, err := pool.Exec(ctx, `
			insert into venues (id, name, location, address, city, country_code, verification_status)
			values ($1, $2, ST_Project(ST_SetSRID(ST_MakePoint(0, 0), 4326)::geography, $3::float8, radians(90)),
			        '1 Main Street', 'Oslo', 'NO', 'verified')`,
			vid(i+1), fmt.Sprintf("Ring %d", i), meters); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `insert into venue_genres (venue_id, genre_id) values ($1, $2)`, vid(i+1), punk); err != nil {
			t.Fatal(err)
		}
	}
	page := discover(t, New(pool, nil, nil, time.Now), RawQuery{Latitude: "0", Longitude: "0", RadiusKM: "1"})
	included := map[string]bool{}
	for _, got := range page.Venues {
		included[got.ID] = true
	}
	if !included[vid(1)] || included[vid(3)] {
		t.Fatalf("inside/outside included %v", included)
	}
	edge := distanceFromOrigin(t, pool, vid(2))
	if included[vid(2)] != (edge <= 1000) {
		t.Fatalf("venue at %v m included %v", edge, included[vid(2)])
	}
}

func TestDistanceOrderAndMeters(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	insertPlace(t, pool, place{id: vid(3), name: "Far", lon: 0.003, lat: 0})
	insertPlace(t, pool, place{id: vid(2), name: "Twin B", lon: 0, lat: 0.001})
	insertPlace(t, pool, place{id: vid(1), name: "Twin A", lon: 0, lat: 0.001})
	insertPlace(t, pool, place{id: vid(4), name: "Outside", lon: 0.02, lat: 0})
	service := New(pool, nil, nil, time.Now)
	page := discover(t, service, RawQuery{Latitude: "0", Longitude: "0", RadiusKM: "1"})
	assertIDs(t, pageIDs(page), vid(1), vid(2), vid(3))
	for _, got := range page.Venues {
		want := int(math.Floor(distanceFromOrigin(t, pool, got.ID) + 0.5))
		if got.DistanceMeters == nil || *got.DistanceMeters != want {
			t.Fatalf("%s distance %v want %d", got.ID, got.DistanceMeters, want)
		}
	}
	for _, got := range discover(t, service, RawQuery{}).Venues {
		if got.DistanceMeters != nil {
			t.Fatalf("distance without location %+v", got)
		}
	}
	detail, err := service.GetVenue(ctx, vid(1))
	if err != nil || detail.DistanceMeters != nil {
		t.Fatalf("detail %+v err %v", detail, err)
	}
}

func TestRelevanceTiers(t *testing.T) {
	pool := testPool(t)
	insertPlace(t, pool, place{id: vid(1), name: "Iron"})
	insertPlace(t, pool, place{id: vid(2), name: "Iron Bar"})
	insertPlace(t, pool, place{id: vid(3), name: "Ironbar"})
	insertPlace(t, pool, place{id: vid(4), name: "The Iron"})
	insertPlace(t, pool, place{id: vid(5), name: "Rust", city: "Iron"})
	insertPlace(t, pool, place{id: vid(6), name: "Pub", address: "1 Ironside Road"})
	insertPlace(t, pool, place{id: vid(7), name: "Bar", city: "Ironton"})
	insertPlace(t, pool, place{id: vid(8), name: "Rust Pub"})
	page := discover(t, New(pool, nil, nil, time.Now), RawQuery{Query: "iron"})
	// Tiers 0, 1, 1, 2, 3, 4, 4. "iron bar" sorts before "ironbar" under C collation.
	assertIDs(t, pageIDs(page), vid(1), vid(2), vid(3), vid(4), vid(5), vid(7), vid(6))
}

func TestRelevanceDistanceTieBreak(t *testing.T) {
	pool := testPool(t)
	insertPlace(t, pool, place{id: vid(1), name: "Iron", lon: 0.5})
	insertPlace(t, pool, place{id: vid(2), name: "Iron Bar", lon: 0.2})
	insertPlace(t, pool, place{id: vid(3), name: "Ironbar", lon: 0.1})
	page := discover(t, New(pool, nil, nil, time.Now), RawQuery{Query: "iron", Latitude: "0", Longitude: "0", RadiusKM: "100"})
	assertIDs(t, pageIDs(page), vid(1), vid(3), vid(2))
}

func TestTextAndCityFolding(t *testing.T) {
	pool := testPool(t)
	insertPlace(t, pool, place{id: vid(1), name: "Iron-Maiden Pub", city: "Saint-Étienne", country: "FR"})
	insertPlace(t, pool, place{id: vid(2), name: "Other", city: "Saint Etienne", country: "FR"})
	insertPlace(t, pool, place{id: vid(3), name: "Third", city: "Lyon", country: "FR"})
	service := New(pool, nil, nil, time.Now)
	assertIDs(t, pageIDs(discover(t, service, RawQuery{City: "saint etienne"})), vid(1), vid(2))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{City: "SAINT-ÉTIENNE"})), vid(1), vid(2))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{City: "Saint"})))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{Query: "iron maiden"})), vid(1))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{Query: "IRON-MAIDEN"})), vid(1))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{Query: "ironmaidn"})))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{Query: "st etienne"})))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{Query: "étienne"})), vid(1), vid(2))
}

func TestPunctuationOnlyText(t *testing.T) {
	service := New(testPool(t), nil, nil, time.Now)
	for field, raw := range map[string]RawQuery{
		"query": {Limit: 20, Query: "!!!"},
		"city":  {Limit: 20, City: "---"},
	} {
		q, err := ParseQuery(raw)
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.ListVenues(context.Background(), q)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Fields[field] != "must contain a letter or digit" {
			t.Fatalf("%s err %v", field, err)
		}
	}
}

func TestWalkEveryMode(t *testing.T) {
	pool := testPool(t)
	insertPlace(t, pool, place{id: vid(1), name: "Iron", lat: 0.001})
	insertPlace(t, pool, place{id: vid(2), name: "Iron", lat: 0.001})
	insertPlace(t, pool, place{id: vid(3), name: "Iron Bar", lon: 0.002})
	insertPlace(t, pool, place{id: vid(4), name: "Ironbar", lon: 0.002})
	insertPlace(t, pool, place{id: vid(5), name: "Pub", address: "1 Ironside Road", lon: 0.003})
	insertPlace(t, pool, place{id: vid(6), name: "Rust", city: "Iron", lon: 0.003})
	insertPlace(t, pool, place{id: vid(7), name: "!!!", lon: 0.004})
	service := New(pool, nil, nil, time.Now)
	modes := map[string]RawQuery{
		"name":               {},
		"distance":           {Latitude: "0", Longitude: "0", RadiusKM: "100"},
		"relevance-name":     {Query: "iron"},
		"relevance-distance": {Query: "iron", Latitude: "0", Longitude: "0", RadiusKM: "100"},
	}
	for name, raw := range modes {
		full := pageIDs(discover(t, service, raw))
		if len(full) < 5 {
			t.Fatalf("%s full %v", name, full)
		}
		walked := []string{}
		cursor := ""
		for i := 0; i < 20; i++ {
			step := raw
			step.Limit = 2
			step.Cursor = cursor
			page := discover(t, service, step)
			walked = append(walked, pageIDs(page)...)
			if page.NextCursor == nil {
				break
			}
			cursor = *page.NextCursor
		}
		assertIDs(t, walked, full...)
	}
}

func TestCursorBoundToFilters(t *testing.T) {
	pool := testPool(t)
	insertPlace(t, pool, place{id: vid(1), name: "Alpha", country: "NO"})
	insertPlace(t, pool, place{id: vid(2), name: "Beta", country: "NO"})
	insertPlace(t, pool, place{id: vid(3), name: "Gamma", country: "DE"})
	service := New(pool, nil, nil, time.Now)
	first := discover(t, service, RawQuery{Limit: 1, CountryCode: "NO"})
	if first.NextCursor == nil {
		t.Fatal("no cursor")
	}
	cursor := *first.NextCursor
	assertIDs(t, pageIDs(discover(t, service, RawQuery{Limit: 5, CountryCode: "NO", Cursor: cursor})), vid(2))
	for _, raw := range []RawQuery{
		{Limit: 1, CountryCode: "DE", Cursor: cursor},
		{Limit: 1, Cursor: cursor},
		{Limit: 1, CountryCode: "NO", Query: "beta", Cursor: cursor},
	} {
		q, err := ParseQuery(raw)
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.ListVenues(context.Background(), q)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Fields["cursor"] != "invalid" {
			t.Fatalf("raw %+v err %v", raw, err)
		}
	}
}
