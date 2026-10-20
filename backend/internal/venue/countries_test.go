package venue

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

func TestCountryCodesMatchMigration(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	body, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "infra", "supabase", "migrations", "0005_venue_catalogue.sql"))
	if err != nil {
		t.Fatal(err)
	}
	start := bytes.Index(body, []byte("venues_country_code_check"))
	if start < 0 {
		t.Fatal("country constraint not found")
	}
	end := bytes.Index(body[start:], []byte("));"))
	if end < 0 {
		t.Fatal("country constraint end not found")
	}
	want := map[string]struct{}{}
	for _, match := range regexp.MustCompile(`'([A-Z]{2})'`).FindAllSubmatch(body[start:start+end], -1) {
		want[string(match[1])] = struct{}{}
	}
	if len(want) != len(countryCodes) {
		t.Fatalf("migration %d codes, Go %d codes", len(want), len(countryCodes))
	}
	for code := range want {
		if _, ok := countryCodes[code]; !ok {
			t.Fatalf("Go set misses %s", code)
		}
	}
}
