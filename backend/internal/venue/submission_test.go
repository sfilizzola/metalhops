package venue

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func validRaw() RawSubmission {
	name, address, city, country, venueType := "  Iron Maiden  ", "1 High Street", "Oslo", "no", "pub"
	lat, lon := 59.91, 10.75
	return RawSubmission{
		Name: &name, Latitude: &lat, Longitude: &lon, Address: &address, City: &city,
		CountryCode: &country, VenueType: &venueType, GenreIDs: []int64{4, 1, 4},
	}
}

func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("err %v", err)
	}
	return validation.Fields
}

func TestParseSubmissionNormalizes(t *testing.T) {
	got, err := ParseSubmission(validRaw())
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Iron Maiden" || got.CountryCode != "NO" || got.Description != nil || len(got.GenreIDs) != 2 || got.GenreIDs[0] != 1 || got.GenreIDs[1] != 4 {
		t.Fatalf("draft %+v", got)
	}
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"name":"Iron Maiden","description":null,"latitude":59.91,"longitude":10.75,"address":"1 High Street","city":"Oslo","country_code":"NO","venue_type":"pub","genre_ids":[1,4]}`
	if string(body) != want {
		t.Fatalf("command %s", body)
	}
	for _, code := range []string{"gb", " XK ", "Gb"} {
		raw := validRaw()
		raw.CountryCode = &code
		if got, err := ParseSubmission(raw); err != nil || (got.CountryCode != "GB" && got.CountryCode != "XK") {
			t.Fatalf("%q %+v %v", code, got, err)
		}
	}
	description := " Loud.\nEvery night. "
	raw := validRaw()
	raw.Description = &description
	if got, err := ParseSubmission(raw); err != nil || *got.Description != "Loud.\nEvery night." {
		t.Fatalf("description %+v %v", got, err)
	}
	decomposed := "Cafe\u0301"
	raw = validRaw()
	raw.Name = &decomposed
	if got, err := ParseSubmission(raw); err != nil || got.Name != "Caf\u00e9" {
		t.Fatalf("nfc %q %v", got.Name, err)
	}
}

func TestParseSubmissionMissing(t *testing.T) {
	fields := fieldsOf(t, func() error { _, err := ParseSubmission(RawSubmission{}); return err }())
	want := map[string]string{
		"name":         "must be 1 to 150 characters",
		"address":      "must be 1 to 500 characters",
		"city":         "must be 1 to 120 characters",
		"country_code": "must be an ISO 3166-1 alpha-2 code",
		"venue_type":   "must be bar, pub, or club",
		"latitude":     "must be from -90 to 90",
		"longitude":    "must be from -180 to 180",
		"genre_ids":    "must include a genre",
	}
	if len(fields) != len(want) {
		t.Fatalf("fields %v", fields)
	}
	for name, message := range want {
		if fields[name] != message {
			t.Fatalf("%s %q", name, fields[name])
		}
	}
}

func TestParseSubmissionBoundaries(t *testing.T) {
	str := func(s string) *string { return &s }
	num := func(f float64) *float64 { return &f }
	cases := []struct {
		name    string
		edit    func(*RawSubmission)
		field   string
		message string
	}{
		{"blank name", func(r *RawSubmission) { r.Name = str("   ") }, "name", "must be 1 to 150 characters"},
		{"long name", func(r *RawSubmission) { r.Name = str(strings.Repeat("a", 151)) }, "name", "must be 1 to 150 characters"},
		{"name newline", func(r *RawSubmission) { r.Name = str("Iron\nMaiden") }, "name", "must be a single line of text"},
		{"address cr", func(r *RawSubmission) { r.Address = str("1\rHigh") }, "address", "must be a single line of text"},
		{"long address", func(r *RawSubmission) { r.Address = str(strings.Repeat("a", 501)) }, "address", "must be 1 to 500 characters"},
		{"long city", func(r *RawSubmission) { r.City = str(strings.Repeat("a", 121)) }, "city", "must be 1 to 120 characters"},
		{"blank description", func(r *RawSubmission) { r.Description = str("  ") }, "description", "must be 1 to 2000 characters"},
		{"long description", func(r *RawSubmission) { r.Description = str(strings.Repeat("a", 2001)) }, "description", "must be 1 to 2000 characters"},
		{"null byte description", func(r *RawSubmission) { r.Description = str("a\x00b") }, "description", "must be text"},
		{"bell description", func(r *RawSubmission) { r.Description = str("a\x07b") }, "description", "must be text"},
		{"uk", func(r *RawSubmission) { r.CountryCode = str("UK") }, "country_code", "must be an ISO 3166-1 alpha-2 code"},
		{"type case", func(r *RawSubmission) { r.VenueType = str("Pub") }, "venue_type", "must be bar, pub, or club"},
		{"lat high", func(r *RawSubmission) { r.Latitude = num(90.0001) }, "latitude", "must be from -90 to 90"},
		{"lat low", func(r *RawSubmission) { r.Latitude = num(-90.0001) }, "latitude", "must be from -90 to 90"},
		{"lon high", func(r *RawSubmission) { r.Longitude = num(180.0001) }, "longitude", "must be from -180 to 180"},
		{"empty genres", func(r *RawSubmission) { r.GenreIDs = []int64{} }, "genre_ids", "must include a genre"},
		{"zero genre", func(r *RawSubmission) { r.GenreIDs = []int64{0} }, "genre_ids", "must be a positive integer"},
		{"int4 overflow", func(r *RawSubmission) { r.GenreIDs = []int64{2147483648} }, "genre_ids", "must be a positive integer"},
	}
	for _, c := range cases {
		raw := validRaw()
		c.edit(&raw)
		_, err := ParseSubmission(raw)
		if got := fieldsOf(t, err)[c.field]; got != c.message {
			t.Fatalf("%s: %q", c.name, got)
		}
	}
	for _, edit := range []func(*RawSubmission){
		func(r *RawSubmission) { r.Name = str(strings.Repeat("é", 150)) },
		func(r *RawSubmission) { r.Description = str(strings.Repeat("a", 2000)) },
		func(r *RawSubmission) { r.Latitude, r.Longitude = num(-90), num(180) },
		func(r *RawSubmission) { r.GenreIDs = []int64{2147483647} },
	} {
		raw := validRaw()
		edit(&raw)
		if _, err := ParseSubmission(raw); err != nil {
			t.Fatalf("boundary rejected %v", err)
		}
	}
}

func TestDisclose(t *testing.T) {
	if got := disclose([]duplicateCandidate{{ID: "b", Status: "pending"}}); got != nil {
		t.Fatalf("private pending disclosed %+v", got)
	}
	got := disclose([]duplicateCandidate{{ID: "a", Status: "pending"}, {ID: "b", Status: "verified"}})
	if got == nil || got.VenueID != "b" || got.VerificationStatus != "verified" {
		t.Fatalf("verified %+v", got)
	}
	got = disclose([]duplicateCandidate{{ID: "a", Status: "pending"}, {ID: "c", Status: "pending", Own: true}, {ID: "d", Status: "verified"}})
	if got == nil || got.VenueID != "c" || got.VerificationStatus != "pending" {
		t.Fatalf("own %+v", got)
	}
}

func TestQuotaRetryAfter(t *testing.T) {
	now := time.Date(2020, 1, 15, 12, 0, 0, 0, time.UTC)
	if got := quotaRetryAfter(now.Add(-23*time.Hour), now); got != 3600 {
		t.Fatalf("hour %d", got)
	}
	if got := quotaRetryAfter(now.Add(-24*time.Hour+500*time.Millisecond), now); got != 1 {
		t.Fatalf("round up %d", got)
	}
	if got := quotaRetryAfter(now.Add(-25*time.Hour), now); got != 1 {
		t.Fatalf("minimum %d", got)
	}
}

func TestSameSubmission(t *testing.T) {
	draft, err := ParseSubmission(validRaw())
	if err != nil {
		t.Fatal(err)
	}
	current := Submission{
		Name: draft.Name, Latitude: draft.Latitude, Longitude: draft.Longitude,
		Address: draft.Address, City: draft.City, CountryCode: draft.CountryCode, VenueType: draft.VenueType,
		Genres: []Genre{{ID: 4, Name: "A"}, {ID: 1, Name: "B"}},
	}
	if !sameSubmission(current, draft) {
		t.Fatal("equal submission differs")
	}
	note := "x"
	for _, edit := range []func(*Submission){
		func(s *Submission) { s.Name = "Other" },
		func(s *Submission) { s.Latitude = 59.9100001 },
		func(s *Submission) { s.Description = &note },
		func(s *Submission) { s.Genres = s.Genres[:1] },
		func(s *Submission) { s.Genres = []Genre{{ID: 4}, {ID: 2}} },
	} {
		changed := current
		changed.Genres = append([]Genre(nil), current.Genres...)
		edit(&changed)
		if sameSubmission(changed, draft) {
			t.Fatalf("change not detected %+v", changed)
		}
	}
}

func TestSubmissionBodyRoundTrip(t *testing.T) {
	description := "Loud"
	in := Submission{
		ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Name: "Iron", Description: &description,
		Latitude: 1.5, Longitude: -2.25, Address: "1", City: "Oslo", CountryCode: "NO", VenueType: "pub",
		VerificationStatus: "pending", Genres: []Genre{{ID: 1, Name: "Punk"}},
		CreatedAt: time.Date(2020, 1, 15, 12, 0, 0, 123456000, time.UTC),
		UpdatedAt: time.Date(2020, 1, 15, 12, 0, 1, 0, time.UTC),
	}
	body, err := json.Marshal(bodyOf(in))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"genres":[{"id":1,"name":"Punk"}]`) || !strings.Contains(string(body), `"created_at":"2020-01-15T12:00:00.123456Z"`) {
		t.Fatalf("body %s", body)
	}
	var stored submissionBody
	if err := json.Unmarshal(body, &stored); err != nil {
		t.Fatal(err)
	}
	out := stored.submission()
	if out.ID != in.ID || *out.Description != description || !out.CreatedAt.Equal(in.CreatedAt) || out.Genres[0] != in.Genres[0] {
		t.Fatalf("round trip %+v", out)
	}
}
