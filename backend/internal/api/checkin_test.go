package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/metalhops/metalhops/backend/internal/auth"
	"github.com/metalhops/metalhops/backend/internal/checkin"
	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/profile"
)

type fakeCheckIns struct {
	created   checkin.CheckIn
	page      checkin.Page
	feed      []checkin.FeedItem
	err       error
	create    checkin.CreateCommand
	query     checkin.ListQuery
	userID    string
	venueID   string
	checkInID string
	calls     int
}

func (f *fakeCheckIns) Create(_ context.Context, cmd checkin.CreateCommand) (checkin.CheckIn, error) {
	f.calls++
	f.create = cmd
	return f.created, f.err
}

func (f *fakeCheckIns) ListOwn(_ context.Context, q checkin.ListQuery) (checkin.Page, error) {
	f.calls++
	f.query = q
	return f.page, f.err
}

func (f *fakeCheckIns) ListVenueFeed(_ context.Context, venueID, callerUserID string) ([]checkin.FeedItem, error) {
	f.calls++
	f.venueID, f.userID = venueID, callerUserID
	return f.feed, f.err
}

func (f *fakeCheckIns) Delete(_ context.Context, userID, checkInID string) error {
	f.calls++
	f.userID, f.checkInID = userID, checkInID
	return f.err
}

const (
	checkInVenueID    = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	checkInID         = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	checkInUserID     = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	venueCheckInsPath = "/v1/venues/" + checkInVenueID + "/check-ins"
	ownCheckInsPath   = "/v1/me/check-ins"
	ownCheckInPath    = ownCheckInsPath + "/" + checkInID
)

func checkInHandler(authenticator *fakeAuth, checkIns *fakeCheckIns) http.Handler {
	return New(Options{
		Readiness:   readyOK(),
		PublicLimit: allowAll,
		Auth:        authenticator,
		CheckIns:    checkIns,
		Logger:      slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)),
	})
}

func sampleCheckIn() checkin.CheckIn {
	bar := "bar"
	return checkin.CheckIn{
		ID: checkInID, VenueID: checkInVenueID,
		VisitedAt:  time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC),
		RecordedAt: time.Date(2026, 10, 24, 10, 0, 1, 0, time.UTC),
		Venue:      checkin.VenueSummary{Name: "The Riff", City: "Berlin", CountryCode: "DE", VenueType: &bar, VerificationStatus: "verified"},
	}
}

func keyed(key string) map[string]string {
	return map[string]string{"Content-Type": "application/json", "Idempotency-Key": key}
}

func TestCheckInRoutes(t *testing.T) {
	caller := auth.Caller{UserID: checkInUserID}
	cursor := "next"
	rejected := sampleCheckIn()
	rejected.Venue.VenueType = nil
	rejected.Venue.VerificationStatus = "rejected"
	checkIns := &fakeCheckIns{created: sampleCheckIn(), page: checkin.Page{CheckIns: []checkin.CheckIn{rejected}, NextCursor: &cursor}}
	handler := checkInHandler(&fakeAuth{caller: caller}, checkIns)

	created := requestWith(handler, http.MethodPost, venueCheckInsPath, "Bearer t", []byte(`{}`), keyed("checkin-key-1"))
	if created.Code != http.StatusCreated || checkIns.create.UserID != caller.UserID || checkIns.create.VenueID != checkInVenueID || checkIns.create.Key != "checkin-key-1" || checkIns.create.VisitedAt != nil {
		t.Fatalf("create %d %s %+v", created.Code, created.Body.String(), checkIns.create)
	}
	for _, want := range []string{`"visited_at":"2026-10-24T10:00:00Z"`, `"recorded_at"`, `"venue_id"`, `"venue":{"name":"The Riff","city":"Berlin","country_code":"DE","venue_type":"bar","verification_status":"verified"}`} {
		if !bytes.Contains(created.Body.Bytes(), []byte(want)) {
			t.Fatalf("missing %s in %s", want, created.Body.String())
		}
	}

	checkIns.err = &checkin.ValidationError{Fields: map[string]string{"visited_at": "must be a timestamp"}}
	bad := requestWith(handler, http.MethodPost, venueCheckInsPath, "Bearer t", []byte(`{"visited_at":5}`), keyed("checkin-key-1"))
	assertCode(t, bad, http.StatusBadRequest, "validation_error")
	if string(checkIns.create.VisitedAt) != "5" || !bytes.Contains(bad.Body.Bytes(), []byte(`"visited_at":"must be a timestamp"`)) {
		t.Fatalf("raw visited_at %s %+v", bad.Body.String(), checkIns.create)
	}
	checkIns.err = nil

	before := checkIns.calls
	noKey := requestWith(handler, http.MethodPost, venueCheckInsPath, "Bearer t", []byte(`{}`), keyed(""))
	assertCode(t, noKey, http.StatusBadRequest, "validation_error")
	if !bytes.Contains(noKey.Body.Bytes(), []byte(`"idempotency_key":"required"`)) {
		t.Fatalf("no key %s", noKey.Body.String())
	}
	for _, body := range []string{`{"extra":1}`, `not json`} {
		assertCode(t, requestWith(handler, http.MethodPost, venueCheckInsPath, "Bearer t", []byte(body), keyed("k")), http.StatusBadRequest, "validation_error")
	}
	assertCode(t, requestWith(handler, http.MethodPost, venueCheckInsPath, "Bearer t", []byte(`{}`), map[string]string{"Content-Type": "text/plain", "Idempotency-Key": "k"}), http.StatusUnsupportedMediaType, "unsupported_media_type")
	large := []byte(`{"visited_at":"` + strings.Repeat("a", 1<<16) + `"}`)
	assertCode(t, requestWith(handler, http.MethodPost, venueCheckInsPath, "Bearer t", large, keyed("k")), http.StatusRequestEntityTooLarge, "body_too_large")
	assertCode(t, requestWith(handler, http.MethodPost, venueCheckInsPath+"?x=1", "Bearer t", []byte(`{}`), keyed("k")), http.StatusBadRequest, "validation_error")
	if checkIns.calls != before {
		t.Fatal("rejected create reached the service")
	}

	list := requestWith(handler, http.MethodGet, ownCheckInsPath, "Bearer t", nil, nil)
	if list.Code != http.StatusOK || checkIns.query.Limit != 20 || checkIns.query.UserID != caller.UserID || !bytes.Contains(list.Body.Bytes(), []byte(`"next_cursor":"next"`)) {
		t.Fatalf("list %d %s %+v", list.Code, list.Body.String(), checkIns.query)
	}
	if bytes.Contains(list.Body.Bytes(), []byte(`"venue_type"`)) || !bytes.Contains(list.Body.Bytes(), []byte(`"verification_status":"rejected"`)) {
		t.Fatalf("rejected venue %s", list.Body.String())
	}
	requestWith(handler, http.MethodGet, ownCheckInsPath+"?limit=5&cursor=abc", "Bearer t", nil, nil)
	if checkIns.query.Limit != 5 || checkIns.query.Cursor != "abc" {
		t.Fatalf("query %+v", checkIns.query)
	}
	before = checkIns.calls
	for _, path := range []string{ownCheckInsPath + "?limit=x", ownCheckInsPath + "?limit=1&limit=2", ownCheckInsPath + "?sort=x"} {
		assertCode(t, requestWith(handler, http.MethodGet, path, "Bearer t", nil, nil), http.StatusBadRequest, "validation_error")
	}
	if checkIns.calls != before {
		t.Fatal("bad query reached the service")
	}

	deleted := requestWith(handler, http.MethodDelete, ownCheckInPath, "Bearer t", nil, map[string]string{"Idempotency-Key": "bad"})
	if deleted.Code != http.StatusNoContent || deleted.Body.Len() != 0 || checkIns.checkInID != checkInID || checkIns.userID != caller.UserID {
		t.Fatalf("delete %d %q %+v", deleted.Code, deleted.Body.String(), checkIns)
	}
	before = checkIns.calls
	assertCode(t, requestWith(handler, http.MethodDelete, ownCheckInPath+"?x=1", "Bearer t", nil, nil), http.StatusBadRequest, "validation_error")
	if checkIns.calls != before {
		t.Fatal("bad delete query reached the service")
	}
}

func TestCheckInFeedRoute(t *testing.T) {
	caller := auth.Caller{UserID: checkInUserID}
	visited := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	checkIns := &fakeCheckIns{feed: []checkin.FeedItem{
		{ID: checkInID, VisitedAt: visited, RecordedAt: visited, Checker: profile.Identity{Username: "metal_fan", DisplayName: "Metal Fan"}},
		{ID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", VisitedAt: visited, RecordedAt: visited, Checker: profile.Identity{DisplayName: "MetalHops User"}},
	}}
	handler := checkInHandler(&fakeAuth{caller: caller}, checkIns)

	feed := requestWith(handler, http.MethodGet, venueCheckInsPath, "Bearer t", nil, nil)
	if feed.Code != http.StatusOK || checkIns.venueID != checkInVenueID || checkIns.userID != caller.UserID {
		t.Fatalf("feed %d %s", feed.Code, feed.Body.String())
	}
	want := `{"data":[{"id":"` + checkInID + `","visited_at":"2026-10-24T10:00:00Z","recorded_at":"2026-10-24T10:00:00Z","checker":{"username":"metal_fan","display_name":"Metal Fan"}},` +
		`{"id":"eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee","visited_at":"2026-10-24T10:00:00Z","recorded_at":"2026-10-24T10:00:00Z","checker":{"username":null,"display_name":"MetalHops User"}}]}`
	if strings.TrimSpace(feed.Body.String()) != want {
		t.Fatalf("feed body %s", feed.Body.String())
	}

	before := checkIns.calls
	for _, path := range []string{venueCheckInsPath + "?x=1", "/v1/venues/AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA/check-ins"} {
		assertCode(t, requestWith(handler, http.MethodGet, path, "Bearer t", nil, nil), http.StatusBadRequest, "validation_error")
	}
	if checkIns.calls != before {
		t.Fatal("bad feed request reached the service")
	}

	checkIns.feed = nil
	empty := requestWith(handler, http.MethodGet, venueCheckInsPath, "Bearer t", nil, nil)
	if empty.Code != http.StatusOK || !bytes.Contains(empty.Body.Bytes(), []byte(`"data":[]`)) {
		t.Fatalf("empty %d %s", empty.Code, empty.Body.String())
	}

	checkIns.err = checkin.ErrVenueNotFound
	assertCode(t, requestWith(handler, http.MethodGet, venueCheckInsPath, "Bearer t", nil, nil), http.StatusNotFound, "venue_not_found")
	checkIns.err = profile.ErrProfileRequired
	noProfile := requestWith(handler, http.MethodGet, venueCheckInsPath, "Bearer t", nil, nil)
	assertCode(t, noProfile, http.StatusForbidden, "profile_required")
	if !bytes.Contains(noProfile.Body.Bytes(), []byte(`Create a profile first.`)) {
		t.Fatalf("profile required %s", noProfile.Body.String())
	}
	checkIns.err = errors.New("db down")
	assertCode(t, requestWith(handler, http.MethodGet, venueCheckInsPath, "Bearer t", nil, nil), http.StatusInternalServerError, "internal_error")
}

func TestCheckInAuth(t *testing.T) {
	routes := []struct{ method, path string }{
		{http.MethodPost, venueCheckInsPath},
		{http.MethodGet, venueCheckInsPath},
		{http.MethodGet, ownCheckInsPath},
		{http.MethodDelete, ownCheckInPath},
	}
	checkIns := &fakeCheckIns{}
	anonymous := checkInHandler(&fakeAuth{}, checkIns)
	operator := checkInHandler(&fakeAuth{caller: auth.Caller{UserID: checkInUserID}, operator: true}, checkIns)
	for _, route := range routes {
		assertCode(t, requestWith(anonymous, route.method, route.path, "", []byte(`{}`), keyed("k")), http.StatusUnauthorized, "unauthorized")
		assertForbidden(t, requestWith(operator, route.method, route.path, "Bearer t", []byte(`{}`), keyed("k")))
	}
	if checkIns.calls != 0 {
		t.Fatal("rejected caller reached the service")
	}
}

func TestCheckInErrors(t *testing.T) {
	until := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		err    error
		status int
		code   string
		want   string
		header string
	}{
		{checkin.ErrVenueNotFound, 404, "venue_not_found", "Venue not found.", ""},
		{&checkin.SpacingError{CheckInID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", VisitedAt: time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)}, 409, "check_in_spacing", "Another check-in at this venue is too close to this visit.", ""},
		{&checkin.SpacingError{CheckInID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", VisitedAt: time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)}, 409, "check_in_spacing", `"details":{"check_in_id":"eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee","visited_at":"2026-10-24T09:00:00Z"}`, ""},
		{idempotency.ErrReused, 409, "idempotency_key_reused", "", ""},
		{idempotency.ErrInvalid, 400, "validation_error", "", ""},
		{profile.ErrProfileRequired, 409, "profile_required", "Create a profile first.", ""},
		{profile.ErrRenameRequired, 403, "profile_rename_required", "", ""},
		{&profile.SuspendedError{Until: &until}, 403, "profile_suspended", `"suspended_until":"2026-11-01T00:00:00Z"`, ""},
		{&checkin.RateLimitError{RetryAfter: 2}, 429, "rate_limited", "Too many requests.", "2"},
		{errors.New("db down"), 500, "internal_error", "", ""},
	}
	for _, c := range cases {
		checkIns := &fakeCheckIns{err: c.err}
		handler := checkInHandler(&fakeAuth{caller: auth.Caller{UserID: checkInUserID}}, checkIns)
		rec := requestWith(handler, http.MethodPost, venueCheckInsPath, "Bearer t", []byte(`{}`), keyed("k"))
		assertCode(t, rec, c.status, c.code)
		if c.want != "" && !bytes.Contains(rec.Body.Bytes(), []byte(c.want)) {
			t.Fatalf("%s body %s", c.code, rec.Body.String())
		}
		if rec.Header().Get("Retry-After") != c.header {
			t.Fatalf("%s retry %q", c.code, rec.Header().Get("Retry-After"))
		}
	}

	deletes := []struct {
		err    error
		status int
		code   string
		want   string
	}{
		{checkin.ErrCheckInNotFound, 404, "check_in_not_found", "Check-in not found."},
		{&checkin.ValidationError{Fields: map[string]string{"check_in_id": "must be a canonical uuid"}}, 400, "validation_error", `"check_in_id":"must be a canonical uuid"`},
	}
	for _, c := range deletes {
		handler := checkInHandler(&fakeAuth{caller: auth.Caller{UserID: checkInUserID}}, &fakeCheckIns{err: c.err})
		rec := requestWith(handler, http.MethodDelete, ownCheckInPath, "Bearer t", nil, nil)
		assertCode(t, rec, c.status, c.code)
		if !bytes.Contains(rec.Body.Bytes(), []byte(c.want)) {
			t.Fatalf("%s body %s", c.code, rec.Body.String())
		}
	}
}
