package review

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

var t0 = time.Date(2020, 1, 15, 12, 0, 0, 0, time.UTC)

type writeHarness struct {
	pool    *pgxpool.Pool
	now     time.Time
	limiter *ratelimit.UserLimiter
	service *Service
}

func newWriteHarness(t *testing.T) *writeHarness {
	t.Helper()
	h := &writeHarness{pool: testPool(t), now: t0}
	clock := func() time.Time { return h.now }
	h.limiter = ratelimit.NewUserLimiter(clock)
	h.service = New(h.pool, profile.New(h.pool, h.limiter, nil, clock), h.limiter, clock)
	return h
}

func (h *writeHarness) save(userID, venueID, rating string, commentary json.RawMessage) (SaveResult, error) {
	return h.service.Save(context.Background(), SaveCommand{
		UserID: userID, VenueID: venueID, Rating: json.RawMessage(rating), Commentary: commentary,
	})
}

func (h *writeHarness) count(t *testing.T) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(), `select count(*) from reviews`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func tokensLeft(limiter *ratelimit.UserLimiter, userID string) int {
	held := make([]*ratelimit.Reservation, 0, 60)
	for len(held) < 60 {
		reservation, _, ok := limiter.Reserve(userID)
		if !ok {
			break
		}
		held = append(held, reservation)
	}
	for _, reservation := range held {
		reservation.Cancel()
	}
	return len(held)
}

func TestSaveCreateReplaceNoop(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)

	got, err := h.save(userA, verifiedVenue, "4", json.RawMessage(`"  loud "`))
	if err != nil || !got.Created || got.Review.Commentary == nil || *got.Review.Commentary != "loud" ||
		got.Review.Rating != 4 || got.Review.UserID != userA || got.Review.DisplayName != "Tester" ||
		!got.Review.CreatedAt.Equal(t0) || !got.Review.UpdatedAt.Equal(t0) {
		t.Fatalf("create %+v %v", got, err)
	}
	if left := tokensLeft(h.limiter, userA); left != 59 {
		t.Fatalf("tokens after create %d", left)
	}

	h.now = h.now.Add(time.Millisecond)
	got, err = h.save(userA, verifiedVenue, "4", json.RawMessage(`"loud"`))
	if err != nil || got.Created || !got.Review.UpdatedAt.Equal(t0) {
		t.Fatalf("noop %+v %v", got, err)
	}
	if left := tokensLeft(h.limiter, userA); left != 59 {
		t.Fatalf("tokens after noop %d", left)
	}

	h.now = h.now.Add(time.Millisecond)
	got, err = h.save(userA, verifiedVenue, "2", nil)
	if err != nil || got.Created || got.Review.Rating != 2 || got.Review.Commentary != nil ||
		!got.Review.UpdatedAt.Equal(t0.Add(2*time.Millisecond)) || !got.Review.CreatedAt.Equal(t0) {
		t.Fatalf("replace %+v %v", got, err)
	}
	if left := tokensLeft(h.limiter, userA); left != 58 {
		t.Fatalf("tokens after replace %d", left)
	}
	if n := h.count(t); n != 1 {
		t.Fatalf("rows %d", n)
	}
}

func TestSaveNormalizedCommentaryNoop(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)

	got, err := h.save(userA, verifiedVenue, "3", json.RawMessage(`"e\u0301"`))
	if err != nil || got.Review.Commentary == nil || *got.Review.Commentary != "é" {
		t.Fatalf("decomposed %+v %v", got, err)
	}
	if left := tokensLeft(h.limiter, userA); left != 59 {
		t.Fatalf("tokens after create %d", left)
	}
	h.now = h.now.Add(time.Millisecond)
	got, err = h.save(userA, verifiedVenue, "3", json.RawMessage(`"é"`))
	if err != nil || got.Created || !got.Review.UpdatedAt.Equal(t0) {
		t.Fatalf("composed %+v %v", got, err)
	}
	if left := tokensLeft(h.limiter, userA); left != 59 {
		t.Fatalf("tokens after noop %d", left)
	}
}

func TestSaveValidation(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)

	var validation *ValidationError
	if _, err := h.save(userA, verifiedVenue, "6", nil); !errors.As(err, &validation) || validation.Fields["rating"] == "" {
		t.Fatalf("rating err %v", err)
	}
	if _, err := h.save(userA, verifiedVenue, "4", json.RawMessage(`"a\u0000b"`)); !errors.As(err, &validation) ||
		validation.Fields["commentary"] != "must be text" {
		t.Fatalf("null byte err %v", err)
	}
	if n := h.count(t); n != 0 {
		t.Fatalf("rows %d", n)
	}
	if left := tokensLeft(h.limiter, userA); left != 60 {
		t.Fatalf("tokens %d", left)
	}
}

func TestSaveOutcomes(t *testing.T) {
	h := newWriteHarness(t)
	for id, status := range map[string]string{
		verifiedVenue: "verified", pendingVenue: "pending", rejectedVenue: "rejected",
		delistedVenue: "delisted", bareVenue: "bare",
	} {
		insertVenue(t, h.pool, id, status)
	}
	future := t0.Add(time.Hour)
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "", nil)
	insertUser(t, h.pool, userC, "rename_required", nil)
	suspended := "44444444-4444-4444-8444-444444444444"
	insertUser(t, h.pool, suspended, "suspended", &future)

	if _, err := h.save(userA, pendingVenue, "4", nil); !errors.Is(err, ErrVenueNotVerified) {
		t.Fatalf("pending err %v", err)
	}
	if n := h.count(t); n != 0 {
		t.Fatalf("pending rows %d", n)
	}
	if left := tokensLeft(h.limiter, userA); left != 60 {
		t.Fatalf("pending tokens %d", left)
	}
	for _, id := range []string{rejectedVenue, delistedVenue, bareVenue, unknownVenue} {
		if _, err := h.save(userA, id, "4", nil); !errors.Is(err, ErrVenueNotFound) {
			t.Fatalf("hidden %s err %v", id, err)
		}
	}
	if _, err := h.save(userB, verifiedVenue, "4", nil); !errors.Is(err, profile.ErrProfileRequired) {
		t.Fatalf("no profile err %v", err)
	}
	if _, err := h.save(userC, verifiedVenue, "4", nil); !errors.Is(err, profile.ErrRenameRequired) {
		t.Fatalf("rename err %v", err)
	}
	var suspendedErr *profile.SuspendedError
	if _, err := h.save(suspended, verifiedVenue, "4", nil); !errors.As(err, &suspendedErr) ||
		suspendedErr.Until == nil || !suspendedErr.Until.Equal(future) {
		t.Fatalf("suspended err %v", err)
	}
	if _, err := h.save(suspended, delistedVenue, "4", nil); !errors.Is(err, ErrVenueNotFound) {
		t.Fatalf("suspended on delisted err %v", err)
	}
	if n := h.count(t); n != 0 {
		t.Fatalf("rows %d", n)
	}

	if _, err := h.pool.Exec(context.Background(), `update venues set submitted_by = $1 where id = $2`, userA, verifiedVenue); err != nil {
		t.Fatal(err)
	}
	if got, err := h.save(userA, verifiedVenue, "5", nil); err != nil || !got.Created {
		t.Fatalf("submitter %+v %v", got, err)
	}
}

func TestSaveAfterExpiredSuspension(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	expired := t0.Add(-time.Hour)
	insertUser(t, h.pool, userA, "suspended", &expired)

	if got, err := h.save(userA, verifiedVenue, "4", nil); err != nil || !got.Created {
		t.Fatalf("save %+v %v", got, err)
	}
}

func TestSaveRateLimited(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	for i := 0; i < 60; i++ {
		if _, _, ok := h.limiter.Reserve(userA); !ok {
			t.Fatalf("drain %d", i)
		}
	}

	var limited *RateLimitError
	if _, err := h.save(userA, verifiedVenue, "4", nil); !errors.As(err, &limited) || limited.RetryAfter < 1 {
		t.Fatalf("err %v", err)
	}
	if n := h.count(t); n != 0 {
		t.Fatalf("rows %d", n)
	}
}

func TestConcurrentFirstSave(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)

	var start, done sync.WaitGroup
	start.Add(1)
	results := make([]SaveResult, 2)
	errs := make([]error, 2)
	for i, rating := range []string{"3", "5"} {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			results[i], errs[i] = h.save(userA, verifiedVenue, rating, nil)
		}()
	}
	start.Done()
	done.Wait()

	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("errs %v %v", errs[0], errs[1])
	}
	if results[0].Created == results[1].Created {
		t.Fatalf("created %v %v", results[0].Created, results[1].Created)
	}
	if n := h.count(t); n != 1 {
		t.Fatalf("rows %d", n)
	}
}

func TestDelete(t *testing.T) {
	ctx := context.Background()
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertVenue(t, h.pool, pendingVenue, "pending")
	insertUser(t, h.pool, userA, "active", nil)
	insertReview(t, h.pool, verifiedVenue, userA, 4, nil, t0)

	if err := h.service.Delete(ctx, userA, verifiedVenue); err != nil {
		t.Fatal(err)
	}
	if n := h.count(t); n != 0 {
		t.Fatalf("rows %d", n)
	}
	if left := tokensLeft(h.limiter, userA); left != 59 {
		t.Fatalf("tokens after delete %d", left)
	}
	if err := h.service.Delete(ctx, userA, verifiedVenue); !errors.Is(err, ErrReviewNotFound) {
		t.Fatalf("second delete err %v", err)
	}
	if left := tokensLeft(h.limiter, userA); left != 59 {
		t.Fatalf("tokens after second delete %d", left)
	}
	if err := h.service.Delete(ctx, userA, pendingVenue); !errors.Is(err, ErrVenueNotVerified) {
		t.Fatalf("pending err %v", err)
	}
	if err := h.service.Delete(ctx, userA, unknownVenue); !errors.Is(err, ErrVenueNotFound) {
		t.Fatalf("unknown err %v", err)
	}
}

func TestDelistedVenueKeepsReview(t *testing.T) {
	ctx := context.Background()
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	if _, err := h.save(userA, verifiedVenue, "4", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(ctx, `
		update venues set verification_status = 'delisted', delisting_reason = 'duplicate' where id = $1`, verifiedVenue); err != nil {
		t.Fatal(err)
	}

	if _, err := h.service.List(ctx, ListQuery{VenueID: verifiedVenue, Limit: 20}); !errors.Is(err, ErrVenueNotFound) {
		t.Fatalf("list err %v", err)
	}
	if _, err := h.service.GetOwn(ctx, userA, verifiedVenue); !errors.Is(err, ErrReviewNotFound) {
		t.Fatalf("own err %v", err)
	}
	if _, err := h.save(userA, verifiedVenue, "1", nil); !errors.Is(err, ErrVenueNotFound) {
		t.Fatalf("save err %v", err)
	}
	if err := h.service.Delete(ctx, userA, verifiedVenue); !errors.Is(err, ErrVenueNotFound) {
		t.Fatalf("delete err %v", err)
	}
	var rating int
	if err := h.pool.QueryRow(ctx, `select rating from reviews where venue_id = $1 and user_id = $2`, verifiedVenue, userA).Scan(&rating); err != nil || rating != 4 {
		t.Fatalf("stored rating %d %v", rating, err)
	}

	if _, err := h.pool.Exec(ctx, `
		update venues set verification_status = 'verified', delisting_reason = null, delisting_note = null where id = $1`, verifiedVenue); err != nil {
		t.Fatal(err)
	}
	if own, err := h.service.GetOwn(ctx, userA, verifiedVenue); err != nil || own.Rating != 4 {
		t.Fatalf("reopened own %+v %v", own, err)
	}
}

func TestUserDeletionRemovesReview(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	if _, err := h.save(userA, verifiedVenue, "4", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `delete from auth.users where id = $1`, userA); err != nil {
		t.Fatal(err)
	}
	if n := h.count(t); n != 0 {
		t.Fatalf("rows %d", n)
	}
}

func seedRestriction(t *testing.T, h *writeHarness, reviewID, userID, venueID, reason string, note *string) time.Time {
	t.Helper()
	var exists bool
	if err := h.pool.QueryRow(context.Background(), `select exists(select 1 from auth.users where id = $1)`, userC).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		insertUser(t, h.pool, userC, "", nil)
	}
	started := t0.Add(time.Hour)
	actionID := insertModerationAction(t, h.pool, reviewID, userID, venueID, userC, "commentary_removed", reason, note, t0)
	if _, err := h.pool.Exec(context.Background(), `
		insert into review_commentary_restrictions (user_id, venue_id, source_action_id, created_at)
		values ($1, $2, $3, $4)`, userID, venueID, actionID, started); err != nil {
		t.Fatal(err)
	}
	return started
}

func TestLockedSameCommentaryIsConflict(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	reviewID := insertReview(t, h.pool, verifiedVenue, userA, 4, ptr("loud"), t0)
	note := "keep it down"
	started := seedRestriction(t, h, reviewID, userA, verifiedVenue, "spam", &note)

	_, err := h.save(userA, verifiedVenue, "4", json.RawMessage(`"loud"`))
	var locked *CommentaryRestrictedError
	if !errors.As(err, &locked) || !errors.Is(err, ErrCommentaryLocked) ||
		locked.Restriction.Reason != "spam" || locked.Restriction.Note == nil || *locked.Restriction.Note != note ||
		!locked.Restriction.CreatedAt.Equal(started) {
		t.Fatalf("locked %+v", err)
	}
	var commentary string
	if err := h.pool.QueryRow(context.Background(), `
		select commentary from reviews where id = $1`, reviewID).Scan(&commentary); err != nil || commentary != "loud" {
		t.Fatalf("stored %q %v", commentary, err)
	}
	if left := tokensLeft(h.limiter, userA); left != 60 {
		t.Fatalf("tokens %d", left)
	}
}

func TestLockedClearAndRecreate(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	reviewID := insertReview(t, h.pool, verifiedVenue, userA, 4, ptr("loud"), t0)
	seedRestriction(t, h, reviewID, userA, verifiedVenue, "spam", nil)

	got, err := h.save(userA, verifiedVenue, "3", nil)
	if err != nil || got.Review.Rating != 3 || got.Review.Commentary != nil {
		t.Fatalf("clear %+v %v", got, err)
	}
	var removedAt *time.Time
	var restrictions int
	if err := h.pool.QueryRow(context.Background(), `
		select commentary_removed_at from reviews where id = $1`, reviewID).Scan(&removedAt); err != nil || removedAt != nil {
		t.Fatalf("removed_at %v %v", removedAt, err)
	}
	if err := h.pool.QueryRow(context.Background(), `
		select count(*) from review_commentary_restrictions where user_id = $1`, userA).Scan(&restrictions); err != nil || restrictions != 1 {
		t.Fatalf("restrictions %d %v", restrictions, err)
	}

	ctx := context.Background()
	if err := h.service.Delete(ctx, userA, verifiedVenue); err != nil {
		t.Fatal(err)
	}
	got, err = h.save(userA, verifiedVenue, "2", nil)
	if err != nil || got.Review.CommentaryRemoved || got.Review.Rating != 2 {
		t.Fatalf("recreate %+v %v", got, err)
	}
	if _, err := h.save(userA, verifiedVenue, "2", json.RawMessage(`"again"`)); !errors.Is(err, ErrCommentaryLocked) {
		t.Fatalf("again err %v", err)
	}
}

func TestSaveAfterLiftClearsMarker(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userC, "", nil)
	reviewID := insertReview(t, h.pool, verifiedVenue, userA, 4, ptr("loud"), t0)

	if _, err := moderate(h, userC, reviewID, "mod-remove", "remove_commentary", "spam", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := moderate(h, userC, reviewID, "mod-lift", "lift_commentary_restriction", "", nil); err != nil {
		t.Fatal(err)
	}

	h.now = t0.Add(time.Hour)
	got, err := h.save(userA, verifiedVenue, "4", json.RawMessage(`"back"`))
	if err != nil || got.Review.Commentary == nil || *got.Review.Commentary != "back" ||
		got.Review.CommentaryRemoved || !got.Review.UpdatedAt.Equal(h.now) || !got.Review.CreatedAt.Equal(t0) {
		t.Fatalf("save after lift %+v %v", got, err)
	}
	var removedAt *time.Time
	if err := h.pool.QueryRow(context.Background(), `
		select commentary_removed_at from reviews where id = $1`, reviewID).Scan(&removedAt); err != nil || removedAt != nil {
		t.Fatalf("removed_at %v %v", removedAt, err)
	}
	var audits int
	if err := h.pool.QueryRow(context.Background(), `
		select count(*) from review_moderation_actions
		where review_id = $1 and action in ('commentary_removed', 'commentary_restriction_lifted')`,
		reviewID).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("audits %d %v", audits, err)
	}
}

func TestRatingOnlySaveKeepsMarker(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userC, "", nil)
	reviewID := insertReview(t, h.pool, verifiedVenue, userA, 4, ptr("loud"), t0)

	if _, err := moderate(h, userC, reviewID, "mod-remove", "remove_commentary", "spam", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := moderate(h, userC, reviewID, "mod-lift", "lift_commentary_restriction", "", nil); err != nil {
		t.Fatal(err)
	}

	h.now = t0.Add(time.Hour)
	got, err := h.save(userA, verifiedVenue, "2", nil)
	if err != nil || got.Review.Rating != 2 || got.Review.Commentary != nil {
		t.Fatalf("rating-only save %+v %v", got, err)
	}
	var removedAt *time.Time
	if err := h.pool.QueryRow(context.Background(), `
		select commentary_removed_at from reviews where id = $1`, reviewID).Scan(&removedAt); err != nil || removedAt == nil {
		t.Fatalf("removed_at %v %v", removedAt, err)
	}

	before := tokensLeft(h.limiter, userA)
	repeat, err := h.save(userA, verifiedVenue, "2", nil)
	if err != nil || repeat.Created || !repeat.Review.UpdatedAt.Equal(h.now) {
		t.Fatalf("repeat %+v %v", repeat, err)
	}
	if left := tokensLeft(h.limiter, userA); left != before {
		t.Fatalf("tokens %d want %d", left, before)
	}
}

func TestCommentaryRestrictionLifecycle(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userC, "", nil)

	if _, err := h.save(userA, verifiedVenue, "4", json.RawMessage(`"loud"`)); err != nil {
		t.Fatal(err)
	}
	own, err := h.service.GetOwn(context.Background(), userA, verifiedVenue)
	if err != nil {
		t.Fatal(err)
	}
	reviewID := own.ID

	if _, err := moderate(h, userC, reviewID, "lc-remove", "remove_commentary", "spam", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := h.save(userA, verifiedVenue, "4", json.RawMessage(`"again"`)); !errors.Is(err, ErrCommentaryLocked) {
		t.Fatalf("locked save err %v", err)
	}
	h.now = h.now.Add(time.Minute)
	lift, err := moderate(h, userC, reviewID, "lc-lift-key", "lift_commentary_restriction", "", nil)
	if err != nil || !lift.Created {
		t.Fatalf("lift %+v %v", lift, err)
	}
	h.now = h.now.Add(time.Minute)
	if _, err := h.save(userA, verifiedVenue, "4", json.RawMessage(`"again"`)); err != nil {
		t.Fatalf("save after lift err %v", err)
	}

	var removedAt *time.Time
	var restrictions int
	if err := h.pool.QueryRow(context.Background(), `
		select commentary_removed_at from reviews where id = $1`, reviewID).Scan(&removedAt); err != nil || removedAt != nil {
		t.Fatalf("removed_at %v %v", removedAt, err)
	}
	if err := h.pool.QueryRow(context.Background(), `
		select count(*) from review_commentary_restrictions where user_id = $1`, userA).Scan(&restrictions); err != nil || restrictions != 0 {
		t.Fatalf("restrictions %d %v", restrictions, err)
	}
	rows, err := h.pool.Query(context.Background(), `
		select action from review_moderation_actions where review_id = $1 order by created_at`, reviewID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, a)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(actions) != 2 || actions[0] != "commentary_removed" || actions[1] != "commentary_restriction_lifted" {
		t.Fatalf("actions %v", actions)
	}
}

func TestAuthorChangeResolvesReports(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	reviewID := insertReview(t, h.pool, verifiedVenue, userA, 4, ptr("old"), t0)
	insertPendingReport(t, h.pool, userB, reviewID, "spam", nil)

	if _, err := h.save(userA, verifiedVenue, "4", nil); err != nil {
		t.Fatal(err)
	}
	assertReportStatus(t, h.pool, reviewID, "author_resolved", userA)
	var restrictions int
	if err := h.pool.QueryRow(context.Background(), `
		select count(*) from review_commentary_restrictions`).Scan(&restrictions); err != nil || restrictions != 0 {
		t.Fatalf("restrictions %d %v", restrictions, err)
	}

	if _, err := h.save(userA, verifiedVenue, "4", json.RawMessage(`"old"`)); err != nil {
		t.Fatal(err)
	}
	insertPendingReport(t, h.pool, userB, reviewID, "harassment", nil)
	if _, err := h.save(userA, verifiedVenue, "4", json.RawMessage(`"new"`)); err != nil {
		t.Fatal(err)
	}
	assertReportStatus(t, h.pool, reviewID, "author_resolved", userA)
	insertPendingReport(t, h.pool, userB, reviewID, "spam", nil)
	var pending int
	if err := h.pool.QueryRow(context.Background(), `
		select count(*) from review_reports where review_id = $1 and status = 'pending'`, reviewID).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("pending %d %v", pending, err)
	}
}

func TestAuthorChangeDoesNotReopenActioned(t *testing.T) {
	h := newWriteHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	insertUser(t, h.pool, userC, "", nil)
	reviewID := insertReview(t, h.pool, verifiedVenue, userA, 4, ptr("old"), t0)
	if _, err := h.pool.Exec(context.Background(), `
		insert into review_reports (reporter_user_id, review_id, reason, status, resolved_at, resolved_by_user_id)
		values ($1, $2, 'spam', 'actioned', $3, $4)`, userB, reviewID, t0, userC); err != nil {
		t.Fatal(err)
	}
	insertPendingReport(t, h.pool, userB, reviewID, "harassment", nil)

	if _, err := h.save(userA, verifiedVenue, "4", json.RawMessage(`"new"`)); err != nil {
		t.Fatal(err)
	}
	var actioned, authorResolved int
	if err := h.pool.QueryRow(context.Background(), `
		select
			count(*) filter (where status = 'actioned'),
			count(*) filter (where status = 'author_resolved')
		from review_reports where review_id = $1`, reviewID).Scan(&actioned, &authorResolved); err != nil ||
		actioned != 1 || authorResolved != 1 {
		t.Fatalf("statuses actioned=%d author_resolved=%d %v", actioned, authorResolved, err)
	}
}

func insertPendingReport(t *testing.T, pool *pgxpool.Pool, reporterID, reviewID, reason string, note *string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		insert into review_reports (reporter_user_id, review_id, reason, note)
		values ($1, $2, $3, $4)`, reporterID, reviewID, reason, note); err != nil {
		t.Fatal(err)
	}
}

func assertReportStatus(t *testing.T, pool *pgxpool.Pool, reviewID, status, resolvedBy string) {
	t.Helper()
	var gotStatus, gotBy string
	if err := pool.QueryRow(context.Background(), `
		select status, resolved_by_user_id::text from review_reports
		where review_id = $1
		order by created_at desc limit 1`, reviewID).Scan(&gotStatus, &gotBy); err != nil ||
		gotStatus != status || gotBy != resolvedBy {
		t.Fatalf("report %s by %s (want %s by %s): %v", gotStatus, gotBy, status, resolvedBy, err)
	}
}
