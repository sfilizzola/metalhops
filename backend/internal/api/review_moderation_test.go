package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/metalhops/metalhops/backend/internal/auth"
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/review"
)

type fakeReviewModeration struct {
	report       review.Report
	page         review.ReportPage
	detail       review.ModerationDetail
	result       review.ModerationResult
	item         review.ReportItem
	err          error
	calls        int
	cmd          review.ReportCommand
	modCmd       review.ModerationCommand
	limit        int
	cursor       string
	detailID     string
	dismissActor string
	dismissID    string
}

func (f *fakeReviewModeration) ReportReview(_ context.Context, c review.ReportCommand) (review.Report, error) {
	f.calls++
	f.cmd = c
	return f.report, f.err
}
func (f *fakeReviewModeration) ListReviewReports(_ context.Context, limit int, cursor string) (review.ReportPage, error) {
	f.calls++
	f.limit, f.cursor = limit, cursor
	return f.page, f.err
}
func (f *fakeReviewModeration) GetModerationReview(_ context.Context, id string) (review.ModerationDetail, error) {
	f.calls++
	f.detailID = id
	return f.detail, f.err
}
func (f *fakeReviewModeration) ModerateReview(_ context.Context, c review.ModerationCommand) (review.ModerationResult, error) {
	f.calls++
	f.modCmd = c
	return f.result, f.err
}
func (f *fakeReviewModeration) DismissReviewReport(_ context.Context, actor, id string) (review.ReportItem, error) {
	f.calls++
	f.dismissActor, f.dismissID = actor, id
	return f.item, f.err
}

const (
	modReviewID = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	modReportID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
)

func reviewModerationHandler(authenticator *fakeAuth, fake *fakeReviewModeration) http.Handler {
	return New(Options{
		Readiness: readyOK(), PublicLimit: allowAll, Auth: authenticator, ReviewModeration: fake,
		Logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)),
	})
}

func TestReviewModerationRoutes(t *testing.T) {
	at := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	actionID := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	fake := &fakeReviewModeration{
		report: review.Report{ID: modReportID, ReviewID: modReviewID, VenueID: reviewVenueID, Reason: "spam", Status: "pending", CreatedAt: at},
		result: review.ModerationResult{
			Created: true, ActionID: &actionID, ReviewID: modReviewID,
			Review: &review.OperatorReview{ID: modReviewID, VenueID: reviewVenueID, UserID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Rating: 4, CommentaryRemoved: true, CreatedAt: at, UpdatedAt: at},
		},
		item: review.ReportItem{ID: modReportID, Reason: "spam", Status: "dismissed", ReviewID: modReviewID, VenueID: reviewVenueID, Rating: 4, CreatedAt: at},
	}
	authenticator := &fakeAuth{caller: auth.Caller{UserID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}}
	handler := reviewModerationHandler(authenticator, fake)
	reportPath := "/v1/venues/" + reviewVenueID + "/reviews/" + modReviewID + "/reports"
	listPath := "/v1/moderation/review-reports"
	detailPath := "/v1/moderation/reviews/" + modReviewID
	actionPath := detailPath + "/actions"
	dismissPath := "/v1/moderation/review-reports/" + modReportID
	keyed := map[string]string{"Content-Type": "application/json", "Idempotency-Key": "review-key-1"}
	plain := map[string]string{"Content-Type": "application/json"}

	for _, path := range []string{reportPath, listPath, detailPath, actionPath, dismissPath} {
		method := http.MethodGet
		body := []byte(nil)
		headers := map[string]string(nil)
		switch path {
		case reportPath, actionPath:
			method, body, headers = http.MethodPost, []byte(`{"reason":"spam"}`), keyed
		case dismissPath:
			method, body, headers = http.MethodPatch, []byte(`{"status":"dismissed"}`), plain
		}
		assertCode(t, requestWith(handler, method, path, "", body, headers), http.StatusUnauthorized, "unauthorized")
	}
	if fake.calls != 0 {
		t.Fatal("anonymous reached service")
	}

	for _, path := range []string{listPath, detailPath, actionPath, dismissPath} {
		method := http.MethodGet
		body := []byte(nil)
		headers := map[string]string(nil)
		switch path {
		case actionPath:
			method, body, headers = http.MethodPost, []byte(`{"action":"remove_commentary","reason":"spam"}`), keyed
		case dismissPath:
			method, body, headers = http.MethodPatch, []byte(`{"status":"dismissed"}`), plain
		}
		assertCode(t, requestWith(handler, method, path, "Bearer t", body, headers), http.StatusForbidden, "forbidden")
	}
	if fake.calls != 0 {
		t.Fatal("app user reached operator routes")
	}

	authenticator.operator = true
	assertCode(t, requestWith(handler, http.MethodPost, reportPath, "Bearer t", []byte(`{"reason":"spam"}`), keyed), http.StatusForbidden, "forbidden")
	authenticator.operator = false
	if fake.calls != 0 {
		t.Fatal("operator reached report route")
	}

	for _, c := range []struct {
		err    error
		status int
		code   string
	}{
		{review.ErrSelfReport, 400, "self_report"},
		{review.ErrCommentaryNotPublic, 400, "commentary_not_public"},
		{review.ErrReportPending, 409, "review_already_reported"},
		{&profile.QuotaError{RetryAfter: 30}, 429, "quota_exceeded"},
		{&review.RateLimitError{RetryAfter: 4}, 429, "rate_limited"},
	} {
		fake.err = c.err
		rec := requestWith(handler, http.MethodPost, reportPath, "Bearer t", []byte(`{"reason":"spam"}`), keyed)
		assertCode(t, rec, c.status, c.code)
		if c.status == 429 && rec.Header().Get("Retry-After") == "" {
			t.Fatalf("%s: no Retry-After", c.code)
		}
	}
	fake.err = nil

	authenticator.operator = true
	fake.err = review.ErrCommentaryNotPublic
	assertCode(t, requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"remove_commentary","reason":"spam"}`), keyed), http.StatusConflict, "commentary_not_public")
	fake.err = review.ErrReportNotFound
	assertCode(t, requestWith(handler, http.MethodPatch, dismissPath, "Bearer t", []byte(`{"status":"dismissed"}`), plain), http.StatusNotFound, "review_report_not_found")
	fake.err = review.ErrReportResolved
	assertCode(t, requestWith(handler, http.MethodPatch, dismissPath, "Bearer t", []byte(`{"status":"dismissed"}`), plain), http.StatusConflict, "report_already_resolved")
	fake.err = nil

	authenticator.operator = false
	before := fake.calls
	assertCode(t, requestWith(handler, http.MethodPost, reportPath, "Bearer t", []byte(`{"reason":"spam"}`), plain), http.StatusBadRequest, "validation_error")
	assertCode(t, requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"remove_commentary","reason":"spam"}`), plain), http.StatusForbidden, "forbidden")
	authenticator.operator = true
	assertCode(t, requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"remove_commentary","reason":"spam"}`), plain), http.StatusBadRequest, "validation_error")
	if fake.calls != before {
		t.Fatal("missing key reached service")
	}
	dismissed := requestWith(handler, http.MethodPatch, dismissPath, "Bearer t", []byte(`{"status":"dismissed"}`), plain)
	if dismissed.Code != http.StatusOK || fake.dismissID != modReportID || fake.dismissActor == "" {
		t.Fatalf("dismiss %d %+v %s", dismissed.Code, fake, dismissed.Body.String())
	}

	fake.result.Created = true
	created := requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"remove_commentary","reason":"spam"}`), keyed)
	if created.Code != http.StatusCreated || !bytes.Contains(created.Body.Bytes(), []byte(`"action_id":"`+actionID+`"`)) {
		t.Fatalf("created %d %s", created.Code, created.Body.String())
	}
	fake.result.Created = false
	fake.result.ActionID = nil
	noop := requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"remove_commentary","reason":"spam"}`), keyed)
	if noop.Code != http.StatusOK || !bytes.Contains(noop.Body.Bytes(), []byte(`"action_id":null`)) {
		t.Fatalf("noop %d %s", noop.Code, noop.Body.String())
	}
	fake.result.Created = true
	fake.result.ActionID = &actionID
	fake.result.Review = nil
	deleted := requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"delete_review","reason":"spam"}`), keyed)
	if deleted.Code != http.StatusCreated || !bytes.Contains(deleted.Body.Bytes(), []byte(`"review":null`)) {
		t.Fatalf("delete %d %s", deleted.Code, deleted.Body.String())
	}

	assertCode(t, requestWith(handler, http.MethodGet, listPath+"?limit=0", "Bearer t", nil, nil), http.StatusBadRequest, "validation_error")
	rec := requestWith(handler, http.MethodGet, listPath+"?limit=0", "Bearer t", nil, nil)
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"limit":"must be from 1 to 100"`)) {
		t.Fatalf("limit0 %s", rec.Body.String())
	}
	assertCode(t, requestWith(handler, http.MethodGet, listPath+"?extra=1", "Bearer t", nil, nil), http.StatusBadRequest, "validation_error")
}

func TestReviewLiftRoute(t *testing.T) {
	actionID := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	fake := &fakeReviewModeration{
		result: review.ModerationResult{Created: true, ActionID: &actionID, ReviewID: modReviewID},
	}
	authenticator := &fakeAuth{caller: auth.Caller{UserID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}, operator: true}
	handler := reviewModerationHandler(authenticator, fake)
	actionPath := "/v1/moderation/reviews/" + modReviewID + "/actions"
	keyed := map[string]string{"Content-Type": "application/json", "Idempotency-Key": "review-lift-key"}

	created := requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"lift_commentary_restriction"}`), keyed)
	if created.Code != http.StatusCreated || fake.modCmd.Action != "lift_commentary_restriction" || fake.modCmd.Reason != "" || fake.modCmd.Note != nil {
		t.Fatalf("created %d %+v %s", created.Code, fake.modCmd, created.Body.String())
	}

	fake.modCmd = review.ModerationCommand{}
	nulled := requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"lift_commentary_restriction","reason":null,"note":null}`), keyed)
	if nulled.Code != http.StatusCreated || fake.modCmd.Reason != "" || fake.modCmd.Note != nil {
		t.Fatalf("null %d %+v %s", nulled.Code, fake.modCmd, nulled.Body.String())
	}

	fake.result.Created = false
	fake.result.ActionID = nil
	noop := requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"lift_commentary_restriction"}`), keyed)
	if noop.Code != http.StatusOK || !bytes.Contains(noop.Body.Bytes(), []byte(`"action_id":null`)) {
		t.Fatalf("noop %d %s", noop.Code, noop.Body.String())
	}

	fake.err = &review.ValidationError{Fields: map[string]string{"note": "must be omitted"}}
	rec := requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"lift_commentary_restriction"}`), keyed)
	assertCode(t, rec, http.StatusBadRequest, "validation_error")
	if !bytes.Contains(rec.Body.Bytes(), []byte("must be omitted")) {
		t.Fatalf("validation body %s", rec.Body.String())
	}

	fake.err = review.ErrReviewNotFound
	assertCode(t, requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"lift_commentary_restriction"}`), keyed), http.StatusNotFound, "review_not_found")
	fake.err = nil

	authenticator.operator = false
	before := fake.calls
	assertCode(t, requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"lift_commentary_restriction"}`), keyed), http.StatusForbidden, "forbidden")
	if fake.calls != before {
		t.Fatal("app user reached service")
	}
}
