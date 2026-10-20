package venue

import (
	"encoding/base64"
	"errors"
	"testing"
)

func TestCursorRoundTrip(t *testing.T) {
	id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	encoded := EncodeCursor("the bar", id)
	nameKey, gotID, err := DecodeCursor(encoded)
	if err != nil || nameKey != "the bar" || gotID != id {
		t.Fatalf("key %q id %q err %v", nameKey, gotID, err)
	}
	empty := EncodeCursor("", id)
	nameKey, gotID, err = DecodeCursor(empty)
	if err != nil || nameKey != "" || gotID != id {
		t.Fatalf("empty key %q id %q err %v", nameKey, gotID, err)
	}
}

func TestCursorRejectsMalformedValues(t *testing.T) {
	id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	for _, cursor := range []string{
		"",
		"abc",
		EncodeCursor("the|bar", id),
		base64.RawURLEncoding.EncodeToString([]byte("other-v1|the bar|" + id)),
		base64.RawURLEncoding.EncodeToString([]byte("the bar|" + id)),
		base64.RawURLEncoding.EncodeToString([]byte("name-v1|the bar")),
		EncodeCursor("the bar", "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"),
		EncodeCursor("the bar", "not-a-uuid"),
		base64.RawURLEncoding.EncodeToString([]byte("name-v1|\xff|" + id)),
		base64.RawURLEncoding.EncodeToString([]byte("name-v1|bad\x00key|" + id)),
	} {
		if _, _, err := DecodeCursor(cursor); !errors.As(err, new(*ValidationError)) {
			t.Fatalf("cursor %q err %v", cursor, err)
		}
	}
}

func TestValidateLimit(t *testing.T) {
	if err := ValidateLimit(1); err != nil {
		t.Fatal(err)
	}
	if err := ValidateLimit(100); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{0, 101} {
		var validation *ValidationError
		if err := ValidateLimit(limit); !errors.As(err, &validation) || validation.Fields["limit"] != "must be from 1 to 100" {
			t.Fatalf("limit %d err %v", limit, err)
		}
	}
}

func TestCanonicalVenueID(t *testing.T) {
	if !CanonicalVenueID("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa") {
		t.Fatal("lowercase rejected")
	}
	if CanonicalVenueID("AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA") {
		t.Fatal("uppercase accepted")
	}
}
