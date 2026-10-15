package api

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSON(t *testing.T) {
	var got optionalString
	req := jsonRequest(`{"name":"Metal Fan"}`, "application/json")
	var body struct {
		Name optionalString `json:"name"`
	}
	if err := DecodeJSON(httptest.NewRecorder(), req, &body); err != nil || body.Name.Value != "Metal Fan" || !body.Name.Set || body.Name.Null {
		t.Fatalf("%#v %v", body.Name, err)
	}
	req = jsonRequest(`{"name":"Metal Fan"}`, "application/json; charset=utf-8")
	body = struct {
		Name optionalString `json:"name"`
	}{}
	if err := DecodeJSON(httptest.NewRecorder(), req, &body); err != nil {
		t.Fatal(err)
	}

	for _, media := range []string{"", "text/plain"} {
		err := DecodeJSON(httptest.NewRecorder(), jsonRequest(`{"name":"x"}`, media), &body)
		if !errors.Is(err, ErrUnsupportedMediaType) {
			t.Fatalf("%q %v", media, err)
		}
	}
	if err := DecodeJSON(httptest.NewRecorder(), jsonRequest(`{"name":"x","extra":1}`, "application/json"), &body); !errors.Is(err, ErrMalformedBody) {
		t.Fatalf("unknown %v", err)
	}
	body = struct {
		Name optionalString `json:"name"`
	}{}
	if err := DecodeJSON(httptest.NewRecorder(), jsonRequest(`{"name":null}`, "application/json"), &body); err != nil || !body.Name.Set || !body.Name.Null {
		t.Fatalf("null %#v %v", body.Name, err)
	}
	body = struct {
		Name optionalString `json:"name"`
	}{}
	if err := DecodeJSON(httptest.NewRecorder(), jsonRequest(`{}`, "application/json"), &body); err != nil || body.Name.Set || body.Name.Null {
		t.Fatalf("omitted %#v %v", body.Name, err)
	}
	for _, raw := range []string{"", "[]", `{"name":"a"}{"name":"b"}`, "{\"name\":\"\xff\"}"} {
		err := DecodeJSON(httptest.NewRecorder(), jsonRequest(raw, "application/json"), &got)
		if !errors.Is(err, ErrMalformedBody) {
			t.Fatalf("%q %v", raw, err)
		}
	}

	exact := jsonSize(1 << 16)
	var sized struct {
		Name string `json:"name"`
	}
	if err := DecodeJSON(httptest.NewRecorder(), jsonRequest(string(exact), "application/json"), &sized); err != nil || !strings.HasPrefix(sized.Name, "a") {
		t.Fatalf("exact %d %v", len(exact), err)
	}
	tooBig := append(append([]byte{}, exact...), 'x')
	sized.Name = "untouched"
	err := DecodeJSON(httptest.NewRecorder(), jsonRequest(string(tooBig), "application/json"), &sized)
	if !errors.Is(err, ErrBodyTooLarge) || sized.Name != "untouched" {
		t.Fatalf("too big %d %#v %v", len(tooBig), sized.Name, err)
	}
}

func jsonRequest(body, media string) *http.Request {
	req := httptest.NewRequest(http.MethodPatch, "/v1/me", bytes.NewBufferString(body))
	if media != "" {
		req.Header.Set("Content-Type", media)
	}
	return req
}

func jsonSize(n int) []byte {
	prefix := []byte(`{"name":"`)
	suffix := []byte(`"}`)
	pad := n - len(prefix) - len(suffix)
	out := make([]byte, 0, n)
	out = append(out, prefix...)
	out = append(out, bytes.Repeat([]byte("a"), pad)...)
	return append(out, suffix...)
}
