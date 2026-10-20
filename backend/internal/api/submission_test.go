package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/metalhops/metalhops/backend/internal/auth"
	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/venue"
)

type fakeSubmissions struct {
	got     venue.Submission
	page    venue.OwnPage
	err     error
	submit  venue.SubmitCommand
	correct venue.CorrectCommand
	query   venue.OwnQuery
	userID  string
	venueID string
	calls   int
}

func (f *fakeSubmissions) Submit(_ context.Context, cmd venue.SubmitCommand) (venue.Submission, error) {
	f.calls++
	f.submit = cmd
	return f.got, f.err
}

func (f *fakeSubmissions) Correct(_ context.Context, cmd venue.CorrectCommand) (venue.Submission, error) {
	f.calls++
	f.correct = cmd
	return f.got, f.err
}

func (f *fakeSubmissions) ListOwn(_ context.Context, q venue.OwnQuery) (venue.OwnPage, error) {
	f.calls++
	f.query = q
	return f.page, f.err
}

func (f *fakeSubmissions) GetOwn(_ context.Context, userID, venueID string) (venue.Submission, error) {
	f.calls++
	f.userID, f.venueID = userID, venueID
	return f.got, f.err
}

const submissionBodyJSON = `{"name":"Iron","latitude":59.9,"longitude":10.7,"address":"1 Street","city":"Oslo","country_code":"no","venue_type":"pub","genre_ids":[3,1]}`

func submissionHandler(authenticator *fakeAuth, submissions *fakeSubmissions) http.Handler {
	return New(Options{
		Readiness:   readyOK(),
		PublicLimit: allowAll,
		Auth:        authenticator,
		Submissions: submissions,
		Logger:      slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)),
	})
}

func sampleSubmission() venue.Submission {
	at := time.Date(2020, 1, 15, 12, 0, 0, 0, time.UTC)
	return venue.Submission{
		ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Name: "Iron", Latitude: 59.9, Longitude: 10.7,
		Address: "1 Street", City: "Oslo", CountryCode: "NO", VenueType: "pub", VerificationStatus: "pending",
		Genres: []venue.Genre{{ID: 1, Name: "Classic rock"}}, CreatedAt: at, UpdatedAt: at,
	}
}

func TestSubmissionRoutes(t *testing.T) {
	caller := auth.Caller{UserID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc"}
	submissions := &fakeSubmissions{got: sampleSubmission()}
	handler := submissionHandler(&fakeAuth{caller: caller}, submissions)
	plain := map[string]string{"Content-Type": "application/json"}
	keyed := map[string]string{"Content-Type": "application/json", "Idempotency-Key": "submit-key-1"}

	created := requestWith(handler, http.MethodPost, "/v1/venues", "Bearer t", []byte(submissionBodyJSON), keyed)
	if created.Code != http.StatusCreated || submissions.submit.Key != "submit-key-1" || submissions.submit.UserID != caller.UserID {
		t.Fatalf("create %d %s %+v", created.Code, created.Body.String(), submissions.submit)
	}
	if f := submissions.submit.Fields; f.Name == nil || *f.Name != "Iron" || *f.CountryCode != "no" || f.Description != nil || len(f.GenreIDs) != 2 || f.GenreIDs[0] != 3 {
		t.Fatalf("fields %+v", f)
	}
	for _, want := range []string{`"data":{`, `"created_at":"2020-01-15T12:00:00Z"`, `"updated_at":"2020-01-15T12:00:00Z"`, `"verification_status":"pending"`, `"genres":[{"id":1,"name":"Classic rock"}]`} {
		if !bytes.Contains(created.Body.Bytes(), []byte(want)) {
			t.Fatalf("missing %s in %s", want, created.Body.String())
		}
	}
	for _, unwanted := range []string{`average_rating`, `review_count`, `submitted_by`} {
		if bytes.Contains(created.Body.Bytes(), []byte(unwanted)) {
			t.Fatalf("leaked %s", unwanted)
		}
	}

	before := submissions.calls
	missingKey := requestWith(handler, http.MethodPost, "/v1/venues", "Bearer t", []byte(submissionBodyJSON), plain)
	assertCode(t, missingKey, http.StatusBadRequest, "validation_error")
	if !bytes.Contains(missingKey.Body.Bytes(), []byte(`"idempotency_key":"required"`)) || submissions.calls != before {
		t.Fatalf("missing key %s", missingKey.Body.String())
	}
	for _, body := range []string{`{"verification_status":"verified"}`, `{"latitude":"north"}`, `{"genre_ids":[1.5]}`, `not json`} {
		rec := requestWith(handler, http.MethodPost, "/v1/venues", "Bearer t", []byte(body), keyed)
		assertCode(t, rec, http.StatusBadRequest, "validation_error")
		if bytes.Contains(rec.Body.Bytes(), []byte(`"fields"`)) || submissions.calls != before {
			t.Fatalf("%s %s", body, rec.Body.String())
		}
	}

	put := requestWith(handler, http.MethodPut, "/v1/me/venues/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "Bearer t", []byte(submissionBodyJSON), plain)
	if put.Code != http.StatusOK || submissions.correct.VenueID != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" || submissions.correct.UserID != caller.UserID {
		t.Fatalf("put %d %s", put.Code, put.Body.String())
	}

	cursor := "next"
	submissions.page = venue.OwnPage{Submissions: []venue.Submission{sampleSubmission()}, NextCursor: &cursor}
	list := requestWith(handler, http.MethodGet, "/v1/me/venues", "Bearer t", nil, nil)
	if list.Code != http.StatusOK || submissions.query.Limit != 20 || submissions.query.UserID != caller.UserID || !bytes.Contains(list.Body.Bytes(), []byte(`"next_cursor":"next"`)) {
		t.Fatalf("list %d %s %+v", list.Code, list.Body.String(), submissions.query)
	}
	requestWith(handler, http.MethodGet, "/v1/me/venues?limit=5&cursor=abc", "Bearer t", nil, nil)
	if submissions.query.Limit != 5 || submissions.query.Cursor != "abc" {
		t.Fatalf("query %+v", submissions.query)
	}
	before = submissions.calls
	for _, path := range []string{"/v1/me/venues?sort=new", "/v1/me/venues?limit=1&limit=2", "/v1/me/venues?limit=x", "/v1/me/venues/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa?x=1"} {
		assertCode(t, requestWith(handler, http.MethodGet, path, "Bearer t", nil, nil), http.StatusBadRequest, "validation_error")
	}
	if submissions.calls != before {
		t.Fatal("bad query reached the service")
	}
	detail := requestWith(handler, http.MethodGet, "/v1/me/venues/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "Bearer t", nil, nil)
	if detail.Code != http.StatusOK || submissions.venueID != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" || submissions.userID != caller.UserID || bytes.Contains(detail.Body.Bytes(), []byte(`"meta"`)) {
		t.Fatalf("detail %d %s", detail.Code, detail.Body.String())
	}
}

func TestSubmissionAuth(t *testing.T) {
	routes := []struct{ method, path string }{
		{http.MethodPost, "/v1/venues"},
		{http.MethodGet, "/v1/me/venues"},
		{http.MethodGet, "/v1/me/venues/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
		{http.MethodPut, "/v1/me/venues/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
	}
	submissions := &fakeSubmissions{}
	headers := map[string]string{"Content-Type": "application/json", "Idempotency-Key": "submit-key-1"}
	anonymous := submissionHandler(&fakeAuth{}, submissions)
	operator := submissionHandler(&fakeAuth{caller: auth.Caller{UserID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc"}, operator: true}, submissions)
	for _, route := range routes {
		assertCode(t, requestWith(anonymous, route.method, route.path, "", []byte(submissionBodyJSON), headers), http.StatusUnauthorized, "unauthorized")
		assertForbidden(t, requestWith(operator, route.method, route.path, "Bearer t", []byte(submissionBodyJSON), headers))
	}
	if submissions.calls != 0 {
		t.Fatal("rejected caller reached the service")
	}
}

func TestSubmissionErrors(t *testing.T) {
	until := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		err    error
		status int
		code   string
		want   string
		header string
	}{
		{&venue.ValidationError{Fields: map[string]string{"genre_ids": "must be an existing genre"}}, 400, "validation_error", `"genre_ids":"must be an existing genre"`, ""},
		{idempotency.ErrInvalid, 400, "validation_error", "", ""},
		{idempotency.ErrReused, 409, "idempotency_key_reused", "", ""},
		{profile.ErrProfileRequired, 409, "profile_required", "Create a profile first.", ""},
		{profile.ErrRenameRequired, 403, "profile_rename_required", "", ""},
		{&profile.SuspendedError{Until: &until}, 403, "profile_suspended", `"suspended_until":"2026-11-01T00:00:00Z"`, ""},
		{venue.ErrNotFound, 404, "venue_not_found", "Venue not found.", ""},
		{&venue.NotPendingError{Status: "verified"}, 409, "venue_not_pending", "This venue can no longer be corrected.", ""},
		{&venue.NotPendingError{Status: "rejected"}, 409, "venue_not_pending", "Only a pending venue can be corrected.", ""},
		{&venue.DuplicateError{Match: &venue.DuplicateMatch{VenueID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", VerificationStatus: "verified"}}, 409, "duplicate_venue", `"details":{"venue_id":"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","verification_status":"verified"}`, ""},
		{&venue.QuotaError{RetryAfter: 3600}, 429, "quota_exceeded", "The submission limit has been reached.", "3600"},
		{&venue.RateLimitError{RetryAfter: 2}, 429, "rate_limited", "Too many requests.", "2"},
		{errors.New("db down"), 500, "internal_error", "", ""},
	}
	headers := map[string]string{"Content-Type": "application/json", "Idempotency-Key": "submit-key-1"}
	for _, c := range cases {
		submissions := &fakeSubmissions{err: c.err}
		handler := submissionHandler(&fakeAuth{caller: auth.Caller{UserID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc"}}, submissions)
		rec := requestWith(handler, http.MethodPost, "/v1/venues", "Bearer t", []byte(submissionBodyJSON), headers)
		assertCode(t, rec, c.status, c.code)
		if c.want != "" && !bytes.Contains(rec.Body.Bytes(), []byte(c.want)) {
			t.Fatalf("%s body %s", c.code, rec.Body.String())
		}
		if rec.Header().Get("Retry-After") != c.header {
			t.Fatalf("%s retry %q", c.code, rec.Header().Get("Retry-After"))
		}
	}
	private := &fakeSubmissions{err: &venue.DuplicateError{}}
	rec := requestWith(submissionHandler(&fakeAuth{caller: auth.Caller{UserID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc"}}, private), http.MethodPost, "/v1/venues", "Bearer t", []byte(submissionBodyJSON), headers)
	assertCode(t, rec, http.StatusConflict, "duplicate_venue")
	if bytes.Contains(rec.Body.Bytes(), []byte(`"details"`)) {
		t.Fatalf("private duplicate leaked %s", rec.Body.String())
	}
}
