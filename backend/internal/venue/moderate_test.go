package venue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/metalhops/metalhops/backend/internal/idempotency"
)

const operatorID = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"

func insertOperator(t *testing.T, h *harness) {
	t.Helper()
	insertUser(t, h.pool, operatorID, "", nil)
	if _, err := h.pool.Exec(context.Background(), `insert into platform_operators (user_id) values ($1)`, operatorID); err != nil {
		t.Fatal(err)
	}
}

func act(h *harness, venueID, action, key string, reason string, note *string) (ModerationResult, error) {
	return h.service.ModerateVenue(context.Background(), ModerationCommand{
		ActorID: operatorID, VenueID: venueID, Action: action, Key: key,
		Reason: reason, ReasonSet: reason != "", Note: note, NoteSet: note != nil,
	})
}

func venueStatus(t *testing.T, h *harness, id string) (status string, rejection, delisting *string) {
	t.Helper()
	if err := h.pool.QueryRow(context.Background(),
		`select verification_status, rejection_reason, delisting_reason from venues where id = $1`, id).Scan(&status, &rejection, &delisting); err != nil {
		t.Fatal(err)
	}
	return
}

func TestModerateLifecycle(t *testing.T) {
	h := newHarness(t)
	insertOperator(t, h)
	insertUser(t, h.pool, userA, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(50), name: "Life", status: "pending", submitter: ptr(userA), lat: 40, lon: 10})

	res, err := act(h, vid(50), "verify", "verify-key-1", "", nil)
	if err != nil || !res.Created || res.ActionID == nil || res.Venue.VerificationStatus != "verified" {
		t.Fatalf("verify %+v %v", res, err)
	}
	res, err = act(h, vid(50), "delist", "delist-key-1", "permanently_closed", nil)
	if err != nil || res.Venue.VerificationStatus != "delisted" || res.Venue.DelistingReason == nil || *res.Venue.DelistingReason != "permanently_closed" {
		t.Fatalf("delist %+v %v", res, err)
	}
	res, err = act(h, vid(50), "reopen", "reopen-key-1", "", nil)
	if err != nil || res.Venue.VerificationStatus != "verified" || res.Venue.DelistingReason != nil {
		t.Fatalf("reopen %+v %v", res, err)
	}
	if n := countRows(t, h, `select count(*) from venue_moderation_actions where venue_id = $1 and actor_user_id = $2`, vid(50), operatorID); n != 3 {
		t.Fatalf("audit rows %d", n)
	}
}

func TestModerateRejectAndTransitions(t *testing.T) {
	h := newHarness(t)
	insertOperator(t, h)
	seedVenue(t, h.pool, seed{id: vid(51), name: "Reject Me", status: "pending", lat: 41, lon: 10})
	res, err := act(h, vid(51), "reject", "reject-key-1", "other", ptr("Not a bar"))
	if err != nil || res.Venue.VerificationStatus != "rejected" || res.Venue.RejectionNote == nil || *res.Venue.RejectionNote != "Not a bar" {
		t.Fatalf("reject %+v %v", res, err)
	}
	var bad *InvalidTransitionError
	for _, c := range []struct{ action, reason string }{{"verify", ""}, {"delist", "duplicate"}, {"reopen", ""}} {
		if _, err := act(h, vid(51), c.action, "bad-"+c.action+"-1", c.reason, nil); !errors.As(err, &bad) || bad.Status != "rejected" {
			t.Fatalf("%s from rejected: %v", c.action, err)
		}
	}
	// A different reason on an already rejected venue updates the feedback and audits it.
	res, err = act(h, vid(51), "reject", "reject-key-2", "duplicate", nil)
	if err != nil || !res.Created || res.ActionID == nil || res.Venue.RejectionReason == nil || *res.Venue.RejectionReason != "duplicate" || res.Venue.RejectionNote != nil {
		t.Fatalf("re-reject with another reason %+v %v", res, err)
	}
	var prior, next, reason string
	var note *string
	if err := h.pool.QueryRow(context.Background(), `
		select prior_status, new_status, rejection_reason, rejection_note from venue_moderation_actions where id = $1`, *res.ActionID).
		Scan(&prior, &next, &reason, &note); err != nil || prior != "rejected" || next != "rejected" || reason != "duplicate" || note != nil {
		t.Fatalf("feedback audit %s %s %s %v %v", prior, next, reason, note, err)
	}
	if noop, err := act(h, vid(51), "reject", "reject-key-3", "duplicate", nil); err != nil || noop.Created || noop.ActionID != nil {
		t.Fatalf("identical re-reject %+v %v", noop, err)
	}
	if n := countRows(t, h, `select count(*) from venue_moderation_actions where venue_id = $1`, vid(51)); n != 2 {
		t.Fatalf("audit rows %d", n)
	}
	if _, err := act(h, vid(52), "verify", "missing-key-1", "", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing venue: %v", err)
	}
}

func TestModerateReplayNoopAndReuse(t *testing.T) {
	h := newHarness(t)
	insertOperator(t, h)
	seedVenue(t, h.pool, seed{id: vid(53), name: "Replay", status: "pending", lat: 42, lon: 10})

	first, err := act(h, vid(53), "verify", "replay-key-1", "", nil)
	if err != nil || !first.Created {
		t.Fatal(first, err)
	}
	if _, err := h.pool.Exec(context.Background(), `update venues set name = 'Renamed' where id = $1`, vid(53)); err != nil {
		t.Fatal(err)
	}
	replay, err := act(h, vid(53), "verify", "replay-key-1", "", nil)
	if err != nil || !replay.Created || *replay.ActionID != *first.ActionID || replay.Venue.Name != "Replay" {
		t.Fatalf("replay should return the stored response: %+v %v", replay, err)
	}
	if _, err := act(h, vid(53), "delist", "replay-key-1", "duplicate", nil); !errors.Is(err, idempotency.ErrReused) {
		t.Fatalf("reused key: %v", err)
	}

	before := tokensLeft(h.limiter, operatorID)
	noop, err := act(h, vid(53), "verify", "noop-key-001", "", nil)
	if err != nil || noop.Created || noop.ActionID != nil {
		t.Fatalf("no-op %+v %v", noop, err)
	}
	if after := tokensLeft(h.limiter, operatorID); after != before {
		t.Fatalf("no-op spent a write token: %d to %d", before, after)
	}
	if n := countRows(t, h, `select count(*) from venue_moderation_actions where venue_id = $1`, vid(53)); n != 1 {
		t.Fatalf("audit rows %d", n)
	}
}

func TestModerateResolvesPendingReports(t *testing.T) {
	h := newHarness(t)
	insertOperator(t, h)
	insertUser(t, h.pool, userA, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(54), name: "Reported", status: "verified", lat: 43, lon: 10})
	if _, err := h.pool.Exec(context.Background(), `
		insert into venue_reports (reporter_user_id, venue_id, reason) values ($1, $2, 'moved')`, userA, vid(54)); err != nil {
		t.Fatal(err)
	}
	if _, err := act(h, vid(54), "delist", "report-key-01", "permanently_closed", nil); err != nil {
		t.Fatal(err)
	}
	var status string
	var by *string
	if err := h.pool.QueryRow(context.Background(), `select status, resolved_by_user_id::text from venue_reports where venue_id = $1`, vid(54)).Scan(&status, &by); err != nil || status != "actioned" || by == nil || *by != operatorID {
		t.Fatalf("report %s %v %v", status, by, err)
	}
}

func TestModerateDelistFeedbackChange(t *testing.T) {
	h := newHarness(t)
	insertOperator(t, h)
	insertUser(t, h.pool, userA, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(55), name: "Closing", status: "verified", lat: 44, lon: 10})
	if _, err := act(h, vid(55), "delist", "delist-fb-key1", "permanently_closed", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `
		insert into venue_reports (reporter_user_id, venue_id, reason) values ($1, $2, 'duplicate')`, userA, vid(55)); err != nil {
		t.Fatal(err)
	}
	before := tokensLeft(h.limiter, operatorID)
	res, err := act(h, vid(55), "delist", "delist-fb-key2", "other", ptr("Moved to a new venue"))
	if err != nil || !res.Created || res.ActionID == nil || res.Venue.DelistingReason == nil || *res.Venue.DelistingReason != "other" ||
		res.Venue.DelistingNote == nil || *res.Venue.DelistingNote != "Moved to a new venue" {
		t.Fatalf("re-delist %+v %v", res, err)
	}
	if after := tokensLeft(h.limiter, operatorID); after != before-1 {
		t.Fatalf("feedback change spent %d tokens", before-after)
	}
	var prior, next, reason string
	var note *string
	if err := h.pool.QueryRow(context.Background(), `
		select prior_status, new_status, delisting_reason, delisting_note from venue_moderation_actions where id = $1`, *res.ActionID).
		Scan(&prior, &next, &reason, &note); err != nil || prior != "delisted" || next != "delisted" || reason != "other" || note == nil || *note != "Moved to a new venue" {
		t.Fatalf("feedback audit %s %s %s %v %v", prior, next, reason, note, err)
	}
	if n := countRows(t, h, `select count(*) from venue_reports where venue_id = $1 and status = 'actioned'`, vid(55)); n != 1 {
		t.Fatalf("report not actioned: %d", n)
	}
	if noop, err := act(h, vid(55), "delist", "delist-fb-key3", "other", ptr("Moved to a new venue")); err != nil || noop.Created {
		t.Fatalf("identical re-delist %+v %v", noop, err)
	}
	if n := countRows(t, h, `select count(*) from venue_moderation_actions where venue_id = $1`, vid(55)); n != 2 {
		t.Fatalf("audit rows %d", n)
	}
}

func TestModerateNeedsGenre(t *testing.T) {
	h := newHarness(t)
	insertOperator(t, h)
	seedVenue(t, h.pool, seed{id: vid(56), name: "Bare", status: "pending", lat: 45, lon: 10})
	seedVenue(t, h.pool, seed{id: vid(57), name: "Bare Delisted", status: "verified", lat: 46, lon: 10})
	if _, err := h.pool.Exec(context.Background(), `
		update venues set verification_status = 'delisted', delisting_reason = 'duplicate' where id = $1`, vid(57)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `delete from venue_genres where venue_id in ($1, $2)`, vid(56), vid(57)); err != nil {
		t.Fatal(err)
	}
	before := tokensLeft(h.limiter, operatorID)
	if _, err := act(h, vid(56), "verify", "bare-verify-1", "", nil); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("verify without genre: %v", err)
	}
	if _, err := act(h, vid(57), "reopen", "bare-reopen-1", "", nil); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("reopen without genre: %v", err)
	}
	if after := tokensLeft(h.limiter, operatorID); after != before {
		t.Fatalf("refusal spent a write token: %d to %d", before, after)
	}
	if status, _, _ := venueStatus(t, h, vid(56)); status != "pending" {
		t.Fatalf("status %s", status)
	}
	if n := countRows(t, h, `select count(*) from venue_moderation_actions where venue_id in ($1, $2)`, vid(56), vid(57)); n != 0 {
		t.Fatalf("audit rows %d", n)
	}
	// Rejecting a Venue without a genre stays possible.
	if _, err := act(h, vid(56), "reject", "bare-reject-1", "insufficient_information", nil); err != nil {
		t.Fatalf("reject without genre: %v", err)
	}
}

func TestOperatorCorrect(t *testing.T) {
	h := newHarness(t)
	insertOperator(t, h)
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(60), name: "Typo Bar", status: "verified", submitter: ptr(userA), lat: 50, lon: 10})
	seedVenue(t, h.pool, seed{id: vid(61), name: "Rejected", status: "rejected", lat: 51, lon: 10})
	if _, err := h.pool.Exec(context.Background(), `insert into venue_reports (reporter_user_id, venue_id, reason) values ($1, $2, 'incorrect_details' ) `, userB, vid(60)); err == nil {
		t.Fatal("note should be required")
	}
	if _, err := h.pool.Exec(context.Background(), `insert into venue_reports (reporter_user_id, venue_id, reason, note) values ($1, $2, 'incorrect_details', 'typo')`, userB, vid(60)); err != nil {
		t.Fatal(err)
	}
	punk := genreID(t, h.pool, "Punk")
	correct := func(id string, raw RawSubmission) (Moderated, error) {
		return h.service.CorrectVenue(context.Background(), OperatorCorrectCommand{ActorID: operatorID, VenueID: id, Fields: raw})
	}

	got, err := correct(vid(60), rawVenue("Type Bar", 50, 10, punk))
	if err != nil || got.Name != "Type Bar" || got.VerificationStatus != "verified" {
		t.Fatalf("correct %+v %v", got, err)
	}
	var prior, next string
	if err := h.pool.QueryRow(context.Background(), `select prior_status, new_status from venue_moderation_actions where venue_id = $1 and action = 'correct'`, vid(60)).Scan(&prior, &next); err != nil || prior != "verified" || next != "verified" {
		t.Fatalf("audit %s %s %v", prior, next, err)
	}
	var reportStatus string
	if err := h.pool.QueryRow(context.Background(), `select status from venue_reports where venue_id = $1`, vid(60)).Scan(&reportStatus); err != nil || reportStatus != "actioned" {
		t.Fatalf("report %s %v", reportStatus, err)
	}
	if _, err := correct(vid(60), rawVenue("Type Bar", 50, 10, punk)); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, h, `select count(*) from venue_moderation_actions where venue_id = $1 and action = 'correct'`, vid(60)); n != 1 {
		t.Fatalf("unchanged correction wrote audit: %d", n)
	}
	var bad *InvalidTransitionError
	if _, err := correct(vid(61), rawVenue("Rejected 2", 51, 10, punk)); !errors.As(err, &bad) || bad.Status != "rejected" {
		t.Fatalf("rejected correction: %v", err)
	}
	if _, err := correct(vid(99), rawVenue("None", 1, 1, punk)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	seedVenue(t, h.pool, seed{id: vid(62), name: "Type Bar", status: "verified", lat: 52, lon: 10})
	var dup *DuplicateError
	if _, err := correct(vid(62), rawVenue("Type Bar", 50, 10, punk)); !errors.As(err, &dup) {
		t.Fatalf("duplicate: %v", err)
	}
}

func TestGetModerationVenue(t *testing.T) {
	h := newHarness(t)
	insertOperator(t, h)
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(70), name: "Detail", status: "verified", submitter: ptr(userA), lat: 60, lon: 10})
	if _, err := h.pool.Exec(context.Background(), `insert into venue_reports (reporter_user_id, venue_id, reason) values ($1, $2, 'moved')`, userB, vid(70)); err != nil {
		t.Fatal(err)
	}
	// 22 corrections a minute apart, then a delist: history is the newest 20, newest first.
	if _, err := h.pool.Exec(context.Background(), `
		insert into venue_moderation_actions (venue_id, action, prior_status, new_status, actor_user_id, created_at)
		select $1, 'correct', 'verified', 'verified', $2, $3::timestamptz + (i || ' minutes')::interval
		from generate_series(1, 22) as i`, vid(70), operatorID, submitNow); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `
		insert into venue_moderation_actions (venue_id, action, prior_status, new_status, delisting_reason, delisting_note, actor_user_id, created_at)
		values ($1, 'delist', 'verified', 'delisted', 'other', 'Gone', $2, $3)`, vid(70), operatorID, submitNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := h.service.GetModerationVenue(context.Background(), vid(70))
	if err != nil || got.Submitter == nil || got.Submitter.ID != userA || len(got.Reports) != 1 || got.Reports[0].Reporter == nil || got.Reports[0].Reporter.ID != userB {
		t.Fatalf("%+v %v", got, err)
	}
	if len(got.History) != 20 {
		t.Fatalf("history %d", len(got.History))
	}
	newest := got.History[0]
	if newest.Action != "delist" || newest.PriorStatus != "verified" || newest.NewStatus != "delisted" ||
		newest.DelistingReason == nil || *newest.DelistingReason != "other" || newest.DelistingNote == nil || *newest.DelistingNote != "Gone" ||
		newest.RejectionReason != nil || newest.ActorUserID == nil || *newest.ActorUserID != operatorID ||
		!newest.CreatedAt.Equal(submitNow.Add(time.Hour)) || newest.CreatedAt.Location() != time.UTC || newest.ID == "" {
		t.Fatalf("newest %+v", newest)
	}
	for i := 1; i < len(got.History); i++ {
		if !got.History[i].CreatedAt.Before(got.History[i-1].CreatedAt) {
			t.Fatalf("history out of order at %d", i)
		}
	}
	if last := got.History[19]; last.Action != "correct" || !last.CreatedAt.Equal(submitNow.Add(4*time.Minute)) {
		t.Fatalf("oldest kept %+v", last)
	}
	seedVenue(t, h.pool, seed{id: vid(71), name: "Orphan", status: "pending", lat: 61, lon: 10})
	orphan, err := h.service.GetModerationVenue(context.Background(), vid(71))
	if err != nil || orphan.Submitter != nil || len(orphan.Reports) != 0 || orphan.History == nil || len(orphan.History) != 0 {
		t.Fatalf("%+v %v", orphan, err)
	}
	if _, err := h.service.GetModerationVenue(context.Background(), vid(72)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := h.service.GetModerationVenue(context.Background(), "nope"); err == nil {
		t.Fatal("bad id accepted")
	}
}

func TestDelistActionsReviewReports(t *testing.T) {
	h := newHarness(t)
	insertOperator(t, h)
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(90), name: "Delist Reviews", status: "verified", lat: 90, lon: 10})
	var reviewID string
	if err := h.pool.QueryRow(context.Background(), `
		insert into reviews (venue_id, user_id, rating, commentary, created_at, updated_at)
		values ($1, $2, 4, 'loud', now(), now()) returning id::text`, vid(90), userA).Scan(&reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `
		insert into review_reports (reporter_user_id, review_id, reason) values ($1, $2, 'spam')`, userB, reviewID); err != nil {
		t.Fatal(err)
	}

	if _, err := act(h, vid(90), "delist", "delist-rr-01", "permanently_closed", nil); err != nil {
		t.Fatal(err)
	}
	var status, by string
	var commentary *string
	var removedAt *time.Time
	if err := h.pool.QueryRow(context.Background(), `
		select rr.status, rr.resolved_by_user_id::text, r.commentary, r.commentary_removed_at
		from review_reports rr join reviews r on r.id = rr.review_id
		where rr.review_id = $1`, reviewID).Scan(&status, &by, &commentary, &removedAt); err != nil ||
		status != "actioned" || by != operatorID || commentary == nil || *commentary != "loud" || removedAt != nil {
		t.Fatalf("after delist status=%s by=%s commentary=%v removed=%v err=%v", status, by, commentary, removedAt, err)
	}
	if n := countRows(t, h, `select count(*) from review_moderation_actions`); n != 0 {
		t.Fatalf("review audits %d", n)
	}
	if n := countRows(t, h, `select count(*) from review_commentary_restrictions`); n != 0 {
		t.Fatalf("restrictions %d", n)
	}

	if _, err := h.pool.Exec(context.Background(), `
		insert into review_reports (reporter_user_id, review_id, reason) values ($1, $2, 'harassment')`, userB, reviewID); err != nil {
		t.Fatal(err)
	}
	if noop, err := act(h, vid(90), "delist", "delist-rr-02", "permanently_closed", nil); err != nil || noop.Created {
		t.Fatalf("identical re-delist %+v %v", noop, err)
	}
	if n := countRows(t, h, `select count(*) from review_reports where review_id = $1 and status = 'pending'`, reviewID); n != 1 {
		t.Fatalf("pending after identical re-delist %d", n)
	}

	if _, err := act(h, vid(90), "reopen", "reopen-rr-01", "", nil); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, h, `select count(*) from review_reports where review_id = $1 and status = 'actioned'`, reviewID); n != 1 {
		t.Fatalf("actioned after reopen %d", n)
	}
	if n := countRows(t, h, `select count(*) from review_reports where review_id = $1 and status = 'pending'`, reviewID); n != 1 {
		t.Fatalf("pending after reopen %d", n)
	}

	seedVenue(t, h.pool, seed{id: vid(91), name: "Pending RR", status: "pending", lat: 91, lon: 10})
	var pendingReview string
	if err := h.pool.QueryRow(context.Background(), `
		insert into reviews (venue_id, user_id, rating, commentary, created_at, updated_at)
		values ($1, $2, 3, 'x', now(), now()) returning id::text`, vid(91), userA).Scan(&pendingReview); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `
		insert into review_reports (reporter_user_id, review_id, reason) values ($1, $2, 'spam')`, userB, pendingReview); err != nil {
		t.Fatal(err)
	}
	if _, err := act(h, vid(91), "verify", "verify-rr-01", "", nil); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, h, `select count(*) from review_reports where review_id = $1 and status = 'pending'`, pendingReview); n != 1 {
		t.Fatalf("pending after verify %d", n)
	}

	seedVenue(t, h.pool, seed{id: vid(92), name: "Reject RR", status: "pending", lat: 92, lon: 10})
	var rejectReview string
	if err := h.pool.QueryRow(context.Background(), `
		insert into reviews (venue_id, user_id, rating, commentary, created_at, updated_at)
		values ($1, $2, 3, 'y', now(), now()) returning id::text`, vid(92), userA).Scan(&rejectReview); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `
		insert into review_reports (reporter_user_id, review_id, reason) values ($1, $2, 'spam')`, userB, rejectReview); err != nil {
		t.Fatal(err)
	}
	if _, err := act(h, vid(92), "reject", "reject-rr-01", "duplicate", nil); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, h, `select count(*) from review_reports where review_id = $1 and status = 'pending'`, rejectReview); n != 1 {
		t.Fatalf("pending after reject %d", n)
	}

	// Feedback-only delist actions a fresh pending review report.
	if _, err := act(h, vid(90), "delist", "delist-rr-03", "permanently_closed", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `
		insert into review_reports (reporter_user_id, review_id, reason) values ($1, $2, 'off_topic')`, userB, reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := act(h, vid(90), "delist", "delist-rr-04", "other", ptr("feedback note")); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, h, `select count(*) from review_reports where review_id = $1 and status = 'pending'`, reviewID); n != 0 {
		t.Fatalf("pending after feedback delist %d", n)
	}
}
