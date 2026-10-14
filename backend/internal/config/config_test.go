package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEnvironment(t *testing.T) {
	dir := t.TempDir()
	lookup := map[string]string{
		"DATABASE_URL": "postgres://user:s3cret@127.0.0.1:5432/metalhops",
	}
	getenv := func(key string) (string, bool) {
		value, ok := lookup[key]
		return value, ok
	}

	cfg, err := load(getenv, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "8080" || !cfg.DocsUIEnabled {
		t.Fatalf("port %q docs %v", cfg.Port, cfg.DocsUIEnabled)
	}
	if cfg.DatabaseURL != lookup["DATABASE_URL"] {
		t.Fatalf("url %q", cfg.DatabaseURL)
	}

	lookup["PORT"] = "9090"
	lookup["DOCS_UI_ENABLED"] = "false"
	lookup["SUPABASE_JWT_SECRET"] = "legacy-secret"
	cfg, err = load(getenv, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "9090" || cfg.DocsUIEnabled {
		t.Fatalf("port %q docs %v", cfg.Port, cfg.DocsUIEnabled)
	}
}

func TestLoadRejectsEnvironment(t *testing.T) {
	dir := t.TempDir()
	validURL := "postgres://user:s3cret@127.0.0.1:5432/metalhops"
	cases := []struct {
		name    string
		env     map[string]string
		errText string
	}{
		{name: "missing url", env: map[string]string{}, errText: "DATABASE_URL is required"},
		{name: "bad url", env: map[string]string{"DATABASE_URL": "not-a-url-s3cret"}, errText: "DATABASE_URL must be a valid PostgreSQL URL"},
		{name: "port zero", env: map[string]string{"DATABASE_URL": validURL, "PORT": "0"}, errText: "PORT must be an integer from 1 through 65535"},
		{name: "port high", env: map[string]string{"DATABASE_URL": validURL, "PORT": "65536"}, errText: "PORT must be an integer from 1 through 65535"},
		{name: "port padded", env: map[string]string{"DATABASE_URL": validURL, "PORT": " 8080"}, errText: "PORT must be an integer from 1 through 65535"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(func(key string) (string, bool) {
				value, ok := tc.env[key]
				return value, ok
			}, dir)
			if err == nil || err.Error() != tc.errText {
				t.Fatalf("err %v", err)
			}
			if strings.Contains(err.Error(), "s3cret") {
				t.Fatalf("error contains secret: %s", err.Error())
			}
		})
	}

	for _, value := range []string{"TRUE", "1", " true"} {
		_, err := load(func(key string) (string, bool) {
			if key == "DATABASE_URL" {
				return "postgres://user:s3cret@127.0.0.1:5432/metalhops", true
			}
			if key == "DOCS_UI_ENABLED" {
				return value, true
			}
			return "", false
		}, dir)
		if err == nil || err.Error() != "DOCS_UI_ENABLED must be true, false, or empty" {
			t.Fatalf("%q err %v", value, err)
		}
	}
}

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	body := "  # comment\n" +
		"export PORT=9090\n" +
		"DATABASE_URL=postgres://user:p=a=ss@127.0.0.1:5432/metalhops\n" +
		"DOCS_UI_ENABLED=false\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := load(func(string) (string, bool) { return "", false }, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "9090" || cfg.DocsUIEnabled {
		t.Fatalf("port %q docs %v", cfg.Port, cfg.DocsUIEnabled)
	}
	if cfg.DatabaseURL != "postgres://user:p=a=ss@127.0.0.1:5432/metalhops" {
		t.Fatalf("url %q", cfg.DatabaseURL)
	}

	cfg, err = load(func(key string) (string, bool) {
		if key == "PORT" {
			return "8081", true
		}
		return "", false
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "8081" {
		t.Fatalf("env port lost: %q", cfg.Port)
	}

	quoted := t.TempDir()
	if err := os.WriteFile(filepath.Join(quoted, ".env"), []byte("PORT=\"8080\"\nDATABASE_URL=postgres://user:s3cret@127.0.0.1:5432/metalhops\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = load(func(string) (string, bool) { return "", false }, quoted)
	if err == nil || err.Error() != "PORT must be an integer from 1 through 65535" {
		t.Fatalf("quoted port err %v", err)
	}

	missing := t.TempDir()
	_, err = load(func(key string) (string, bool) {
		if key == "DATABASE_URL" {
			return "postgres://user:s3cret@127.0.0.1:5432/metalhops", true
		}
		return "", false
	}, missing)
	if err != nil {
		t.Fatal(err)
	}

	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, ".env"), []byte("# c\n\nNOTAKEY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = load(func(string) (string, bool) { return "", false }, broken)
	if err == nil || err.Error() != ".env line 3 is malformed" || strings.Contains(err.Error(), "NOTAKEY") {
		t.Fatalf("malformed err %v", err)
	}

	emptyKey := t.TempDir()
	if err := os.WriteFile(filepath.Join(emptyKey, ".env"), []byte("=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = load(func(string) (string, bool) { return "", false }, emptyKey)
	if err == nil || err.Error() != ".env line 1 is malformed" || strings.Contains(err.Error(), "value") {
		t.Fatalf("empty key err %v", err)
	}

	unreadable := t.TempDir()
	if err := os.Mkdir(filepath.Join(unreadable, ".env"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = load(func(string) (string, bool) { return "", false }, unreadable)
	if err == nil || !strings.Contains(err.Error(), filepath.Join(unreadable, ".env")) || strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("unreadable err %v", err)
	}
}
