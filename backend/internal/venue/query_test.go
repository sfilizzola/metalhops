package venue

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func parseFields(t *testing.T, raw RawQuery) map[string]string {
	t.Helper()
	_, err := ParseQuery(raw)
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("raw %+v err %v", raw, err)
	}
	return validation.Fields
}

func TestParseQueryAcceptsEveryFilter(t *testing.T) {
	got, err := ParseQuery(RawQuery{
		Limit: 10, Cursor: "abc", Query: "  Cafe\u0301 Iron  ",
		GenreIDs: []string{"3", "", "1", "3", "01"},
		City:     "Oslo", CountryCode: "XK", VenueType: "club",
		Latitude: "-90", Longitude: "180", RadiusKM: "100",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Query{
		Limit: 10, Cursor: "abc", Text: "Caf\u00e9 Iron", GenreIDs: []int{1, 3},
		City: "Oslo", CountryCode: "XK", VenueType: "club",
		Location: &Location{Latitude: -90, Longitude: 180, RadiusKM: 100},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestParseQueryEmptyMeansAbsent(t *testing.T) {
	got, err := ParseQuery(RawQuery{Limit: 20, GenreIDs: []string{""}})
	if err != nil || !reflect.DeepEqual(got, Query{Limit: 20}) {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestParseQueryBoundaries(t *testing.T) {
	twenty := []string{}
	for i := 1; i <= 20; i++ {
		twenty = append(twenty, strconv.Itoa(i), strconv.Itoa(i))
	}
	for _, raw := range []RawQuery{
		{Limit: 1},
		{Limit: 100},
		{Limit: 20, Query: strings.Repeat("a", 100)},
		{Limit: 20, City: strings.Repeat("a", 120)},
		{Limit: 20, Query: "iron\tmaiden"},
		{Limit: 20, GenreIDs: twenty},
		{Limit: 20, GenreIDs: []string{"2147483647"}},
		{Limit: 20, CountryCode: "GB"},
		{Limit: 20, VenueType: "bar"},
		{Limit: 20, VenueType: "pub"},
		{Limit: 20, Latitude: "90", Longitude: "-180", RadiusKM: "1"},
	} {
		if _, err := ParseQuery(raw); err != nil {
			t.Fatalf("raw %+v err %v", raw, err)
		}
	}
}

func TestParseQueryRejects(t *testing.T) {
	twentyOne := []string{}
	for i := 1; i <= 21; i++ {
		twentyOne = append(twentyOne, strconv.Itoa(i))
	}
	at := func(lat, lon, radius string) RawQuery {
		return RawQuery{Limit: 20, Latitude: lat, Longitude: lon, RadiusKM: radius}
	}
	cases := []struct {
		raw     RawQuery
		field   string
		message string
	}{
		{RawQuery{Limit: 0}, "limit", "must be from 1 to 100"},
		{RawQuery{Limit: 101}, "limit", "must be from 1 to 100"},
		{RawQuery{Limit: 20, Query: "   "}, "query", "must be 1 to 100 characters"},
		{RawQuery{Limit: 20, Query: strings.Repeat("a", 101)}, "query", "must be 1 to 100 characters"},
		{RawQuery{Limit: 20, Query: "iron\nmaiden"}, "query", "must be a single line of text"},
		{RawQuery{Limit: 20, Query: "iron\x00"}, "query", "must be a single line of text"},
		{RawQuery{Limit: 20, Query: "\xff"}, "query", "must be a single line of text"},
		{RawQuery{Limit: 20, City: strings.Repeat("a", 121)}, "city", "must be 1 to 120 characters"},
		{RawQuery{Limit: 20, City: "Os\rlo"}, "city", "must be a single line of text"},
		{RawQuery{Limit: 20, GenreIDs: []string{"0"}}, "genre_id", "must be a positive integer"},
		{RawQuery{Limit: 20, GenreIDs: []string{"-1"}}, "genre_id", "must be a positive integer"},
		{RawQuery{Limit: 20, GenreIDs: []string{"x"}}, "genre_id", "must be a positive integer"},
		{RawQuery{Limit: 20, GenreIDs: []string{"1.5"}}, "genre_id", "must be a positive integer"},
		{RawQuery{Limit: 20, GenreIDs: []string{"2147483648"}}, "genre_id", "must be a positive integer"},
		{RawQuery{Limit: 20, GenreIDs: twentyOne}, "genre_id", "must be provided at most 20 times"},
		{RawQuery{Limit: 20, CountryCode: "gb"}, "country_code", "must be an ISO 3166-1 alpha-2 code"},
		{RawQuery{Limit: 20, CountryCode: "UK"}, "country_code", "must be an ISO 3166-1 alpha-2 code"},
		{RawQuery{Limit: 20, CountryCode: "ZZ"}, "country_code", "must be an ISO 3166-1 alpha-2 code"},
		{RawQuery{Limit: 20, VenueType: "tavern"}, "venue_type", "must be bar, pub, or club"},
		{at("90.0001", "0", "5"), "latitude", "must be from -90 to 90"},
		{at("NaN", "0", "5"), "latitude", "must be from -90 to 90"},
		{at("Inf", "0", "5"), "latitude", "must be from -90 to 90"},
		{at("north", "0", "5"), "latitude", "must be from -90 to 90"},
		{at("0", "-180.5", "5"), "longitude", "must be from -180 to 180"},
		{at("0", "0", "0"), "radius_km", "must be from 1 to 100"},
		{at("0", "0", "101"), "radius_km", "must be from 1 to 100"},
		{at("0", "0", "1.5"), "radius_km", "must be from 1 to 100"},
	}
	for _, c := range cases {
		fields := parseFields(t, c.raw)
		if fields[c.field] != c.message {
			t.Fatalf("raw %+v fields %+v want %s=%q", c.raw, fields, c.field, c.message)
		}
	}
}

func TestParseQueryIncompleteLocation(t *testing.T) {
	fields := parseFields(t, RawQuery{Limit: 20, Latitude: "59.9"})
	if len(fields) != 2 || fields["longitude"] != locationGroup || fields["radius_km"] != locationGroup {
		t.Fatalf("fields %+v", fields)
	}
	fields = parseFields(t, RawQuery{Limit: 20, RadiusKM: "5"})
	if len(fields) != 2 || fields["latitude"] != locationGroup || fields["longitude"] != locationGroup {
		t.Fatalf("fields %+v", fields)
	}
}

func TestParseQueryReportsEveryField(t *testing.T) {
	fields := parseFields(t, RawQuery{Limit: 0, VenueType: "tavern", CountryCode: "UK"})
	if len(fields) != 3 {
		t.Fatalf("fields %+v", fields)
	}
}

func TestSortMode(t *testing.T) {
	location := &Location{Latitude: 0, Longitude: 0, RadiusKM: 5}
	cases := map[sortMode]Query{
		modeName:              {Limit: 20, City: "Oslo", GenreIDs: []int{1}, CountryCode: "NO", VenueType: "bar"},
		modeDistance:          {Limit: 20, Location: location},
		modeRelevanceName:     {Limit: 20, Text: "iron"},
		modeRelevanceDistance: {Limit: 20, Text: "iron", Location: location},
	}
	for want, q := range cases {
		if got := q.mode(); got != want {
			t.Fatalf("query %+v mode %d want %d", q, got, want)
		}
	}
}
