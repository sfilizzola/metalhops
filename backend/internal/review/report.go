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

type reviewReportCommand struct {
	ReviewID string  `json:"review_id"`
	VenueID  string  `json:"venue_id"`
	Reason   string  `json:"reason"`
	Note     *string `json:"note"`
}

type reviewReportBody struct {
	ID        string    `json:"id"`
	ReviewID  string    `json:"review_id"`
	VenueID   string    `json:"venue_id"`
	Reason    string    `json:"reason"`
	Note      *string   `json:"note"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) ReportReview(ctx context.Context, cmd ReportCommand) (Report, error) {
	if !CanonicalID(cmd.VenueID) {
		return Report{}, &ValidationError{Fields: map[string]string{"venue_id": "must be a canonical uuid"}}
	}
	if !CanonicalID(cmd.ReviewID) {
		return Report{}, &ValidationError{Fields: map[string]string{"review_id": "must be a canonical uuid"}}
	}
	reason, note, err := NormalizeReport(cmd.Reason, cmd.Note)
	if err != nil {
		return Report{}, err
	}
	if s.profiles == nil || s.writes == nil {
		return Report{}, errWritesUnavailable
	}
	txCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.profiles.NormalizeExpiry(txCtx, cmd.ReporterID); err != nil {
		return Report{}, err
	}
	tx, err := s.pool.Begin(txCtx)
	if err != nil {
		return Report{}, fmt.Errorf("review: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := profile.RequireContributor(txCtx, tx, cmd.ReporterID); err != nil {
		return Report{}, err
	}
	var status string
	err = tx.QueryRow(txCtx, `
		select v.verification_status from venues v
		where v.id = $1::uuid
		  and v.verification_status in ('pending', 'verified')
		  and exists (select 1 from venue_genres g where g.venue_id = v.id)
		for share of v`, cmd.VenueID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Report{}, ErrVenueNotFound
	}
	if err != nil {
		return Report{}, fmt.Errorf("review: report: %w", err)
	}
	if status != "verified" {
		return Report{}, ErrVenueNotVerified
	}
	var authorID string
	var commentary *string
	err = tx.QueryRow(txCtx, `
		select user_id::text, commentary from reviews
		where id = $1::uuid and venue_id = $2::uuid`, cmd.ReviewID, cmd.VenueID).Scan(&authorID, &commentary)
	if errors.Is(err, pgx.ErrNoRows) {
		return Report{}, ErrReviewNotFound
	}
	if err != nil {
		return Report{}, fmt.Errorf("review: report: %w", err)
	}
	if authorID == cmd.ReporterID {
		return Report{}, ErrSelfReport
	}
	if commentary == nil {
		return Report{}, ErrCommentaryNotPublic
	}
	command, err := json.Marshal(reviewReportCommand{
		ReviewID: cmd.ReviewID, VenueID: cmd.VenueID, Reason: reason, Note: note,
	})
	if err != nil {
		return Report{}, fmt.Errorf("review: report: %w", err)
	}
	now := s.clock()
	var reservation *ratelimit.Reservation
	committed := false
	defer func() {
		if reservation != nil && !committed {
			reservation.Cancel()
		}
	}()
	result, err := idempotency.Execute(txCtx, tx, now, idempotency.Request{
		UserID: cmd.ReporterID, Operation: "review_report", Key: cmd.Key, Command: command,
	}, func(ctx context.Context) (idempotency.Result, error) {
		var pending bool
		if err := tx.QueryRow(ctx, `
			select exists (
				select 1 from review_reports
				where reporter_user_id = $1::uuid and review_id = $2::uuid and status = 'pending'
			)`, cmd.ReporterID, cmd.ReviewID).Scan(&pending); err != nil {
			return idempotency.Result{}, fmt.Errorf("review: report: %w", err)
		}
		if pending {
			return idempotency.Result{}, ErrReportPending
		}
		if err := profile.CheckReportQuota(ctx, tx, cmd.ReporterID, now); err != nil {
			return idempotency.Result{}, err
		}
		next, retryAfter, ok := s.writes.Reserve(cmd.ReporterID)
		if !ok {
			return idempotency.Result{}, &RateLimitError{RetryAfter: retryAfter}
		}
		reservation = next
		var stored reviewReportBody
		err := tx.QueryRow(ctx, `
			insert into review_reports (reporter_user_id, review_id, reason, note, created_at)
			values ($1::uuid, $2::uuid, $3, $4, $5)
			on conflict (reporter_user_id, review_id) where status = 'pending' do nothing
			returning id::text, review_id::text, reason, note, status, created_at`,
			cmd.ReporterID, cmd.ReviewID, reason, note, now).Scan(
			&stored.ID, &stored.ReviewID, &stored.Reason, &stored.Note, &stored.Status, &stored.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return idempotency.Result{}, ErrReportPending
		}
		if err != nil {
			return idempotency.Result{}, fmt.Errorf("review: report: %w", err)
		}
		stored.VenueID = cmd.VenueID
		stored.CreatedAt = stored.CreatedAt.UTC()
		if err := profile.RecordReportEvent(ctx, tx, cmd.ReporterID, now); err != nil {
			return idempotency.Result{}, err
		}
		body, err := json.Marshal(stored)
		if err != nil {
			return idempotency.Result{}, fmt.Errorf("review: report: %w", err)
		}
		return idempotency.Result{Status: 201, Body: body}, nil
	})
	if err != nil {
		return Report{}, err
	}
	var stored reviewReportBody
	if err := json.Unmarshal(result.Body, &stored); err != nil {
		return Report{}, fmt.Errorf("review: report: %w", err)
	}
	if err := tx.Commit(txCtx); err != nil {
		return Report{}, fmt.Errorf("review: commit: %w", err)
	}
	if reservation != nil {
		reservation.Commit()
		committed = true
	}
	return Report{
		ID: stored.ID, ReviewID: stored.ReviewID, VenueID: stored.VenueID,
		Reason: stored.Reason, Note: stored.Note, Status: stored.Status, CreatedAt: stored.CreatedAt,
	}, nil
}
