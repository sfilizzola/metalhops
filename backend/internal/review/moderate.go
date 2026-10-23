package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

type moderationCommandJSON struct {
	ReviewID string  `json:"review_id"`
	Action   string  `json:"action"`
	Reason   string  `json:"reason"`
	Note     *string `json:"note"`
}

// storedOperatorReview omits commentary so the idempotency body never retains it.
type storedOperatorReview struct {
	ID                  string           `json:"id"`
	VenueID             string           `json:"venue_id"`
	UserID              string           `json:"user_id"`
	Rating              int              `json:"rating"`
	CommentaryRemoved   bool             `json:"commentary_removed"`
	CommentaryRemovedAt *time.Time       `json:"commentary_removed_at"`
	Author              *profile.Profile `json:"author"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
}

type storedReviewModeration struct {
	ActionID string                `json:"action_id"`
	ReviewID string                `json:"review_id"`
	Review   *storedOperatorReview `json:"review"`
}

func (s *Service) ModerateReview(ctx context.Context, cmd ModerationCommand) (ModerationResult, error) {
	if !CanonicalID(cmd.ReviewID) {
		return ModerationResult{}, &ValidationError{Fields: map[string]string{"review_id": "must be a canonical uuid"}}
	}
	action, reason, note, err := NormalizeAction(cmd.Action, cmd.Reason, cmd.Note)
	if err != nil {
		return ModerationResult{}, err
	}
	if s.writes == nil {
		return ModerationResult{}, errWritesUnavailable
	}
	now := s.clock()
	txCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(txCtx)
	if err != nil {
		return ModerationResult{}, fmt.Errorf("review: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	command, err := json.Marshal(moderationCommandJSON{ReviewID: cmd.ReviewID, Action: action, Reason: reason, Note: note})
	if err != nil {
		return ModerationResult{}, fmt.Errorf("review: moderate: %w", err)
	}
	req := idempotency.Request{UserID: cmd.ActorID, Operation: "review_moderation_action", Key: cmd.Key, Command: command}
	stored, found, err := idempotency.Lookup(txCtx, tx, now, req)
	if err != nil {
		return ModerationResult{}, err
	}
	if found {
		result, err := moderationFromStored(stored)
		if err != nil {
			return ModerationResult{}, err
		}
		if err := tx.Commit(txCtx); err != nil {
			return ModerationResult{}, fmt.Errorf("review: commit: %w", err)
		}
		return result, nil
	}

	view, err := loadOperatorReview(txCtx, tx, cmd.ReviewID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ModerationResult{}, ErrReviewNotFound
	}
	if err != nil {
		return ModerationResult{}, err
	}
	if action == "remove_commentary" && view.Commentary == nil {
		restricted, err := restrictionExists(txCtx, tx, view.UserID, view.VenueID)
		if err != nil {
			return ModerationResult{}, err
		}
		if restricted {
			if err := tx.Commit(txCtx); err != nil {
				return ModerationResult{}, fmt.Errorf("review: commit: %w", err)
			}
			return ModerationResult{Created: false, ReviewID: cmd.ReviewID, Review: &view}, nil
		}
		return ModerationResult{}, ErrCommentaryNotPublic
	}

	var reservation *ratelimit.Reservation
	committed := false
	defer func() {
		if reservation != nil && !committed {
			reservation.Cancel()
		}
	}()
	executed, err := idempotency.Execute(txCtx, tx, now, req, func(ctx context.Context) (idempotency.Result, error) {
		next, retryAfter, ok := s.writes.Reserve(cmd.ActorID)
		if !ok {
			return idempotency.Result{}, &RateLimitError{RetryAfter: retryAfter}
		}
		reservation = next

		var locked OperatorReview
		err := tx.QueryRow(ctx, `
			select r.id::text, r.venue_id::text, r.user_id::text, r.rating, r.commentary,
				r.commentary_removed_at is not null, r.commentary_removed_at, r.created_at, r.updated_at
			from reviews r where r.id = $1::uuid for update`, cmd.ReviewID).Scan(
			&locked.ID, &locked.VenueID, &locked.UserID, &locked.Rating, &locked.Commentary,
			&locked.CommentaryRemoved, &locked.CommentaryRemovedAt, &locked.CreatedAt, &locked.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return idempotency.Result{}, ErrReviewNotFound
		}
		if err != nil {
			return idempotency.Result{}, fmt.Errorf("review: moderate: %w", err)
		}

		auditAction := "commentary_removed"
		if action == "delete_review" {
			auditAction = "review_deleted"
		}
		var actionID string
		err = tx.QueryRow(ctx, `
			insert into review_moderation_actions (
				review_id, reviewer_user_id, venue_id, action, reason, note, operator_user_id, created_at
			) values ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7::uuid, $8)
			returning id::text`,
			locked.ID, locked.UserID, locked.VenueID, auditAction, reason, note, cmd.ActorID, now).Scan(&actionID)
		if err != nil {
			return idempotency.Result{}, fmt.Errorf("review: audit: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			insert into review_commentary_restrictions (user_id, venue_id, source_action_id, created_at)
			values ($1::uuid, $2::uuid, $3::uuid, $4)
			on conflict (user_id, venue_id) do nothing`, locked.UserID, locked.VenueID, actionID, now); err != nil {
			return idempotency.Result{}, fmt.Errorf("review: restriction: %w", err)
		}

		var bodyReview *storedOperatorReview
		if action == "remove_commentary" {
			if _, err := tx.Exec(ctx, `
				update reviews
				set commentary = null, commentary_removed_at = $2, updated_at = $2
				where id = $1::uuid`, locked.ID, now); err != nil {
				return idempotency.Result{}, fmt.Errorf("review: remove: %w", err)
			}
			if _, err := tx.Exec(ctx, `
				update review_reports
				set status = 'actioned', resolved_at = $2, resolved_by_user_id = $3::uuid
				where review_id = $1::uuid and status = 'pending'`, locked.ID, now, cmd.ActorID); err != nil {
				return idempotency.Result{}, fmt.Errorf("review: reports: %w", err)
			}
			live, err := loadOperatorReview(ctx, tx, locked.ID)
			if err != nil {
				return idempotency.Result{}, err
			}
			bodyReview = storeOperatorReview(live)
		} else {
			if _, err := tx.Exec(ctx, `delete from reviews where id = $1::uuid`, locked.ID); err != nil {
				return idempotency.Result{}, fmt.Errorf("review: delete: %w", err)
			}
		}

		body, err := json.Marshal(storedReviewModeration{ActionID: actionID, ReviewID: locked.ID, Review: bodyReview})
		if err != nil {
			return idempotency.Result{}, fmt.Errorf("review: moderate: %w", err)
		}
		return idempotency.Result{Status: 201, Body: body}, nil
	})
	if err != nil {
		return ModerationResult{}, err
	}
	result, err := moderationFromStored(executed)
	if err != nil {
		return ModerationResult{}, err
	}
	if err := tx.Commit(txCtx); err != nil {
		return ModerationResult{}, fmt.Errorf("review: commit: %w", err)
	}
	if reservation != nil {
		reservation.Commit()
		committed = true
	}
	return result, nil
}

func moderationFromStored(r idempotency.Result) (ModerationResult, error) {
	var body storedReviewModeration
	if err := json.Unmarshal(r.Body, &body); err != nil {
		return ModerationResult{}, fmt.Errorf("review: moderate: %w", err)
	}
	id := body.ActionID
	out := ModerationResult{Created: true, ActionID: &id, ReviewID: body.ReviewID}
	if body.Review != nil {
		live := OperatorReview{
			ID: body.Review.ID, VenueID: body.Review.VenueID, UserID: body.Review.UserID,
			Rating: body.Review.Rating, CommentaryRemoved: body.Review.CommentaryRemoved,
			CommentaryRemovedAt: body.Review.CommentaryRemovedAt, Author: body.Review.Author,
			CreatedAt: body.Review.CreatedAt, UpdatedAt: body.Review.UpdatedAt,
		}
		out.Review = &live
	}
	return out, nil
}

func storeOperatorReview(r OperatorReview) *storedOperatorReview {
	return &storedOperatorReview{
		ID: r.ID, VenueID: r.VenueID, UserID: r.UserID, Rating: r.Rating,
		CommentaryRemoved: r.CommentaryRemoved, CommentaryRemovedAt: r.CommentaryRemovedAt,
		Author: r.Author, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func restrictionExists(ctx context.Context, q queryRower, userID, venueID string) (bool, error) {
	var exists bool
	if err := q.QueryRow(ctx, `
		select exists (
			select 1 from review_commentary_restrictions
			where user_id = $1::uuid and venue_id = $2::uuid
		)`, userID, venueID).Scan(&exists); err != nil {
		return false, fmt.Errorf("review: restriction: %w", err)
	}
	return exists, nil
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadOperatorReview(ctx context.Context, q queryRower, reviewID string) (OperatorReview, error) {
	var r OperatorReview
	var author nullableProfile
	dest := []any{
		&r.ID, &r.VenueID, &r.UserID, &r.Rating, &r.Commentary,
		&r.CommentaryRemoved, &r.CommentaryRemovedAt, &r.CreatedAt, &r.UpdatedAt,
	}
	dest = append(dest, author.dest()...)
	err := q.QueryRow(ctx, `
		select r.id::text, r.venue_id::text, r.user_id::text, r.rating, r.commentary,
			r.commentary_removed_at is not null, r.commentary_removed_at, r.created_at, r.updated_at,
			`+profileColumns("p")+`
		from reviews r
		left join profiles p on p.id = r.user_id
		where r.id = $1::uuid`, reviewID).Scan(dest...)
	if err != nil {
		return OperatorReview{}, err
	}
	r.CreatedAt, r.UpdatedAt = r.CreatedAt.UTC(), r.UpdatedAt.UTC()
	if r.CommentaryRemovedAt != nil {
		at := r.CommentaryRemovedAt.UTC()
		r.CommentaryRemovedAt = &at
	}
	r.Author = author.profile()
	return r, nil
}

func profileColumns(alias string) string {
	return fmt.Sprintf(`%[1]s.id::text, %[1]s.username, %[1]s.display_name, %[1]s.status,
		%[1]s.moderation_reason, %[1]s.moderation_note, %[1]s.suspended_until, %[1]s.created_at, %[1]s.updated_at`, alias)
}

type nullableProfile struct {
	id, username, displayName, status, reason, note *string
	until, createdAt, updatedAt                     *time.Time
}

func (n *nullableProfile) dest() []any {
	return []any{&n.id, &n.username, &n.displayName, &n.status, &n.reason, &n.note, &n.until, &n.createdAt, &n.updatedAt}
}

func (n nullableProfile) profile() *profile.Profile {
	if n.id == nil {
		return nil
	}
	p := profile.Profile{
		ID: *n.id, Username: *n.username, DisplayName: *n.displayName, Status: *n.status,
		ModerationReason: n.reason, ModerationNote: n.note, SuspendedUntil: n.until,
		CreatedAt: n.createdAt.UTC(), UpdatedAt: n.updatedAt.UTC(),
	}
	if p.SuspendedUntil != nil {
		until := p.SuspendedUntil.UTC()
		p.SuspendedUntil = &until
	}
	return &p
}
