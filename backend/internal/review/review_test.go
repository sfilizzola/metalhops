package review

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseDraft(t *testing.T) {
	str := func(s string) *string { return &s }
	quote := func(s string) json.RawMessage {
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	badRating := map[string]string{"rating": "must be between 1 and 5"}
	notText := map[string]string{"commentary": "must be text"}
	tests := []struct {
		name       string
		rating     json.RawMessage
		commentary json.RawMessage
		want       Draft
		fields     map[string]string
	}{
		{name: "rating 1", rating: json.RawMessage(`1`), want: Draft{Rating: 1}},
		{name: "rating 5", rating: json.RawMessage(`5`), want: Draft{Rating: 5}},
		{name: "rating 0", rating: json.RawMessage(`0`), fields: badRating},
		{name: "rating 6", rating: json.RawMessage(`6`), fields: badRating},
		{name: "rating -1", rating: json.RawMessage(`-1`), fields: badRating},
		{name: "rating fraction", rating: json.RawMessage(`4.0`), fields: badRating},
		{name: "rating exponent", rating: json.RawMessage(`4e0`), fields: badRating},
		{name: "rating string", rating: json.RawMessage(`"4"`), fields: badRating},
		{name: "rating bool", rating: json.RawMessage(`true`), fields: badRating},
		{name: "rating null", rating: json.RawMessage(`null`), fields: badRating},
		{name: "rating omitted", fields: badRating},
		{name: "commentary null", rating: json.RawMessage(`4`), commentary: json.RawMessage(`null`), want: Draft{Rating: 4}},
		{name: "commentary blank", rating: json.RawMessage(`4`), commentary: quote("   "), want: Draft{Rating: 4}},
		{name: "commentary unicode blank", rating: json.RawMessage(`4`), commentary: quote("\u00a0 \n"), want: Draft{Rating: 4}},
		{name: "commentary trimmed", rating: json.RawMessage(`4`), commentary: quote("  loud  "), want: Draft{Rating: 4, Commentary: str("loud")}},
		{name: "commentary nfc", rating: json.RawMessage(`4`), commentary: quote("e\u0301"), want: Draft{Rating: 4, Commentary: str("é")}},
		{name: "commentary line feed", rating: json.RawMessage(`4`), commentary: quote("loud\ncold"), want: Draft{Rating: 4, Commentary: str("loud\ncold")}},
		{name: "commentary null byte", rating: json.RawMessage(`4`), commentary: quote("a\x00b"), fields: notText},
		{name: "commentary bell", rating: json.RawMessage(`4`), commentary: quote("a\u0007b"), fields: notText},
		{name: "commentary at cap", rating: json.RawMessage(`4`), commentary: quote(strings.Repeat("é", 2000)), want: Draft{Rating: 4, Commentary: str(strings.Repeat("é", 2000))}},
		{name: "commentary over cap", rating: json.RawMessage(`4`), commentary: quote(strings.Repeat("é", 2001)), fields: map[string]string{"commentary": "must be at most 2000 characters"}},
		{name: "commentary decomposed under cap", rating: json.RawMessage(`4`), commentary: quote(strings.Repeat("e\u0301", 2000)), want: Draft{Rating: 4, Commentary: str(strings.Repeat("é", 2000))}},
		{name: "commentary number", rating: json.RawMessage(`4`), commentary: json.RawMessage(`5`), fields: notText},
		{name: "commentary object", rating: json.RawMessage(`4`), commentary: json.RawMessage(`{}`), fields: notText},
		{name: "commentary array", rating: json.RawMessage(`4`), commentary: json.RawMessage(`[]`), fields: notText},
		{name: "both bad", rating: json.RawMessage(`0`), commentary: json.RawMessage(`5`), fields: map[string]string{"rating": "must be between 1 and 5", "commentary": "must be text"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDraft(tt.rating, tt.commentary)
			if tt.fields != nil {
				var verr *ValidationError
				if !errors.As(err, &verr) || !reflect.DeepEqual(verr.Fields, tt.fields) {
					t.Fatalf("err = %v, want fields %v", err, tt.fields)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("draft = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCursorRoundTrip(t *testing.T) {
	venueID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	otherVenueID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	reviewID := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	created := time.Date(2026, 10, 21, 9, 0, 0, 123456789, time.FixedZone("CEST", 2*60*60))

	gotTime, gotID, err := decodeCursor(encodeCursor(venueID, created, reviewID), venueID)
	if err != nil {
		t.Fatal(err)
	}
	if !gotTime.Equal(created) || gotTime.Location() != time.UTC || gotID != reviewID {
		t.Fatalf("decoded = %v %q", gotTime, gotID)
	}

	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	stamp := created.UTC().Format(time.RFC3339Nano)
	for name, cursor := range map[string]string{
		"other venue":   encodeCursor(venueID, created, reviewID),
		"not base64":    "abc",
		"discriminator": enc("review-v2|" + otherVenueID + "|" + stamp + "|" + reviewID),
		"review id":     enc("review-v1|" + otherVenueID + "|" + stamp + "|" + strings.ToUpper(reviewID)),
		"timestamp":     enc("review-v1|" + otherVenueID + "|yesterday|" + reviewID),
		"extra part":    enc("review-v1|" + otherVenueID + "|" + stamp + "|" + reviewID + "|x"),
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := decodeCursor(cursor, otherVenueID)
			var verr *ValidationError
			if !errors.As(err, &verr) || !reflect.DeepEqual(verr.Fields, map[string]string{"cursor": "invalid"}) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestValidateLimit(t *testing.T) {
	for _, limit := range []int{0, 101} {
		var verr *ValidationError
		if err := ValidateLimit(limit); !errors.As(err, &verr) || !reflect.DeepEqual(verr.Fields, map[string]string{"limit": "must be from 1 to 100"}) {
			t.Fatalf("ValidateLimit(%d) = %v", limit, err)
		}
	}
	for _, limit := range []int{1, 100} {
		if err := ValidateLimit(limit); err != nil {
			t.Fatalf("ValidateLimit(%d) = %v", limit, err)
		}
	}
}
