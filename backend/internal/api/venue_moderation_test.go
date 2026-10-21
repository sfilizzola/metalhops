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

type fakeVenueModeration struct {
	report    venue.VenueReport
	page      venue.ReportPage
	detail    venue.ModerationVenue
	result    venue.ModerationResult
	moderated venue.Moderated
	item      venue.ReportItem
	err       error
	calls     int

	reportCmd    venue.ReportCommand
	moderateCmd  venue.ModerationCommand
	correctCmd   venue.OperatorCorrectCommand
	limit        int
	cursor       string
	detailID     string
	dismissActor string
	dismissID    string
}

func (f *fakeVenueModeration) ReportVenue(_ context.Context, c venue.ReportCommand) (venue.VenueReport, error) {
	f.calls++
	f.reportCmd = c
	return f.report, f.err
}
func (f *fakeVenueModeration) ListVenueReports(_ context.Context, limit int, cursor string) (venue.ReportPage, error) {
	f.calls++
	f.limit, f.cursor = limit, cursor
	return f.page, f.err
}
func (f *fakeVenueModeration) GetModerationVenue(_ context.Context, id string) (venue.ModerationVenue, error) {
	f.calls++
	f.detailID = id
	return f.detail, f.err
}
func (f *fakeVenueModeration) ModerateVenue(_ context.Context, c venue.ModerationCommand) (venue.ModerationResult, error) {
	f.calls++
	f.moderateCmd = c
	return f.result, f.err
}
func (f *fakeVenueModeration) CorrectVenue(_ context.Context, c venue.OperatorCorrectCommand) (venue.Moderated, error) {
	f.calls++
	f.correctCmd = c
	return f.moderated, f.err
}
func (f *fakeVenueModeration) DismissVenueReport(_ context.Context, actor, id string) (venue.ReportItem, error) {
	f.calls++
	f.dismissActor, f.dismissID = actor, id
	return f.item, f.err
}

const (
	venueUUID  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	reportUUID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
)

func moderationVenueHandler(authenticator *fakeAuth, fake *fakeVenueModeration) http.Handler {
	return New(Options{
		Readiness: readyOK(), PublicLimit: allowAll, Auth: authenticator, VenueModeration: fake,
		Logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)),
	})
}

func TestVenueReportRoute(t *testing.T) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	fake := &fakeVenueModeration{report: venue.VenueReport{ID: reportUUID, VenueID: venueUUID, Reason: "moved", Status: "pending", CreatedAt: at}}
	authenticator := &fakeAuth{caller: auth.Caller{UserID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd"}}
	handler := moderationVenueHandler(authenticator, fake)
	path := "/v1/venues/" + venueUUID + "/reports"
	keyed := map[string]string{"Content-Type": "application/json", "Idempotency-Key": "report-key-1"}

	rec := requestWith(handler, http.MethodPost, path, "Bearer t", []byte(`{"reason":"moved"}`), keyed)
	if rec.Code != http.StatusCreated || fake.reportCmd.Key != "report-key-1" || fake.reportCmd.VenueID != venueUUID || fake.reportCmd.Reason != "moved" {
		t.Fatalf("create %d %s %+v", rec.Code, rec.Body.String(), fake.reportCmd)
	}
	for _, want := range []string{`"venue_id":"` + venueUUID + `"`, `"status":"pending"`, `"created_at":"2026-10-20T12:00:00Z"`} {
		if !bytes.Contains(rec.Body.Bytes(), []byte(want)) {
			t.Fatalf("missing %s in %s", want, rec.Body.String())
		}
	}
	before := fake.calls
	assertCode(t, requestWith(handler, http.MethodPost, path, "Bearer t", []byte(`{"reason":"moved"}`), map[string]string{"Content-Type": "application/json"}), http.StatusBadRequest, "validation_error")
	assertCode(t, requestWith(handler, http.MethodPost, path, "Bearer t", []byte(`not json`), keyed), http.StatusBadRequest, "validation_error")
	if fake.calls != before {
		t.Fatal("invalid requests reached the service")
	}
	authenticator.operator = true
	assertCode(t, requestWith(handler, http.MethodPost, path, "Bearer t", []byte(`{"reason":"moved"}`), keyed), http.StatusForbidden, "forbidden")
	authenticator.operator = false

	for _, c := range []struct {
		err    error
		status int
		code   string
	}{
		{venue.ErrNotFound, 404, "venue_not_found"},
		{venue.ErrSelfReport, 400, "self_report"},
		{venue.ErrReportPending, 409, "venue_already_reported"},
		{&profile.QuotaError{RetryAfter: 30}, 429, "quota_exceeded"},
		{&venue.RateLimitError{RetryAfter: 3}, 429, "rate_limited"},
		{&venue.ValidationError{Fields: map[string]string{"note": "required"}}, 400, "validation_error"},
		{idempotency.ErrReused, 409, "idempotency_key_reused"},
		{profile.ErrProfileRequired, 409, "profile_required"},
		{errors.New("boom"), 500, "internal_error"},
	} {
		fake.err = c.err
		rec := requestWith(handler, http.MethodPost, path, "Bearer t", []byte(`{"reason":"moved"}`), keyed)
		assertCode(t, rec, c.status, c.code)
		if c.status == 429 && rec.Header().Get("Retry-After") == "" {
			t.Fatalf("%s: no Retry-After", c.code)
		}
		if bytes.Contains(rec.Body.Bytes(), []byte("boom")) {
			t.Fatal("leaked internal error")
		}
	}
}

func TestVenueModerationReads(t *testing.T) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	reporter := &profile.Profile{ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Username: "rep", DisplayName: "Rep", Status: "active", CreatedAt: at, UpdatedAt: at}
	cursor := "next"
	rejection := "duplicate"
	fake := &fakeVenueModeration{
		page: venue.ReportPage{Items: []venue.ReportItem{{
			ID: reportUUID, Reason: "moved", Status: "pending", CreatedAt: at, Reporter: reporter,
			Venue: venue.VenueSummary{ID: venueUUID, Name: "Iron", City: "Oslo", CountryCode: "NO", VerificationStatus: "verified"},
		}}, NextCursor: &cursor},
		detail: venue.ModerationVenue{
			Moderated: venue.Moderated{Submission: sampleSubmission()},
			Reports:   []venue.PendingReport{{ID: reportUUID, Reason: "moved", CreatedAt: at, Reporter: reporter}},
			History: []venue.ModerationAction{{
				ID: reportUUID, Action: "reject", PriorStatus: "rejected", NewStatus: "rejected",
				RejectionReason: &rejection, CreatedAt: at,
			}},
		},
	}
	authenticator := &fakeAuth{caller: auth.Caller{UserID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd"}}
	handler := moderationVenueHandler(authenticator, fake)
	paths := []string{"/v1/moderation/venue-reports", "/v1/moderation/venues/" + venueUUID}

	for _, p := range paths {
		rec := authed(handler, http.MethodGet, p, "", nil)
		assertCode(t, rec, http.StatusForbidden, "forbidden")
	}
	if fake.calls != 0 {
		t.Fatal("app user reached operator reads")
	}
	authenticator.operator = true

	list := authed(handler, http.MethodGet, paths[0], "", nil)
	if list.Code != http.StatusOK || fake.limit != 20 || fake.cursor != "" {
		t.Fatalf("list %d limit %d %s", list.Code, fake.limit, list.Body.String())
	}
	for _, want := range []string{`"next_cursor":"next"`, `"reporter":{`, `"submitter":null`, `"venue":{"id":"` + venueUUID + `"`} {
		if !bytes.Contains(list.Body.Bytes(), []byte(want)) {
			t.Fatalf("missing %s in %s", want, list.Body.String())
		}
	}
	authed(handler, http.MethodGet, paths[0]+"?limit=5&cursor=abc", "", nil)
	if fake.limit != 5 || fake.cursor != "abc" {
		t.Fatalf("params %d %q", fake.limit, fake.cursor)
	}
	assertCode(t, authed(handler, http.MethodGet, paths[0]+"?limit=x", "", nil), http.StatusBadRequest, "validation_error")

	detail := authed(handler, http.MethodGet, paths[1], "", nil)
	if detail.Code != http.StatusOK || fake.detailID != venueUUID {
		t.Fatalf("detail %d %s", detail.Code, detail.Body.String())
	}
	for _, want := range []string{
		`"delisting_reason":null`, `"rejection_reason":null`, `"submitter":null`, `"reports":[{`,
		`"history":[{"id":"` + reportUUID + `","action":"reject","prior_status":"rejected","new_status":"rejected",` +
			`"rejection_reason":"duplicate","rejection_note":null,"delisting_reason":null,"delisting_note":null,` +
			`"actor_user_id":null,"created_at":"2026-10-20T12:00:00Z"}]`,
	} {
		if !bytes.Contains(detail.Body.Bytes(), []byte(want)) {
			t.Fatalf("missing %s in %s", want, detail.Body.String())
		}
	}
	fake.err = venue.ErrNotFound
	assertCode(t, authed(handler, http.MethodGet, paths[1], "", nil), http.StatusNotFound, "venue_not_found")
}

func TestVenueModerationWrites(t *testing.T) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	actionID := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	moderated := venue.Moderated{Submission: sampleSubmission()}
	moderated.VerificationStatus = "delisted"
	reason := "permanently_closed"
	moderated.DelistingReason = &reason
	fake := &fakeVenueModeration{
		result:    venue.ModerationResult{Created: true, ActionID: &actionID, Venue: moderated},
		moderated: moderated,
		item:      venue.ReportItem{ID: reportUUID, Reason: "moved", Status: "dismissed", CreatedAt: at, Venue: venue.VenueSummary{ID: venueUUID}},
	}
	caller := auth.Caller{UserID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd"}
	authenticator := &fakeAuth{caller: caller}
	handler := moderationVenueHandler(authenticator, fake)
	actionPath := "/v1/moderation/venues/" + venueUUID + "/actions"
	putPath := "/v1/moderation/venues/" + venueUUID
	patchPath := "/v1/moderation/venue-reports/" + reportUUID
	keyed := map[string]string{"Content-Type": "application/json", "Idempotency-Key": "action-key-1"}
	plain := map[string]string{"Content-Type": "application/json"}

	for _, c := range []struct {
		method, path, body string
	}{
		{http.MethodPost, actionPath, `{"action":"verify"}`},
		{http.MethodPut, putPath, submissionBodyJSON},
		{http.MethodPatch, patchPath, `{"status":"dismissed"}`},
	} {
		assertCode(t, requestWith(handler, c.method, c.path, "Bearer t", []byte(c.body), keyed), http.StatusForbidden, "forbidden")
	}
	if fake.calls != 0 {
		t.Fatal("app user reached operator writes")
	}
	authenticator.operator = true

	created := requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"delist","reason":"permanently_closed","note":"gone"}`), keyed)
	if created.Code != http.StatusCreated || fake.moderateCmd.ActorID != caller.UserID || fake.moderateCmd.VenueID != venueUUID ||
		fake.moderateCmd.Action != "delist" || !fake.moderateCmd.ReasonSet || fake.moderateCmd.Reason != "permanently_closed" ||
		fake.moderateCmd.Note == nil || *fake.moderateCmd.Note != "gone" || fake.moderateCmd.Key != "action-key-1" {
		t.Fatalf("action %d %s %+v", created.Code, created.Body.String(), fake.moderateCmd)
	}
	for _, want := range []string{`"action_id":"` + actionID + `"`, `"delisting_reason":"permanently_closed"`, `"verification_status":"delisted"`} {
		if !bytes.Contains(created.Body.Bytes(), []byte(want)) {
			t.Fatalf("missing %s in %s", want, created.Body.String())
		}
	}
	assertCode(t, requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"verify"}`), plain), http.StatusBadRequest, "validation_error")
	fake.result.Created, fake.result.ActionID = false, nil
	noop := requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"delist","reason":"permanently_closed"}`), keyed)
	if noop.Code != http.StatusOK || !bytes.Contains(noop.Body.Bytes(), []byte(`"action_id":null`)) {
		t.Fatalf("no-op %d %s", noop.Code, noop.Body.String())
	}
	fake.err = &venue.InvalidTransitionError{Action: "verify", Status: "rejected"}
	assertCode(t, requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"verify"}`), keyed), http.StatusConflict, "invalid_venue_transition")
	fake.err = venue.ErrIncomplete
	assertCode(t, requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"verify"}`), keyed), http.StatusConflict, "venue_incomplete")
	fake.err = nil

	put := requestWith(handler, http.MethodPut, putPath, "Bearer t", []byte(submissionBodyJSON), plain)
	if put.Code != http.StatusOK || fake.correctCmd.VenueID != venueUUID || fake.correctCmd.ActorID != caller.UserID || fake.correctCmd.Fields.Name == nil {
		t.Fatalf("put %d %s %+v", put.Code, put.Body.String(), fake.correctCmd)
	}

	patch := requestWith(handler, http.MethodPatch, patchPath, "Bearer t", []byte(`{"status":"dismissed"}`), plain)
	if patch.Code != http.StatusOK || fake.dismissID != reportUUID || fake.dismissActor != caller.UserID {
		t.Fatalf("patch %d %s", patch.Code, patch.Body.String())
	}
	assertCode(t, requestWith(handler, http.MethodPatch, patchPath, "Bearer t", []byte(`{"status":"actioned"}`), plain), http.StatusBadRequest, "validation_error")
	for _, c := range []struct {
		err    error
		status int
		code   string
	}{
		{venue.ErrReportNotFound, 404, "venue_report_not_found"},
		{venue.ErrReportResolved, 409, "report_already_resolved"},
	} {
		fake.err = c.err
		assertCode(t, requestWith(handler, http.MethodPatch, patchPath, "Bearer t", []byte(`{"status":"dismissed"}`), plain), c.status, c.code)
	}
}

func TestVenueModerationLogsItsOwnLabel(t *testing.T) {
	var logs bytes.Buffer
	fake := &fakeVenueModeration{err: errors.New("boom")}
	handler := New(Options{
		Readiness: readyOK(), PublicLimit: allowAll, Auth: &fakeAuth{operator: true}, VenueModeration: fake,
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	})
	assertCode(t, authed(handler, http.MethodGet, "/v1/moderation/venues/"+venueUUID, "", nil), http.StatusInternalServerError, "internal_error")
	if !bytes.Contains(logs.Bytes(), []byte(`"msg":"venue moderation"`)) || bytes.Contains(logs.Bytes(), []byte("venue submission")) {
		t.Fatalf("log %s", logs.String())
	}
}
