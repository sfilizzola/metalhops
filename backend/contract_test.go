package contract

import (
	"bytes"
	"testing"
)

func TestOpenAPIDocument(t *testing.T) {
	for _, want := range []string{"openapi: 3.1.0", "/healthz", "/readyz", "http://localhost:8080"} {
		if !bytes.Contains(OpenAPI, []byte(want)) {
			t.Fatalf("missing %q", want)
		}
	}
	for _, banned := range []string{"/v1", "bearer"} {
		if bytes.Contains(OpenAPI, []byte(banned)) {
			t.Fatalf("contract contains %q", banned)
		}
	}
}
