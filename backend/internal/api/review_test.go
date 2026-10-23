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
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/review"
)

type fakeReviews struct {
	page    review.Page
	own     review.Own
	result  review.SaveResult
	err     error
	query   review.ListQuery
	save    review.SaveCommand
	userID  string
	venueID string
	calls   int
}

func (f *fakeReviews) List(_ context.Context, q review.ListQuery) (review.Page, error) {
	f.calls++
	f.query = q
	return f.page, f.err
}

func (f *fakeReviews) GetOwn(_ context.Context, userID, venueID string) (review.Own, error) {
	f.calls++
	f.userID, f.venueID = userID, venueID
	return f.own, f.err
}

func (f *fakeReviews) Save(_ context.Context, cmd review.SaveCommand) (review.SaveResult, error) {
	f.calls++
	f.save = cmd
	return f.result, f.err
}

func (f *fakeReviews) Delete(_ context.Context, userID, venueID string) error {
	f.calls++
	f.userID, f.venueID = userID, venueID
	return f.err
}

const (
	reviewVenueID  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	reviewsPath    = "/v1/venues/" + reviewVenueID + "/reviews"
	ownReviewPath  = reviewsPath + "/me"
	reviewBodyJSON = `{"rating":4,"commentary":"loud"}`
)

func reviewHandler(authenticator *fakeAuth, reviews *fakeReviews) http.Handler {
	return New(Options{
		Readiness:   readyOK(),
		PublicLimit: allowAll,
		Auth:        authenticator,
		Reviews:     reviews,
		Logger:      slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)),
	})
}

func sampleOwnReview() review.Own {
	at := time.Date(2020, 1, 15, 12, 0, 0, 0, time.UTC)
	commentary := "loud"
	return review.Own{
		ID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", UserID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", Rating: 4,
		Commentary: &commentary, Username: "metal_fan", DisplayName: "Metal Fan", CreatedAt: at, UpdatedAt: at,
	}
}

func TestReviewRoutes(t *testing.T) {
	caller := auth.Caller{UserID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc"}
	at := time.Date(2020, 1, 15, 12, 0, 0, 0, time.UTC)
	cursor := "next"
	reviews := &fakeReviews{
		page: review.Page{Reviews: []review.Review{{
			ID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", Rating: 4,
			Author: review.Author{DisplayName: "MetalHops User"}, CreatedAt: at, UpdatedAt: at,
		}}, NextCursor: &cursor},
		own:    sampleOwnReview(),
		result: review.SaveResult{Review: sampleOwnReview(), Created: true},
	}
	authenticator := &fakeAuth{caller: caller}
	handler := reviewHandler(authenticator, reviews)
	plain := map[string]string{"Content-Type": "application/json"}

	for _, authz := range []string{"", "Bearer t"} {
		list := requestWith(handler, http.MethodGet, reviewsPath, authz, nil, nil)
		if list.Code != http.StatusOK || reviews.query.Limit != 20 || reviews.query.VenueID != reviewVenueID || !bytes.Contains(list.Body.Bytes(), []byte(`"next_cursor":"next"`)) {
			t.Fatalf("list %q %d %s %+v", authz, list.Code, list.Body.String(), reviews.query)
		}
		for _, want := range []string{`"author":{"username":null,"display_name":"MetalHops User"}`, `"commentary":null`, `"commentary_removed":false`, `"created_at":"2020-01-15T12:00:00Z"`} {
			if !bytes.Contains(list.Body.Bytes(), []byte(want)) {
				t.Fatalf("missing %s in %s", want, list.Body.String())
			}
		}
		if bytes.Contains(list.Body.Bytes(), []byte(`user_id`)) || bytes.Contains(list.Body.Bytes(), []byte(`commentary_restriction`)) {
			t.Fatalf("leaked field %s", list.Body.String())
		}
	}
	if authenticator.calls != 0 {
		t.Fatal("public list read Authorization")
	}
	requestWith(handler, http.MethodGet, reviewsPath+"?limit=5&cursor=abc", "", nil, nil)
	if reviews.query.Limit != 5 || reviews.query.Cursor != "abc" {
		t.Fatalf("query %+v", reviews.query)
	}
	before := reviews.calls
	for _, path := range []string{reviewsPath + "?sort=x", reviewsPath + "?limit=1&limit=2", reviewsPath + "?limit=x", ownReviewPath + "?x=1"} {
		assertCode(t, requestWith(handler, http.MethodGet, path, "Bearer t", nil, nil), http.StatusBadRequest, "validation_error")
	}
	if rec := requestWith(handler, http.MethodGet, reviewsPath+"?limit=x", "", nil, nil); !bytes.Contains(rec.Body.Bytes(), []byte(`"limit":"must be from 1 to 100"`)) {
		t.Fatalf("limit %s", rec.Body.String())
	}
	if reviews.calls != before {
		t.Fatal("bad query reached the service")
	}

	own := requestWith(handler, http.MethodGet, ownReviewPath, "Bearer t", nil, nil)
	if own.Code != http.StatusOK || reviews.userID != caller.UserID || reviews.venueID != reviewVenueID {
		t.Fatalf("own %d %s", own.Code, own.Body.String())
	}
	for _, want := range []string{`"user_id":"cccccccc-cccc-4ccc-8ccc-cccccccccccc"`, `"author":{"username":"metal_fan","display_name":"Metal Fan"}`, `"commentary":"loud"`, `"commentary_removed":false`, `"commentary_restriction":null`} {
		if !bytes.Contains(own.Body.Bytes(), []byte(want)) {
			t.Fatalf("missing %s in %s", want, own.Body.String())
		}
	}
	note := "keep it down"
	started := time.Date(2020, 1, 16, 12, 0, 0, 0, time.UTC)
	reviews.page.Reviews[0].CommentaryRemoved = true
	reviews.own.CommentaryRemoved = true
	reviews.own.Commentary = nil
	reviews.own.CommentaryRestriction = &review.CommentaryRestriction{Reason: "spam", Note: &note, CreatedAt: started}
	listRemoved := requestWith(handler, http.MethodGet, reviewsPath, "", nil, nil)
	if !bytes.Contains(listRemoved.Body.Bytes(), []byte(`"commentary_removed":true`)) || bytes.Contains(listRemoved.Body.Bytes(), []byte(`commentary_restriction`)) {
		t.Fatalf("list removed %s", listRemoved.Body.String())
	}
	ownRemoved := requestWith(handler, http.MethodGet, ownReviewPath, "Bearer t", nil, nil)
	for _, want := range []string{`"commentary_removed":true`, `"commentary_restriction":{"reason":"spam","note":"keep it down","created_at":"2020-01-16T12:00:00Z"}`} {
		if !bytes.Contains(ownRemoved.Body.Bytes(), []byte(want)) {
			t.Fatalf("own removed missing %s in %s", want, ownRemoved.Body.String())
		}
	}
	reviews.err = &review.CommentaryRestrictedError{Restriction: review.CommentaryRestriction{Reason: "spam", Note: &note, CreatedAt: started}}
	locked := requestWith(handler, http.MethodPut, ownReviewPath, "Bearer t", []byte(reviewBodyJSON), plain)
	assertCode(t, locked, http.StatusConflict, "commentary_locked")
	for _, want := range []string{`Commentary on this review is locked.`, `"commentary_restriction":{"reason":"spam","note":"keep it down","created_at":"2020-01-16T12:00:00Z"}`} {
		if !bytes.Contains(locked.Body.Bytes(), []byte(want)) {
			t.Fatalf("locked missing %s in %s", want, locked.Body.String())
		}
	}
	if bytes.Contains(locked.Body.Bytes(), []byte(`operator`)) || bytes.Contains(locked.Body.Bytes(), []byte(`reporter`)) {
		t.Fatalf("locked leaked identity %s", locked.Body.String())
	}
	reviews.err = nil
	reviews.own = sampleOwnReview()
	reviews.page.Reviews[0].CommentaryRemoved = false

	created := requestWith(handler, http.MethodPut, ownReviewPath, "Bearer t", []byte(reviewBodyJSON), plain)
	if created.Code != http.StatusCreated || reviews.save.UserID != caller.UserID || reviews.save.VenueID != reviewVenueID || string(reviews.save.Rating) != "4" || string(reviews.save.Commentary) != `"loud"` {
		t.Fatalf("create %d %s %+v", created.Code, created.Body.String(), reviews.save)
	}
	reviews.result.Created = false
	replaced := requestWith(handler, http.MethodPut, ownReviewPath, "Bearer t", []byte(reviewBodyJSON), plain)
	if replaced.Code != http.StatusOK || !bytes.Contains(replaced.Body.Bytes(), []byte(`"user_id"`)) {
		t.Fatalf("replace %d %s", replaced.Code, replaced.Body.String())
	}
	reviews.err = &review.ValidationError{Fields: map[string]string{"rating": "must be between 1 and 5"}}
	fraction := requestWith(handler, http.MethodPut, ownReviewPath, "Bearer t", []byte(`{"rating":4.0}`), plain)
	assertCode(t, fraction, http.StatusBadRequest, "validation_error")
	if string(reviews.save.Rating) != "4.0" || reviews.save.Commentary != nil || !bytes.Contains(fraction.Body.Bytes(), []byte(`"rating":"must be between 1 and 5"`)) {
		t.Fatalf("fraction %s %+v", fraction.Body.String(), reviews.save)
	}
	reviews.err = nil

	before = reviews.calls
	for _, body := range []string{`{"rating":4,"extra":1}`, `not json`} {
		rec := requestWith(handler, http.MethodPut, ownReviewPath, "Bearer t", []byte(body), plain)
		assertCode(t, rec, http.StatusBadRequest, "validation_error")
		if bytes.Contains(rec.Body.Bytes(), []byte(`"fields"`)) {
			t.Fatalf("%s %s", body, rec.Body.String())
		}
	}
	assertCode(t, requestWith(handler, http.MethodPut, ownReviewPath, "Bearer t", []byte(reviewBodyJSON), map[string]string{"Content-Type": "text/plain"}), http.StatusUnsupportedMediaType, "unsupported_media_type")
	large := []byte(`{"rating":4,"commentary":"` + strings.Repeat("a", 1<<16) + `"}`)
	assertCode(t, requestWith(handler, http.MethodPut, ownReviewPath, "Bearer t", large, plain), http.StatusRequestEntityTooLarge, "body_too_large")
	if reviews.calls != before {
		t.Fatal("rejected body reached the service")
	}

	deleted := requestWith(handler, http.MethodDelete, ownReviewPath, "Bearer t", nil, nil)
	if deleted.Code != http.StatusNoContent || deleted.Body.Len() != 0 || reviews.userID != caller.UserID || reviews.venueID != reviewVenueID {
		t.Fatalf("delete %d %q", deleted.Code, deleted.Body.String())
	}
}

func TestReviewAuth(t *testing.T) {
	methods := []string{http.MethodGet, http.MethodPut, http.MethodDelete}
	reviews := &fakeReviews{}
	headers := map[string]string{"Content-Type": "application/json"}
	anonymous := reviewHandler(&fakeAuth{}, reviews)
	operator := reviewHandler(&fakeAuth{caller: auth.Caller{UserID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc"}, operator: true}, reviews)
	for _, method := range methods {
		assertCode(t, requestWith(anonymous, method, ownReviewPath, "", []byte(reviewBodyJSON), headers), http.StatusUnauthorized, "unauthorized")
		assertForbidden(t, requestWith(operator, method, ownReviewPath, "Bearer t", []byte(reviewBodyJSON), headers))
	}
	if reviews.calls != 0 {
		t.Fatal("rejected caller reached the service")
	}
}

func TestReviewErrors(t *testing.T) {
	until := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		err    error
		status int
		code   string
		want   string
		header string
	}{
		{review.ErrVenueNotFound, 404, "venue_not_found", "Venue not found.", ""},
		{review.ErrReviewNotFound, 404, "review_not_found", "Review not found.", ""},
		{review.ErrVenueNotVerified, 409, "venue_not_verified", "Reviews open after this venue is verified.", ""},
		{profile.ErrProfileRequired, 409, "profile_required", "Create a profile first.", ""},
		{profile.ErrRenameRequired, 403, "profile_rename_required", "", ""},
		{&profile.SuspendedError{Until: &until}, 403, "profile_suspended", `"suspended_until":"2026-11-01T00:00:00Z"`, ""},
		{&review.ValidationError{Fields: map[string]string{"rating": "must be between 1 and 5", "commentary": "must be text"}}, 400, "validation_error", `"commentary":"must be text"`, ""},
		{&review.RateLimitError{RetryAfter: 2}, 429, "rate_limited", "Too many requests.", "2"},
		{errors.New("db down"), 500, "internal_error", "", ""},
	}
	headers := map[string]string{"Content-Type": "application/json"}
	for _, c := range cases {
		reviews := &fakeReviews{err: c.err}
		handler := reviewHandler(&fakeAuth{caller: auth.Caller{UserID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc"}}, reviews)
		rec := requestWith(handler, http.MethodPut, ownReviewPath, "Bearer t", []byte(reviewBodyJSON), headers)
		assertCode(t, rec, c.status, c.code)
		if c.want != "" && !bytes.Contains(rec.Body.Bytes(), []byte(c.want)) {
			t.Fatalf("%s body %s", c.code, rec.Body.String())
		}
		if rec.Header().Get("Retry-After") != c.header {
			t.Fatalf("%s retry %q", c.code, rec.Header().Get("Retry-After"))
		}
	}
}
