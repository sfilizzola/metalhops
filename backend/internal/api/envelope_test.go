package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteData(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteData(rec, http.StatusCreated, map[string]string{"username": "lars"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type %q", rec.Header().Get("Content-Type"))
	}
	if strings.TrimSpace(rec.Body.String()) != `{"data":{"username":"lars"}}` {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestWriteListNullCursor(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteList(rec, http.StatusOK, []any{}, nil)
	if strings.TrimSpace(rec.Body.String()) != `{"data":[],"meta":{"next_cursor":null}}` {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestWriteErrorOmitsEmptyFields(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusBadRequest, "validation_error", "The request is invalid.", "abc", nil)
	if strings.Contains(rec.Body.String(), "fields") {
		t.Fatalf("body %s", rec.Body.String())
	}
	var got struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error.Code != "validation_error" || got.Error.RequestID != "abc" {
		t.Fatalf("decoded %#v", got.Error)
	}
}

func TestWriteErrorIncludesFields(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusBadRequest, "validation_error", "The request is invalid.", "abc", map[string]string{
		"rating": "must be between 1 and 5",
	})
	var got struct {
		Error struct {
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error.Fields["rating"] != "must be between 1 and 5" {
		t.Fatalf("fields %#v", got.Error.Fields)
	}
}
