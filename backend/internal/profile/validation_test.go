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

func TestOptionalFieldValidation(t *testing.T) {
	fieldError := func(err error, key string) bool {
		var validation *ValidationError
		return errors.As(err, &validation) && validation.Fields[key] != ""
	}

	bio, err := normalizeBio("  line one\nline two  ")
	if err != nil || bio == nil || *bio != "line one\nline two" {
		t.Fatalf("bio %v %v", bio, err)
	}
	if bio, err := normalizeBio(strings.Repeat("é", 280)); err != nil || utf8.RuneCountInString(*bio) != 280 {
		t.Fatalf("bio 280 %v", err)
	}
	for _, bad := range []string{strings.Repeat("a", 281), "bad\x00", "bad\r", "bad\x01", string([]byte{0xff})} {
		if _, err := normalizeBio(bad); !fieldError(err, "bio") {
			t.Fatalf("bio %q err %v", bad, err)
		}
	}
	if bio, err := normalizeBio("   "); err != nil || bio != nil {
		t.Fatalf("bio clear %v %v", bio, err)
	}

	city, err := normalizeCity(" Hamburg ")
	if err != nil || city == nil || *city != "Hamburg" {
		t.Fatalf("city %v %v", city, err)
	}
	if _, err := normalizeCity(strings.Repeat("é", 80)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Repeat("a", 81), "Ham\nburg", "bad\x00"} {
		if _, err := normalizeCity(bad); !fieldError(err, "from_city") {
			t.Fatalf("city %q err %v", bad, err)
		}
	}
	if city, err := normalizeCity(""); err != nil || city != nil {
		t.Fatalf("city clear %v %v", city, err)
	}

	if country, err := normalizeCountry("DE"); err != nil || *country != "DE" {
		t.Fatalf("country %v", err)
	}
	for _, bad := range []string{"de", "DEU", "D", " DE", "D1"} {
		if _, err := normalizeCountry(bad); !fieldError(err, "from_country") {
			t.Fatalf("country %q err %v", bad, err)
		}
	}
	if country, err := normalizeCountry(""); err != nil || country != nil {
		t.Fatalf("country clear %v %v", country, err)
	}

	for _, ok := range []string{"woman", "man", "non_binary", "prefer_not_to_say"} {
		if gender, err := normalizeGender(ok); err != nil || *gender != ok {
			t.Fatalf("gender %q %v", ok, err)
		}
	}
	for _, bad := range []string{"x", "Woman", "other"} {
		if _, err := normalizeGender(bad); !fieldError(err, "gender") {
			t.Fatalf("gender %q err %v", bad, err)
		}
	}
	if gender, err := normalizeGender(""); err != nil || gender != nil {
		t.Fatalf("gender clear %v %v", gender, err)
	}

	base, user := "https://abc.supabase.co", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	own := base + "/storage/v1/object/public/avatars/" + user + "/0f1e2d3c.webp"
	if avatar, err := normalizeAvatarURL(own, base, user); err != nil || *avatar != own {
		t.Fatalf("avatar %v", err)
	}
	for _, bad := range []string{
		"https://evil.example/a.webp",
		base + "/storage/v1/object/public/avatars/bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb/a.webp",
		base + "/storage/v1/object/public/avatars/" + user + "/",
		base + "/storage/v1/object/public/avatars/" + user + "/../other/a.webp",
		base + "/storage/v1/object/public/avatars/" + user + "/a.webp?x=1",
		base + "/storage/v1/object/public/other/" + user + "/a.webp",
		"https://abc.supabase.co.evil.example/storage/v1/object/public/avatars/" + user + "/a.webp",
	} {
		if _, err := normalizeAvatarURL(bad, base, user); !fieldError(err, "avatar_url") {
			t.Fatalf("avatar %q err %v", bad, err)
		}
	}
	if avatar, err := normalizeAvatarURL("", base, user); err != nil || avatar != nil {
		t.Fatalf("avatar clear %v %v", avatar, err)
	}
}
