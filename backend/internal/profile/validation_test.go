package profile

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestUsernameValidation(t *testing.T) {
	got, err := normalizeUsername("MetalFan")
	if err != nil || got != "MetalFan" {
		t.Fatalf("casing %q %v", got, err)
	}
	other, err := normalizeUsername("metalfan")
	if err != nil || !strings.EqualFold(got, other) || got == other {
		t.Fatalf("identity %q %q %v", got, other, err)
	}
	from, err := normalizeUsername("sfilizZola")
	if err != nil {
		t.Fatal(err)
	}
	to, err := normalizeUsername("sfilizzola")
	if err != nil || !strings.EqualFold(from, to) || from == to {
		t.Fatalf("correction %q %q %v", from, to, err)
	}

	if _, err := normalizeUsername(strings.Repeat("a", 3)); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeUsername(strings.Repeat("a", 30)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"ab", strings.Repeat("a", 31), "foo-bar", "foo.bar", "föo", "foo bar"} {
		_, err := normalizeUsername(bad)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Fields["username"] == "" {
			t.Fatalf("%q err %v", bad, err)
		}
	}

	reserved := []string{
		"admin", "administrator", "mod", "moderator", "metalhops", "support", "staff", "official",
		"Admin", "ad_min", "adm1n", "meta1hops", "m0d", "4dm1n", "5upp0rt", "574ff", "0ff1c14l",
		"my_metalhops",
	}
	for _, name := range reserved {
		_, err := normalizeUsername(name)
		if !errors.Is(err, ErrUsernameReserved) {
			t.Fatalf("%q err %v", name, err)
		}
	}
	if _, err := normalizeUsername("adminfan"); err != nil {
		t.Fatal(err)
	}
}

func TestDisplayNameValidation(t *testing.T) {
	got, err := normalizeDisplayName(" \u00a0Metal Fan\u00a0 ")
	if err != nil || got != "Metal Fan" {
		t.Fatalf("trim %q %v", got, err)
	}
	got, err = normalizeDisplayName("e\u0301")
	if err != nil || got != "é" {
		t.Fatalf("nfc %q %v", got, err)
	}
	if _, err := normalizeDisplayName("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeDisplayName(strings.Repeat("é", 80)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("a", 81), string([]byte{0xff}), "bad\x00", "bad\n", "bad\x01"} {
		_, err := normalizeDisplayName(bad)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Fields["display_name"] == "" {
			t.Fatalf("%q valid %v err %v", bad, utf8.ValidString(bad), err)
		}
	}
}

func TestModerationText(t *testing.T) {
	if !ValidReason("spam") || ValidReason("nope") {
		t.Fatal("reason")
	}
	if !NoteRequired("other", false) || NoteRequired("spam", false) {
		t.Fatal("action note")
	}
	if !NoteRequired("impersonation", true) || NoteRequired("spam", true) {
		t.Fatal("report note")
	}
	note, err := NormalizeNote("  café\nline  ")
	if err != nil || note != "café\nline" {
		t.Fatalf("%q %v", note, err)
	}
	if _, err := NormalizeNote("bad\x00"); err == nil {
		t.Fatal("control")
	}
}
