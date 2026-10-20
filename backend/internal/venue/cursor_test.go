package venue

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

const testHash = "0123456789abcdef"

func TestCursorRoundTripEveryMode(t *testing.T) {
	id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	cases := []struct {
		mode sortMode
		p    position
	}{
		{modeName, position{NameKey: "the bar", ID: id}},
		{modeName, position{NameKey: "", ID: id}},
		{modeDistance, position{Distance: 0.1 + 0.2, ID: id}},
		{modeDistance, position{Distance: 0, ID: id}},
		{modeDistance, position{Distance: 1e6, ID: id}},
		{modeRelevanceName, position{Tier: 4, NameKey: "бар no1", ID: id}},
		{modeRelevanceName, position{Tier: 0, NameKey: "", ID: id}},
		{modeRelevanceDistance, position{Tier: 3, Distance: 123456.78901234567, ID: id}},
	}
	for _, c := range cases {
		got, err := decodeCursor(encodeCursor(c.mode, testHash, c.p), c.mode, testHash)
		if err != nil || got != c.p {
			t.Fatalf("mode %d got %+v want %+v err %v", c.mode, got, c.p, err)
		}
	}
}

func TestCursorRejects(t *testing.T) {
	id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	raw := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	cases := []struct {
		name   string
		cursor string
		mode   sortMode
	}{
		{"not base64", "!!!", modeName},
		{"other hash", encodeCursor(modeName, "fedcba9876543210", position{NameKey: "bar", ID: id}), modeName},
		{"other mode", encodeCursor(modeDistance, testHash, position{Distance: 1, ID: id}), modeName},
		{"session 7", raw("name-v1|bar|" + id), modeName},
		{"too few fields", raw("name-v2|" + testHash + "|bar"), modeName},
		{"too many fields", raw("name-v2|" + testHash + "|bar|baz|" + id), modeName},
		{"uppercase id", raw("name-v2|" + testHash + "|bar|" + strings.ToUpper(id)), modeName},
		{"bad utf8", raw("name-v2|" + testHash + "|\xff|" + id), modeName},
		{"null byte", raw("name-v2|" + testHash + "|b\x00r|" + id), modeName},
		{"negative distance", raw("distance-v1|" + testHash + "|-1|" + id), modeDistance},
		{"nan distance", raw("distance-v1|" + testHash + "|NaN|" + id), modeDistance},
		{"inf distance", raw("distance-v1|" + testHash + "|+Inf|" + id), modeDistance},
		{"word distance", raw("distance-v1|" + testHash + "|far|" + id), modeDistance},
		{"tier 5", raw("relevance-name-v1|" + testHash + "|5|bar|" + id), modeRelevanceName},
		{"tier 10", raw("relevance-name-v1|" + testHash + "|10|bar|" + id), modeRelevanceName},
		{"tier word", raw("relevance-distance-v1|" + testHash + "|x|1|" + id), modeRelevanceDistance},
	}
	for _, c := range cases {
		_, err := decodeCursor(c.cursor, c.mode, testHash)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Fields["cursor"] != "invalid" {
			t.Fatalf("%s err %v", c.name, err)
		}
	}
}

func TestFilterHash(t *testing.T) {
	parse := func(raw RawQuery) Query {
		t.Helper()
		raw.Limit = 20
		q, err := ParseQuery(raw)
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	base := parse(RawQuery{GenreIDs: []string{"3", "1"}, CountryCode: "NO"})
	same := parse(RawQuery{GenreIDs: []string{"1", "3", "1"}, CountryCode: "NO"})
	same.Limit = 50
	same.Cursor = "ignored"
	hash := filterHash(modeName, base, "", "")
	if len(hash) != 16 || strings.ToLower(hash) != hash || filterHash(modeName, same, "", "") != hash {
		t.Fatalf("hash %q same %q", hash, filterHash(modeName, same, "", ""))
	}
	location := RawQuery{GenreIDs: []string{"1", "3"}, CountryCode: "NO", Latitude: "1", Longitude: "2", RadiusKM: "5"}
	wider := location
	wider.RadiusKM = "6"
	others := []string{
		filterHash(modeName, parse(RawQuery{GenreIDs: []string{"1"}, CountryCode: "NO"}), "", ""),
		filterHash(modeName, parse(RawQuery{GenreIDs: []string{"1", "3"}, CountryCode: "DE"}), "", ""),
		filterHash(modeName, parse(RawQuery{GenreIDs: []string{"1", "3"}, CountryCode: "NO", VenueType: "pub"}), "", ""),
		filterHash(modeName, base, "", "oslo"),
		filterHash(modeRelevanceName, base, "iron", ""),
		filterHash(modeDistance, parse(location), "", ""),
		filterHash(modeDistance, parse(wider), "", ""),
	}
	seen := map[string]bool{hash: true}
	for i, other := range others {
		if seen[other] {
			t.Fatalf("hash %d collides: %s", i, other)
		}
		seen[other] = true
	}
}
