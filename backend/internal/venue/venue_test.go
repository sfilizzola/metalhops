package venue

import (
	"errors"
	"testing"
)

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
