package review

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/metalhops/metalhops/backend/internal/profile"
)

func report(h *writeHarness, reporter, venueID, reviewID, key, reason string, note *string) (Report, error) {
	return h.service.ReportReview(context.Background(), ReportCommand{
		ReporterID: reporter, VenueID: venueID, ReviewID: reviewID, Reason: reason, Note: note, Key: key,
	})
}

func TestReportCheckOrder(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertVenue(t, h.pool, pendingVenue, "pending")
	insertVenue(t, h.pool, delistedVenue, "delisted")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	insertUser(t, h.pool, userC, "active", nil)
	ownWithText := insertReview(t, h.pool, verifiedVenue, userA, 4, ptr("loud"), t0)
	ownRatingOnly := insertReview(t, h.pool, pendingVenue, userA, 3, nil, t0)
	_ = ownRatingOnly
	// Own rating-only on the verified venue needs a different author setup: use userA's review
	// after clearing another user's? Plan: own review including rating-only → ErrSelfReport.
	// Seed a rating-only own review on verified by replacing: use a second venue for that case.
	insertVenue(t, h.pool, rejectedVenue, "verified")
	ownRatingVerified := insertReview(t, h.pool, rejectedVenue, userA, 2, nil, t0)
	otherRatingOnly := insertReview(t, h.pool, verifiedVenue, userB, 3, nil, t0)
	otherWithText := insertReview(t, h.pool, verifiedVenue, userC, 5, ptr("cold"), t0)

	var invalid *ValidationError
	if _, err := report(h, userB, "abc", otherWithText, "report-k1", "spam", nil); !errors.As(err, &invalid) ||
		invalid.Fields["venue_id"] != "must be a canonical uuid" {
		t.Fatalf("venue id: %v", err)
	}
	if _, err := report(h, userB, verifiedVenue, "abc", "report-k2", "spam", nil); !errors.As(err, &invalid) ||
		invalid.Fields["review_id"] != "must be a canonical uuid" {
		t.Fatalf("review id: %v", err)
	}
	if _, err := report(h, userB, delistedVenue, otherWithText, "report-k3", "spam", nil); !errors.Is(err, ErrVenueNotFound) {
		t.Fatalf("hidden: %v", err)
	}
	if _, err := report(h, userB, pendingVenue, ownRatingOnly, "report-k4", "spam", nil); !errors.Is(err, ErrVenueNotVerified) {
		t.Fatalf("pending: %v", err)
	}
	unknownReview := "ffffffff-ffff-4fff-8fff-ffffffffffff"
	if _, err := report(h, userB, verifiedVenue, unknownReview, "report-k5", "spam", nil); !errors.Is(err, ErrReviewNotFound) {
		t.Fatalf("unknown review: %v", err)
	}
	if _, err := report(h, userA, verifiedVenue, ownWithText, "report-k6", "spam", nil); !errors.Is(err, ErrSelfReport) {
		t.Fatalf("own with text: %v", err)
	}
	if _, err := report(h, userA, rejectedVenue, ownRatingVerified, "report-k7", "spam", nil); !errors.Is(err, ErrSelfReport) {
		t.Fatalf("own rating-only: %v", err)
	}
	if _, err := report(h, userA, verifiedVenue, otherRatingOnly, "report-k8", "spam", nil); !errors.Is(err, ErrCommentaryNotPublic) {
		t.Fatalf("rating-only other: %v", err)
	}

	got, err := report(h, userB, verifiedVenue, otherWithText, "report-k9", "spam", nil)
	if err != nil || got.Status != "pending" || got.ReviewID != otherWithText || got.VenueID != verifiedVenue {
		t.Fatalf("report %+v %v", got, err)
	}
	var commentary string
	if err := h.pool.QueryRow(context.Background(), `select commentary from reviews where id = $1`, otherWithText).Scan(&commentary); err != nil || commentary != "cold" {
		t.Fatalf("commentary %q %v", commentary, err)
	}
	var quota int
	if err := h.pool.QueryRow(context.Background(), `select count(*) from report_quota_events where user_id = $1`, userB).Scan(&quota); err != nil || quota != 1 {
		t.Fatalf("quota %d %v", quota, err)
	}
	if left := tokensLeft(h.limiter, userB); left != 59 {
		t.Fatalf("tokens %d", left)
	}
	if _, err := report(h, userB, verifiedVenue, otherWithText, "report-k10", "harassment", nil); !errors.Is(err, ErrReportPending) {
		t.Fatalf("pending: %v", err)
	}
	if err := h.pool.QueryRow(context.Background(), `select count(*) from report_quota_events where user_id = $1`, userB).Scan(&quota); err != nil || quota != 1 {
		t.Fatalf("quota after pending %d %v", quota, err)
	}
	if left := tokensLeft(h.limiter, userB); left != 59 {
		t.Fatalf("tokens after pending %d", left)
	}

	if _, err := h.pool.Exec(context.Background(), `
		insert into report_quota_events (user_id, created_at)
		select $1, $2 from generate_series(1, 20)`, userA, t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	var quotaErr *profile.QuotaError
	if _, err := report(h, userA, verifiedVenue, otherWithText, "report-k11", "spam", nil); !errors.As(err, &quotaErr) {
		t.Fatalf("quota: %v", err)
	}
	var reports int
	if err := h.pool.QueryRow(context.Background(), `
		select count(*) from review_reports where reporter_user_id = $1`, userA).Scan(&reports); err != nil || reports != 0 {
		t.Fatalf("reports %d %v", reports, err)
	}
}

func TestReportProfileBeforeVenue(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, delistedVenue, "delisted")
	insertUser(t, h.pool, userA, "suspended", nil)
	insertUser(t, h.pool, userB, "active", nil)
	reviewID := insertReview(t, h.pool, delistedVenue, userB, 4, ptr("loud"), t0)

	_, err := report(h, userA, delistedVenue, reviewID, "report-s1", "spam", nil)
	var suspended *profile.SuspendedError
	if !errors.As(err, &suspended) {
		t.Fatalf("want suspended, got %v", err)
	}
	if errors.Is(err, ErrVenueNotFound) {
		t.Fatal("venue check ran before profile")
	}
}

func TestReportReplayStopsWhenCommentaryGone(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	reviewID := insertReview(t, h.pool, verifiedVenue, userA, 4, ptr("loud"), t0)

	if _, err := report(h, userB, verifiedVenue, reviewID, "report-gone-1", "spam", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `
		update reviews set commentary = null where id = $1`, reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := report(h, userB, verifiedVenue, reviewID, "report-gone-1", "spam", nil); !errors.Is(err, ErrCommentaryNotPublic) {
		t.Fatalf("want commentary_not_public, got %v", err)
	}
	var quota int
	if err := h.pool.QueryRow(context.Background(), `select count(*) from report_quota_events where user_id = $1`, userB).Scan(&quota); err != nil || quota != 1 {
		t.Fatalf("quota %d %v", quota, err)
	}
}

func TestReportReplayWhilePublic(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	reviewID := insertReview(t, h.pool, verifiedVenue, userA, 4, ptr("loud"), t0)

	first, err := report(h, userB, verifiedVenue, reviewID, "report-pub-1", "spam", nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := report(h, userB, verifiedVenue, reviewID, "report-pub-1", "spam", nil)
	if err != nil || second.ID != first.ID {
		t.Fatalf("replay %+v %v", second, err)
	}
	var quota int
	if err := h.pool.QueryRow(context.Background(), `select count(*) from report_quota_events where user_id = $1`, userB).Scan(&quota); err != nil || quota != 1 {
		t.Fatalf("quota %d %v", quota, err)
	}
}

func TestReportReplayAfterAuthorReplacement(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	reviewID := insertReview(t, h.pool, verifiedVenue, userA, 4, ptr("old"), t0)

	first, err := report(h, userB, verifiedVenue, reviewID, "report-replace-1", "spam", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.save(userA, verifiedVenue, "4", json.RawMessage(`"new"`)); err != nil {
		t.Fatal(err)
	}
	replay, err := report(h, userB, verifiedVenue, reviewID, "report-replace-1", "spam", nil)
	if err != nil || replay.ID != first.ID || replay.Status != "pending" {
		t.Fatalf("replay %+v %v", replay, err)
	}
	var quota int
	if err := h.pool.QueryRow(context.Background(), `select count(*) from report_quota_events where user_id = $1`, userB).Scan(&quota); err != nil || quota != 1 {
		t.Fatalf("quota %d %v", quota, err)
	}
	if left := tokensLeft(h.limiter, userB); left != 59 {
		t.Fatalf("tokens %d", left)
	}

	second, err := report(h, userB, verifiedVenue, reviewID, "report-replace-2", "harassment", nil)
	if err != nil || second.ID == first.ID || second.Status != "pending" {
		t.Fatalf("new report %+v %v", second, err)
	}
}
