package review

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/metalhops/metalhops/backend/internal/idempotency"
)

func moderate(h *writeHarness, actor, reviewID, key, action, reason string, note *string) (ModerationResult, error) {
	return h.service.ModerateReview(context.Background(), ModerationCommand{
		ActorID: actor, ReviewID: reviewID, Action: action, Reason: reason, Note: note, Key: key,
	})
}

func TestRemoveCommentary(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	insertUser(t, h.pool, userC, "", nil)
	reviewID := insertReview(t, h.pool, verifiedVenue, userA, 4, ptr("loud"), t0)
	insertPendingReport(t, h.pool, userB, reviewID, "spam", nil)

	got, err := moderate(h, userC, reviewID, "mod-remove-1", "remove_commentary", "spam", nil)
	if err != nil || !got.Created || got.ActionID == nil || got.Review == nil ||
		got.Review.Commentary != nil || !got.Review.CommentaryRemoved || got.Review.Rating != 4 ||
		!got.Review.CreatedAt.Equal(t0) || !got.Review.UpdatedAt.Equal(t0) {
		t.Fatalf("remove %+v %v", got, err)
	}
	var restrictions int
	var sourceID string
	var started time.Time
	if err := h.pool.QueryRow(context.Background(), `
		select count(*), max(source_action_id::text), max(created_at)
		from review_commentary_restrictions where user_id = $1`, userA).Scan(&restrictions, &sourceID, &started); err != nil ||
		restrictions != 1 || sourceID != *got.ActionID {
		t.Fatalf("restriction %d %s %v", restrictions, sourceID, err)
	}
	assertReportStatus(t, h.pool, reviewID, "actioned", userC)
	var auditAction, reviewer string
	if err := h.pool.QueryRow(context.Background(), `
		select action, reviewer_user_id::text from review_moderation_actions where id = $1`, *got.ActionID).
		Scan(&auditAction, &reviewer); err != nil || auditAction != "commentary_removed" || reviewer != userA {
		t.Fatalf("audit %s %s %v", auditAction, reviewer, err)
	}
	var body []byte
	if err := h.pool.QueryRow(context.Background(), `
		select response_body from idempotency_records
		where user_id = $1 and operation = 'review_moderation_action' and key = 'mod-remove-1'`, userC).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"commentary"`) {
		t.Fatalf("stored body has commentary key: %s", body)
	}
	var invalid *ValidationError
	if _, err := moderate(h, userC, "abc", "mod-remove-2", "remove_commentary", "spam", nil); !errors.As(err, &invalid) ||
		invalid.Fields["review_id"] != "must be a canonical uuid" {
		t.Fatalf("bad id: %v", err)
	}
}

func TestRemoveCommentaryNoop(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userC, "", nil)
	reviewID := insertReview(t, h.pool, verifiedVenue, userA, 4, ptr("loud"), t0)
	if _, err := moderate(h, userC, reviewID, "mod-noop-1", "remove_commentary", "spam", nil); err != nil {
		t.Fatal(err)
	}
	before := tokensLeft(h.limiter, userC)
	got, err := moderate(h, userC, reviewID, "mod-noop-2", "remove_commentary", "spam", nil)
	if err != nil || got.Created || got.ActionID != nil {
		t.Fatalf("noop %+v %v", got, err)
	}
	var audits int
	if err := h.pool.QueryRow(context.Background(), `select count(*) from review_moderation_actions`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("audits %d %v", audits, err)
	}
	if left := tokensLeft(h.limiter, userC); left != before {
		t.Fatalf("tokens %d want %d", left, before)
	}
}

func TestRejectRatingOnlyRemoval(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userC, "", nil)
	reviewID := insertReview(t, h.pool, verifiedVenue, userA, 4, nil, t0)

	if _, err := moderate(h, userC, reviewID, "mod-reject-1", "remove_commentary", "spam", nil); !errors.Is(err, ErrCommentaryNotPublic) {
		t.Fatalf("want commentary_not_public, got %v", err)
	}
	var n int
	if err := h.pool.QueryRow(context.Background(), `select count(*) from review_moderation_actions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("audits %d %v", n, err)
	}
	if err := h.pool.QueryRow(context.Background(), `select count(*) from review_commentary_restrictions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("restrictions %d %v", n, err)
	}
	if err := h.pool.QueryRow(context.Background(), `select count(*) from review_reports`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("reports %d %v", n, err)
	}
	if left := tokensLeft(h.limiter, userC); left != 60 {
		t.Fatalf("tokens %d", left)
	}
}

func TestDeleteReviewReplay(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	insertUser(t, h.pool, userC, "", nil)
	reviewID := insertReview(t, h.pool, verifiedVenue, userA, 4, ptr("loud"), t0)
	insertPendingReport(t, h.pool, userB, reviewID, "spam", nil)
	if _, err := h.pool.Exec(context.Background(), `insert into report_quota_events (user_id) values ($1)`, userB); err != nil {
		t.Fatal(err)
	}
	note := "gone"
	firstRestriction := seedRestriction(t, h, reviewID, userA, verifiedVenue, "harassment", &note)
	var firstSource string
	if err := h.pool.QueryRow(context.Background(), `
		select source_action_id::text from review_commentary_restrictions where user_id = $1`, userA).Scan(&firstSource); err != nil {
		t.Fatal(err)
	}

	got, err := moderate(h, userC, reviewID, "mod-delete-1", "delete_review", "spam", nil)
	if err != nil || !got.Created || got.ActionID == nil || got.Review != nil || got.ReviewID != reviewID {
		t.Fatalf("delete %+v %v", got, err)
	}
	var reports, quota, restrictions int
	var source string
	var started time.Time
	if err := h.pool.QueryRow(context.Background(), `select count(*) from review_reports`).Scan(&reports); err != nil || reports != 0 {
		t.Fatalf("reports %d %v", reports, err)
	}
	if err := h.pool.QueryRow(context.Background(), `select count(*) from report_quota_events where user_id = $1`, userB).Scan(&quota); err != nil || quota != 1 {
		t.Fatalf("quota %d %v", quota, err)
	}
	if err := h.pool.QueryRow(context.Background(), `
		select count(*), max(source_action_id::text), max(created_at)
		from review_commentary_restrictions where user_id = $1`, userA).Scan(&restrictions, &source, &started); err != nil ||
		restrictions != 1 || source != firstSource || !started.Equal(firstRestriction) {
		t.Fatalf("restriction kept %d %s %v (want %s %v)", restrictions, source, started, firstSource, firstRestriction)
	}
	var auditAction string
	if err := h.pool.QueryRow(context.Background(), `
		select action from review_moderation_actions where id = $1`, *got.ActionID).Scan(&auditAction); err != nil ||
		auditAction != "review_deleted" {
		t.Fatalf("audit %s %v", auditAction, err)
	}

	replay, err := moderate(h, userC, reviewID, "mod-delete-1", "delete_review", "spam", nil)
	if err != nil || !replay.Created || replay.ActionID == nil || *replay.ActionID != *got.ActionID {
		t.Fatalf("replay %+v %v", replay, err)
	}
	if _, err := moderate(h, userC, reviewID, "mod-delete-2", "delete_review", "spam", nil); !errors.Is(err, ErrReviewNotFound) {
		t.Fatalf("new key: %v", err)
	}
}

func TestModerateOnDelistedVenue(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, delistedVenue, "delisted")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userC, "", nil)
	reviewID := insertReview(t, h.pool, delistedVenue, userA, 4, ptr("loud"), t0)

	got, err := moderate(h, userC, reviewID, "mod-delisted-1", "remove_commentary", "spam", nil)
	if err != nil || !got.Created {
		t.Fatalf("delisted %+v %v", got, err)
	}
}

func TestGetModerationReview(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, delistedVenue, "delisted")
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	insertUser(t, h.pool, userC, "", nil)

	reviewID := insertReview(t, h.pool, delistedVenue, userA, 4, ptr("loud"), t0)
	insertPendingReport(t, h.pool, userB, reviewID, "spam", nil)
	if _, err := h.pool.Exec(context.Background(), `
		insert into review_reports (reporter_user_id, review_id, reason, status, resolved_at, resolved_by_user_id)
		values ($1, $2, 'harassment', 'dismissed', $3, $4)`, userB, reviewID, t0, userC); err != nil {
		t.Fatal(err)
	}
	actionID := insertModerationAction(t, h.pool, reviewID, userA, delistedVenue, userC, "commentary_removed", "spam", nil, t0)

	got, err := h.service.GetModerationReview(context.Background(), reviewID)
	if err != nil || got.Review.ID != reviewID || got.Review.VenueID != delistedVenue ||
		len(got.Reports) != 1 || got.Reports[0].Reason != "spam" || len(got.History) != 1 ||
		got.History[0].ID != actionID || got.Restriction != nil {
		t.Fatalf("detail %+v %v", got, err)
	}

	// History capped at 20.
	for i := 0; i < 22; i++ {
		insertModerationAction(t, h.pool, reviewID, userA, delistedVenue, userC, "commentary_removed", "spam", nil, t0.Add(time.Duration(i+1)*time.Minute))
	}
	got, err = h.service.GetModerationReview(context.Background(), reviewID)
	if err != nil || len(got.History) != 20 {
		t.Fatalf("history cap %d %v", len(got.History), err)
	}

	// Restriction from a deleted earlier Review on the same Venue.
	early := insertReview(t, h.pool, verifiedVenue, userA, 2, ptr("early"), t0)
	started := t0.Add(2 * time.Hour)
	sourceID := insertModerationAction(t, h.pool, early, userA, verifiedVenue, userC, "review_deleted", "spam", ptr("source"), t0.Add(time.Hour))
	if _, err := h.pool.Exec(context.Background(), `
		insert into review_commentary_restrictions (user_id, venue_id, source_action_id, created_at)
		values ($1, $2, $3, $4)`, userA, verifiedVenue, sourceID, started); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `delete from reviews where id = $1`, early); err != nil {
		t.Fatal(err)
	}
	later := insertReview(t, h.pool, verifiedVenue, userA, 5, nil, t0.Add(3*time.Hour))
	laterAction := insertModerationAction(t, h.pool, later, userA, verifiedVenue, userC, "commentary_removed", "off_topic", nil, t0.Add(4*time.Hour))

	got, err = h.service.GetModerationReview(context.Background(), later)
	if err != nil || got.Restriction == nil || !got.Restriction.CreatedAt.Equal(started) ||
		got.Restriction.Source.ID != sourceID || got.Restriction.Source.Note == nil ||
		*got.Restriction.Source.Note != "source" {
		t.Fatalf("restriction %+v %v", got.Restriction, err)
	}
	for _, a := range got.History {
		if a.ID == sourceID {
			t.Fatalf("source leaked into history: %s (later action %s)", sourceID, laterAction)
		}
	}

	if _, err := h.service.GetModerationReview(context.Background(), unknownVenue); !errors.Is(err, ErrReviewNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestLiftCommentaryRestriction(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userC, "", nil)
	reviewID := insertReview(t, h.pool, verifiedVenue, userA, 4, nil, t0)
	if _, err := h.pool.Exec(context.Background(), `
		update reviews set commentary_removed_at = $2 where id = $1`, reviewID, t0); err != nil {
		t.Fatal(err)
	}
	note := "gone"
	seedRestriction(t, h, reviewID, userA, verifiedVenue, "harassment", &note)

	got, err := moderate(h, userC, reviewID, "lift-1-key", "lift_commentary_restriction", "", nil)
	if err != nil || !got.Created || got.ActionID == nil || got.Review == nil || !got.Review.CommentaryRemoved {
		t.Fatalf("lift %+v %v", got, err)
	}
	var auditAction, reason, reviewer, operator string
	var noteGot *string
	var createdAt time.Time
	if err := h.pool.QueryRow(context.Background(), `
		select action, reason, note, reviewer_user_id::text, operator_user_id::text, created_at
		from review_moderation_actions where id = $1`, *got.ActionID).
		Scan(&auditAction, &reason, &noteGot, &reviewer, &operator, &createdAt); err != nil ||
		auditAction != "commentary_restriction_lifted" || reason != "harassment" || noteGot == nil || *noteGot != "gone" ||
		reviewer != userA || operator != userC || !createdAt.Equal(h.now) {
		t.Fatalf("audit %s %s %v %s %s %v %v", auditAction, reason, noteGot, reviewer, operator, createdAt, err)
	}
	var restrictions int
	if err := h.pool.QueryRow(context.Background(), `select count(*) from review_commentary_restrictions`).Scan(&restrictions); err != nil || restrictions != 0 {
		t.Fatalf("restrictions %d %v", restrictions, err)
	}
	var removedAt, updatedAt time.Time
	var rating int
	if err := h.pool.QueryRow(context.Background(), `
		select commentary_removed_at, updated_at, rating from reviews where id = $1`, reviewID).
		Scan(&removedAt, &updatedAt, &rating); err != nil || !removedAt.Equal(t0) || !updatedAt.Equal(t0) || rating != 4 {
		t.Fatalf("review unchanged %v %v %d %v", removedAt, updatedAt, rating, err)
	}
	if left := tokensLeft(h.limiter, userC); left != 59 {
		t.Fatalf("tokens %d", left)
	}
	var body []byte
	if err := h.pool.QueryRow(context.Background(), `
		select response_body from idempotency_records
		where user_id = $1 and operation = 'review_moderation_action' and key = 'lift-1-key'`, userC).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"commentary"`) {
		t.Fatalf("stored body has commentary key: %s", body)
	}
}

func TestLiftNoop(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userC, "", nil)
	reviewID := insertReview(t, h.pool, verifiedVenue, userA, 4, nil, t0)

	for i := 0; i < 2; i++ {
		got, err := moderate(h, userC, reviewID, "lift-noop", "lift_commentary_restriction", "", nil)
		if err != nil || got.Created || got.ActionID != nil || got.Review == nil {
			t.Fatalf("noop %d %+v %v", i, got, err)
		}
	}
	var audits int
	if err := h.pool.QueryRow(context.Background(), `select count(*) from review_moderation_actions`).Scan(&audits); err != nil || audits != 0 {
		t.Fatalf("audits %d %v", audits, err)
	}
	if left := tokensLeft(h.limiter, userC); left != 60 {
		t.Fatalf("tokens %d", left)
	}
	var n int
	if err := h.pool.QueryRow(context.Background(), `
		select count(*) from idempotency_records where key = $1`, "lift-noop").Scan(&n); err != nil || n != 0 {
		t.Fatalf("idempotency rows %d %v", n, err)
	}
}

func TestLiftReplayAndReuse(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userC, "", nil)
	reviewID := insertReview(t, h.pool, verifiedVenue, userA, 4, nil, t0)
	if _, err := h.pool.Exec(context.Background(), `
		update reviews set commentary_removed_at = $2 where id = $1`, reviewID, t0); err != nil {
		t.Fatal(err)
	}
	seedRestriction(t, h, reviewID, userA, verifiedVenue, "spam", nil)

	got, err := moderate(h, userC, reviewID, "lift-r-key", "lift_commentary_restriction", "", nil)
	if err != nil || !got.Created || got.ActionID == nil {
		t.Fatalf("lift %+v %v", got, err)
	}

	if _, err := h.pool.Exec(context.Background(), `
		update reviews set commentary = 'again', commentary_removed_at = null where id = $1`, reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := moderate(h, userC, reviewID, "remove-after", "remove_commentary", "spam", nil); err != nil {
		t.Fatal(err)
	}
	var restrictions int
	if err := h.pool.QueryRow(context.Background(), `select count(*) from review_commentary_restrictions`).Scan(&restrictions); err != nil || restrictions != 1 {
		t.Fatalf("restriction reimposed %d %v", restrictions, err)
	}

	before := tokensLeft(h.limiter, userC)
	replay, err := moderate(h, userC, reviewID, "lift-r-key", "lift_commentary_restriction", "", nil)
	if err != nil || !replay.Created || replay.ActionID == nil || *replay.ActionID != *got.ActionID {
		t.Fatalf("replay %+v %v", replay, err)
	}
	if err := h.pool.QueryRow(context.Background(), `select count(*) from review_commentary_restrictions`).Scan(&restrictions); err != nil || restrictions != 1 {
		t.Fatalf("restriction still present %d %v", restrictions, err)
	}
	if left := tokensLeft(h.limiter, userC); left != before {
		t.Fatalf("tokens changed by replay: %d want %d", left, before)
	}

	if _, err := moderate(h, userC, reviewID, "lift-r-key", "remove_commentary", "spam", nil); !errors.Is(err, idempotency.ErrReused) {
		t.Fatalf("reused key: %v", err)
	}
}

func TestLiftThroughLaterReview(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userC, "", nil)

	early := insertReview(t, h.pool, verifiedVenue, userA, 2, ptr("early"), t0)
	note := "source note"
	sourceID := insertModerationAction(t, h.pool, early, userA, verifiedVenue, userC, "review_deleted", "harassment", &note, t0)
	if _, err := h.pool.Exec(context.Background(), `
		insert into review_commentary_restrictions (user_id, venue_id, source_action_id, created_at)
		values ($1, $2, $3, $4)`, userA, verifiedVenue, sourceID, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `delete from reviews where id = $1`, early); err != nil {
		t.Fatal(err)
	}
	later := insertReview(t, h.pool, verifiedVenue, userA, 5, nil, t0.Add(2*time.Hour))

	got, err := moderate(h, userC, later, "lift-later", "lift_commentary_restriction", "", nil)
	if err != nil || !got.Created || got.ActionID == nil {
		t.Fatalf("lift %+v %v", got, err)
	}
	var reviewID, reason, reviewer string
	var noteGot *string
	if err := h.pool.QueryRow(context.Background(), `
		select review_id::text, reason, note, reviewer_user_id::text from review_moderation_actions where id = $1`, *got.ActionID).
		Scan(&reviewID, &reason, &noteGot, &reviewer); err != nil || reviewID != later || reason != "harassment" ||
		noteGot == nil || *noteGot != note || reviewer != userA {
		t.Fatalf("audit %s %s %v %s %v", reviewID, reason, noteGot, reviewer, err)
	}
}

func TestLiftOnDelistedVenue(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, delistedVenue, "delisted")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userC, "", nil)
	reviewID := insertReview(t, h.pool, delistedVenue, userA, 4, nil, t0)
	seedRestriction(t, h, reviewID, userA, delistedVenue, "spam", nil)

	got, err := moderate(h, userC, reviewID, "lift-delisted", "lift_commentary_restriction", "", nil)
	if err != nil || !got.Created {
		t.Fatalf("lift on delisted %+v %v", got, err)
	}
}

func TestLiftMissingReview(t *testing.T) {
	h := newWriteHarness(t)
	if _, err := moderate(h, userC, unknownVenue, "lift-miss", "lift_commentary_restriction", "", nil); !errors.Is(err, ErrReviewNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestLiftRateLimited(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userC, "", nil)
	reviewID := insertReview(t, h.pool, verifiedVenue, userA, 4, nil, t0)
	seedRestriction(t, h, reviewID, userA, verifiedVenue, "spam", nil)

	for i := 0; i < 60; i++ {
		if _, _, ok := h.limiter.Reserve(userC); !ok {
			t.Fatalf("drain %d", i)
		}
	}

	_, err := moderate(h, userC, reviewID, "lift-ratelimited", "lift_commentary_restriction", "", nil)
	var limited *RateLimitError
	if !errors.As(err, &limited) {
		t.Fatalf("want rate limit, got %v", err)
	}
	var restrictions, audits int
	if err := h.pool.QueryRow(context.Background(), `select count(*) from review_commentary_restrictions`).Scan(&restrictions); err != nil || restrictions != 1 {
		t.Fatalf("restriction %d %v", restrictions, err)
	}
	if err := h.pool.QueryRow(context.Background(), `
		select count(*) from review_moderation_actions where action = 'commentary_restriction_lifted'`).Scan(&audits); err != nil || audits != 0 {
		t.Fatalf("audit %d %v", audits, err)
	}
}

func TestLiftConcurrent(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userC, "", nil)
	reviewID := insertReview(t, h.pool, verifiedVenue, userA, 4, nil, t0)
	seedRestriction(t, h, reviewID, userA, verifiedVenue, "spam", nil)

	release := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]ModerationResult, 2)
	errs := make([]error, 2)
	for i, key := range []string{"lift-a-key", "lift-b-key"} {
		wg.Add(1)
		go func(i int, key string) {
			defer wg.Done()
			<-release
			results[i], errs[i] = moderate(h, userC, reviewID, key, "lift_commentary_restriction", "", nil)
		}(i, key)
	}
	close(release)
	wg.Wait()

	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("errs %v %v", errs[0], errs[1])
	}
	if results[0].Created == results[1].Created {
		t.Fatalf("created %v %v", results[0].Created, results[1].Created)
	}
	var lifts int
	if err := h.pool.QueryRow(context.Background(), `
		select count(*) from review_moderation_actions where action = 'commentary_restriction_lifted'`).Scan(&lifts); err != nil || lifts != 1 {
		t.Fatalf("lifts %d %v", lifts, err)
	}
}
