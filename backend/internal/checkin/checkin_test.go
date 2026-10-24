package checkin

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func timePtr(t time.Time) *time.Time { return &t }

func TestParseVisitedAt(t *testing.T) {
	want := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	wantNanos := time.Date(2026, 10, 24, 10, 0, 0, 123456789, time.UTC)
	badFields := map[string]string{"visited_at": "must be a timestamp"}

	tests := []struct {
		name   string
		raw    json.RawMessage
		want   *time.Time
		fields map[string]string
	}{
		{name: "nil", raw: nil, want: nil},
		{name: "null", raw: json.RawMessage("null"), want: nil},
		{name: "utc", raw: json.RawMessage(`"2026-10-24T10:00:00Z"`), want: &want},
		{name: "offset", raw: json.RawMessage(`"2026-10-24T12:00:00+02:00"`), want: &want},
		{name: "nanoseconds", raw: json.RawMessage(`"2026-10-24T10:00:00.123456789Z"`), want: &wantNanos},
		{name: "no offset", raw: json.RawMessage(`"2026-10-24T10:00:00"`), fields: badFields},
		{name: "date only", raw: json.RawMessage(`"2026-10-24"`), fields: badFields},
		{name: "word", raw: json.RawMessage(`"yesterday"`), fields: badFields},
		{name: "number", raw: json.RawMessage(`5`), fields: badFields},
		{name: "bool", raw: json.RawMessage(`true`), fields: badFields},
		{name: "object", raw: json.RawMessage(`{}`), fields: badFields},
		{name: "array", raw: json.RawMessage(`[]`), fields: badFields},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseVisitedAt(tt.raw)
			if tt.fields != nil {
				var verr *ValidationError
				if !errors.As(err, &verr) || !reflect.DeepEqual(verr.Fields, tt.fields) {
					t.Fatalf("err = %v, want fields %v", err, tt.fields)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if tt.want == nil {
				if got != nil {
					t.Fatalf("got = %v, want nil", got)
				}
				return
			}
			if got == nil || !got.Equal(*tt.want) || got.Location() != time.UTC {
				t.Fatalf("got = %v, want %v (UTC)", got, tt.want)
			}
		})
	}
}

func TestResolveVisit(t *testing.T) {
	now := time.Date(2026, 10, 24, 10, 0, 0, 500, time.UTC)
	truncatedNow := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	windowFields := map[string]string{"visited_at": "must be within the last 24 hours"}
	aheadFields := map[string]string{"visited_at": "must not be more than 5 minutes ahead"}

	tests := []struct {
		name      string
		requested *time.Time
		want      time.Time
		fields    map[string]string
	}{
		{name: "nil", requested: nil, want: truncatedNow},
		{name: "24h ago", requested: timePtr(now.Add(-24 * time.Hour)), want: time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)},
		{name: "24h ago minus 1ns", requested: timePtr(now.Add(-24*time.Hour - time.Nanosecond)), fields: windowFields},
		{name: "5m ahead", requested: timePtr(now.Add(5 * time.Minute)), want: truncatedNow},
		{name: "5m ahead plus 1ns", requested: timePtr(now.Add(5*time.Minute + time.Nanosecond)), fields: aheadFields},
		{name: "1s ahead", requested: timePtr(now.Add(time.Second)), want: truncatedNow},
		{name: "nanosecond truncation", requested: timePtr(time.Date(2026, 10, 24, 9, 0, 0, 123456789, time.UTC)), want: time.Date(2026, 10, 24, 9, 0, 0, 123456000, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveVisit(tt.requested, now)
			if tt.fields != nil {
				var verr *ValidationError
				if !errors.As(err, &verr) || !reflect.DeepEqual(verr.Fields, tt.fields) {
					t.Fatalf("err = %v, want fields %v", err, tt.fields)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !got.Equal(tt.want) || got.Nanosecond() != tt.want.Nanosecond() {
				t.Fatalf("got = %v (%d ns), want %v (%d ns)", got, got.Nanosecond(), tt.want, tt.want.Nanosecond())
			}
		})
	}
}

func TestCursorRoundTrip(t *testing.T) {
	id := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	visitedAt := time.Date(2026, 10, 21, 9, 0, 0, 123456789, time.FixedZone("CEST", 2*60*60))

	gotTime, gotID, err := decodeCursor(encodeCursor(visitedAt, id))
	if err != nil {
		t.Fatal(err)
	}
	if !gotTime.Equal(visitedAt) || gotTime.Location() != time.UTC || gotID != id {
		t.Fatalf("decoded = %v %q", gotTime, gotID)
	}

	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	stamp := visitedAt.UTC().Format(time.RFC3339Nano)
	for name, cursor := range map[string]string{
		"empty":         "",
		"not base64":    "abc",
		"discriminator": enc("check-in-v2|" + stamp + "|" + id),
		"non-canonical": enc("check-in-v1|" + stamp + "|" + strings.ToUpper(id)),
		"bad timestamp": enc("check-in-v1|yesterday|" + id),
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := decodeCursor(cursor)
			var verr *ValidationError
			if !errors.As(err, &verr) || !reflect.DeepEqual(verr.Fields, map[string]string{"cursor": "invalid"}) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestValidateLimit(t *testing.T) {
	for _, limit := range []int{0, 101} {
		var verr *ValidationError
		if err := ValidateLimit(limit); !errors.As(err, &verr) || !reflect.DeepEqual(verr.Fields, map[string]string{"limit": "must be from 1 to 100"}) {
			t.Fatalf("ValidateLimit(%d) = %v", limit, err)
		}
	}
	for _, limit := range []int{1, 100} {
		if err := ValidateLimit(limit); err != nil {
			t.Fatalf("ValidateLimit(%d) = %v", limit, err)
		}
	}
}
