package review

import (
	"errors"
	"testing"
)

func TestNormalizeReport(t *testing.T) {
	reason, note, err := NormalizeReport("spam", nil)
	if err != nil || reason != "spam" || note != nil {
		t.Fatalf("spam nil note: %q %v %v", reason, note, err)
	}

	_, _, err = NormalizeReport("other", nil)
	var invalid *ValidationError
	if !errors.As(err, &invalid) || invalid.Fields["note"] != "required" {
		t.Fatalf("other nil note: %v", err)
	}

	_, _, err = NormalizeReport("nope", nil)
	if !errors.As(err, &invalid) || invalid.Fields["reason"] != "must be a known reason" {
		t.Fatalf("unknown reason: %v", err)
	}

	kept := "  kept  "
	reason, note, err = NormalizeReport("spam", &kept)
	if err != nil || reason != "spam" || note == nil || *note != "kept" {
		t.Fatalf("trim note: %q %v %v", reason, note, err)
	}

	empty := ""
	_, _, err = NormalizeReport("spam", &empty)
	if !errors.As(err, &invalid) || invalid.Fields["note"] != "must be 1 to 1000 characters" {
		t.Fatalf("empty note: %v", err)
	}
}

func TestNormalizeAction(t *testing.T) {
	_, _, _, err := NormalizeAction("nope", "spam", nil)
	var invalid *ValidationError
	if !errors.As(err, &invalid) || invalid.Fields["action"] != "must be remove_commentary, delete_review, or lift_commentary_restriction" {
		t.Fatalf("unknown action: %v", err)
	}

	action, reason, note, err := NormalizeAction("remove_commentary", "spam", nil)
	if err != nil || action != "remove_commentary" || reason != "spam" || note != nil {
		t.Fatalf("remove: %q %q %v %v", action, reason, note, err)
	}

	_, _, _, err = NormalizeAction("delete_review", "other", nil)
	if !errors.As(err, &invalid) || invalid.Fields["note"] != "required" {
		t.Fatalf("delete other: %v", err)
	}
}

func TestNormalizeLift(t *testing.T) {
	action, reason, note, err := NormalizeAction("lift_commentary_restriction", "", nil)
	if err != nil || action != "lift_commentary_restriction" || reason != "" || note != nil {
		t.Fatalf("lift: %q %q %v %v", action, reason, note, err)
	}

	var invalid *ValidationError
	_, _, _, err = NormalizeAction("lift_commentary_restriction", "spam", nil)
	if !errors.As(err, &invalid) || invalid.Fields["reason"] != "must be omitted" {
		t.Fatalf("lift with reason: %v", err)
	}

	blank := "   "
	_, _, _, err = NormalizeAction("lift_commentary_restriction", "", &blank)
	if !errors.As(err, &invalid) || invalid.Fields["note"] != "must be omitted" {
		t.Fatalf("lift with blank note: %v", err)
	}
}
