package contract

import (
	"bytes"
	"testing"
)

func TestOpenAPIDocument(t *testing.T) {
	for _, want := range []string{
		"openapi: 3.1.0", "/healthz", "/readyz", "http://localhost:8080",
		"/v1/me", "type: http", "scheme: bearer",
		"username_reserved", "recent_authentication_required", "auth_provider_error",
	} {
		if !bytes.Contains(OpenAPI, []byte(want)) {
			t.Fatalf("missing %q", want)
		}
	}
	me := bytes.Index(OpenAPI, []byte("/v1/me"))
	if me < 0 || bytes.Contains(OpenAPI[:me], []byte("security:")) {
		t.Fatal("operational route requires security")
	}
}
