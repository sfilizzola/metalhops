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
