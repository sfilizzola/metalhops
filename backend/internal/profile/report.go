package profile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

type ReportCommand struct {
	ReporterID string
	Username   string
	Reason     string
	Note       *string
	Key        string
}

type Report struct {
	ID        string
	Username  string
	Reason    string
	Note      *string
	Status    string
	CreatedAt time.Time
}

type QuotaError struct{ RetryAfter int }

func (e *QuotaError) Error() string { return "profile: quota exceeded" }

var ErrSelfReport = errors.New("profile: self report")
var ErrReportPending = errors.New("profile: report pending")

type reportCommand struct {
	TargetUserID string  `json:"target_user_id"`
	Reason       string  `json:"reason"`
	Note         *string `json:"note"`
}

type reportBody struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Reason    string    `json:"reason"`
	Note      *string   `json:"note"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) Report(ctx context.Context, cmd ReportCommand) (Report, error) {
	txCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.NormalizeExpiry(txCtx, cmd.ReporterID); err != nil {
		return Report{}, err
	}
	tx, err := s.pool.Begin(txCtx)
	if err != nil {
		return Report{}, fmt.Errorf("profile: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	if err := RequireContributor(txCtx, tx, cmd.ReporterID); err != nil {
		return Report{}, err
	}
	reason, note, err := normalizeReport(cmd.Reason, cmd.Note)
	if err != nil {
		return Report{}, err
	}

	var targetID, username, targetStatus string
	err = tx.QueryRow(txCtx, `
		select id::text, username, status
		from profiles
		where lower(username) = lower($1)
		for update`, cmd.Username).Scan(&targetID, &username, &targetStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return Report{}, ErrNotFound
	}
	if err != nil {
		return Report{}, fmt.Errorf("profile: read: %w", err)
	}
	if targetID == cmd.ReporterID {
		return Report{}, ErrSelfReport
	}

	command, err := json.Marshal(reportCommand{TargetUserID: targetID, Reason: reason, Note: note})
	if err != nil {
		return Report{}, fmt.Errorf("profile: report: %w", err)
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
		UserID:    cmd.ReporterID,
		Operation: "profile_report",
		Key:       cmd.Key,
		Command:   command,
	}, func(ctx context.Context) (idempotency.Result, error) {
		if err := CheckReportQuota(ctx, tx, cmd.ReporterID, now); err != nil {
			return idempotency.Result{}, err
		}
		var pending bool
		if err := tx.QueryRow(ctx, `
			select exists (
				select 1 from profile_reports
				where reporter_user_id = $1::uuid and target_user_id = $2::uuid and status = 'pending'
			)`, cmd.ReporterID, targetID).Scan(&pending); err != nil {
			return idempotency.Result{}, fmt.Errorf("profile: report: %w", err)
		}
		if pending {
			return idempotency.Result{}, ErrReportPending
		}
		next, retryAfter, ok := s.writes.Reserve(cmd.ReporterID)
		if !ok {
			return idempotency.Result{}, &RateLimitError{RetryAfter: retryAfter}
		}
		reservation = next
		var id, storedReason, storedStatus string
		var storedNote *string
		var createdAt time.Time
		err := tx.QueryRow(ctx, `
			insert into profile_reports (
				reporter_user_id, target_user_id, reason, note, created_at
			) values ($1::uuid, $2::uuid, $3, $4, $5)
			returning id::text, reason, note, status, created_at`,
			cmd.ReporterID, targetID, reason, note, now).Scan(&id, &storedReason, &storedNote, &storedStatus, &createdAt)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "profile_reports_pending_pair_key" {
				return idempotency.Result{}, ErrReportPending
			}
			return idempotency.Result{}, fmt.Errorf("profile: report: %w", err)
		}
		if err := RecordReportEvent(ctx, tx, cmd.ReporterID, now); err != nil {
			return idempotency.Result{}, err
		}
		body, err := json.Marshal(reportBody{
			ID: id, Username: username, Reason: storedReason, Note: storedNote,
			Status: storedStatus, CreatedAt: createdAt.UTC(),
		})
		if err != nil {
			return idempotency.Result{}, fmt.Errorf("profile: report: %w", err)
		}
		return idempotency.Result{Status: 201, Body: body}, nil
	})
	if err != nil {
		return Report{}, err
	}
	var stored reportBody
	if err := json.Unmarshal(result.Body, &stored); err != nil {
		return Report{}, fmt.Errorf("profile: report: %w", err)
	}
	if err := tx.Commit(txCtx); err != nil {
		return Report{}, fmt.Errorf("profile: commit: %w", err)
	}
	if reservation != nil {
		reservation.Commit()
		committed = true
	}
	return Report{
		ID: stored.ID, Username: stored.Username, Reason: stored.Reason,
		Note: stored.Note, Status: stored.Status, CreatedAt: stored.CreatedAt,
	}, nil
}

const reportQuota = 20

// CheckReportQuota is shared by every moderation Report: profile, venue, and (later) review.
func CheckReportQuota(ctx context.Context, tx pgx.Tx, userID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('reports' || chr(31) || $1, 0))`, userID); err != nil {
		return fmt.Errorf("profile: lock: %w", err)
	}
	var count int
	var oldest *time.Time
	if err := tx.QueryRow(ctx, `
		select count(*), min(created_at)
		from report_quota_events
		where user_id = $1::uuid and created_at > $2`, userID, now.Add(-24*time.Hour)).Scan(&count, &oldest); err != nil {
		return fmt.Errorf("profile: quota: %w", err)
	}
	if count < reportQuota {
		return nil
	}
	retryAfter := 1
	if oldest != nil {
		retryAfter = max(1, int(math.Ceil(oldest.Add(24*time.Hour).Sub(now).Seconds())))
	}
	return &QuotaError{RetryAfter: retryAfter}
}

func RecordReportEvent(ctx context.Context, tx pgx.Tx, userID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `insert into report_quota_events (user_id, created_at) values ($1::uuid, $2)`, userID, now); err != nil {
		return fmt.Errorf("profile: report: %w", err)
	}
	return nil
}

func normalizeReport(reason string, note *string) (string, *string, error) {
	if !ValidReason(reason) {
		return "", nil, &ValidationError{Fields: map[string]string{"reason": "must be a known reason"}}
	}
	if note != nil {
		normalized, err := NormalizeNote(*note)
		if err != nil {
			return "", nil, err
		}
		note = &normalized
	}
	if NoteRequired(reason, true) && note == nil {
		return "", nil, &ValidationError{Fields: map[string]string{"note": "required"}}
	}
	return reason, note, nil
}
