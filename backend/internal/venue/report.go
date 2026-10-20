package venue

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

const reportCursorDiscriminator = "venue-report-v1"

// Profiles are LEFT JOINed: a deleted Submitter or a reporter without a Profile reads as null.
var reportSelect = `
	select r.id::text, r.reason, r.note, r.status, r.created_at,
	       v.id::text, v.name, v.city, v.country_code, v.verification_status,
	       ` + profileColumns("rp") + `, ` + profileColumns("sp") + `
	from venue_reports r
	join venues v on v.id = r.venue_id
	left join profiles rp on rp.id = r.reporter_user_id
	left join profiles sp on sp.id = v.submitted_by`

var reportListSQL = reportSelect + `
	where r.status = 'pending'
	  and ($1::timestamptz is null or (r.created_at, r.id) > ($1::timestamptz, $2::uuid))
	order by r.created_at, r.id
	limit $3`

var venueReportsSQL = `
	select r.id::text, r.reason, r.note, r.created_at, ` + profileColumns("rp") + `
	from venue_reports r left join profiles rp on rp.id = r.reporter_user_id
	where r.venue_id = $1::uuid and r.status = 'pending'
	order by r.created_at, r.id
	limit $2`

type venueReportCommand struct {
	VenueID string  `json:"venue_id"`
	Reason  string  `json:"reason"`
	Note    *string `json:"note"`
}

type venueReportBody struct {
	ID        string    `json:"id"`
	VenueID   string    `json:"venue_id"`
	Reason    string    `json:"reason"`
	Note      *string   `json:"note"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) ReportVenue(ctx context.Context, cmd ReportCommand) (VenueReport, error) {
	if !CanonicalVenueID(cmd.VenueID) {
		return VenueReport{}, &ValidationError{Fields: map[string]string{"venue_id": "must be a canonical uuid"}}
	}
	reason, note, err := normalizeVenueReport(cmd.Reason, cmd.Note)
	if err != nil {
		return VenueReport{}, err
	}
	if s.profiles == nil || s.writes == nil {
		return VenueReport{}, errWritesUnavailable
	}
	txCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.profiles.NormalizeExpiry(txCtx, cmd.ReporterID); err != nil {
		return VenueReport{}, err
	}
	tx, err := s.pool.Begin(txCtx)
	if err != nil {
		return VenueReport{}, fmt.Errorf("venue: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := profile.RequireContributor(txCtx, tx, cmd.ReporterID); err != nil {
		return VenueReport{}, err
	}
	// The catalogue check comes before the self-report check, so a hidden Venue is 404 even
	// for its Submitter. for share: a concurrent moderation action (for update) either commits
	// first, and this report sees the new status, or waits for this report and then resolves it.
	var submitter *string
	err = tx.QueryRow(txCtx, `
		select v.submitted_by::text from venues v
		where v.id = $1::uuid
		  and v.verification_status in ('pending', 'verified')
		  and exists (select 1 from venue_genres g where g.venue_id = v.id)
		for share of v`, cmd.VenueID).Scan(&submitter)
	if errors.Is(err, pgx.ErrNoRows) {
		return VenueReport{}, ErrNotFound
	}
	if err != nil {
		return VenueReport{}, fmt.Errorf("venue: report: %w", err)
	}
	if submitter != nil && *submitter == cmd.ReporterID {
		return VenueReport{}, ErrSelfReport
	}
	command, err := json.Marshal(venueReportCommand{VenueID: cmd.VenueID, Reason: reason, Note: note})
	if err != nil {
		return VenueReport{}, fmt.Errorf("venue: report: %w", err)
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
		UserID: cmd.ReporterID, Operation: "venue_report", Key: cmd.Key, Command: command,
	}, func(ctx context.Context) (idempotency.Result, error) {
		if err := profile.CheckReportQuota(ctx, tx, cmd.ReporterID, now); err != nil {
			return idempotency.Result{}, err
		}
		var pending bool
		if err := tx.QueryRow(ctx, `
			select exists (
				select 1 from venue_reports
				where reporter_user_id = $1::uuid and venue_id = $2::uuid and status = 'pending'
			)`, cmd.ReporterID, cmd.VenueID).Scan(&pending); err != nil {
			return idempotency.Result{}, fmt.Errorf("venue: report: %w", err)
		}
		if pending {
			return idempotency.Result{}, ErrReportPending
		}
		next, retryAfter, ok := s.writes.Reserve(cmd.ReporterID)
		if !ok {
			return idempotency.Result{}, &RateLimitError{RetryAfter: retryAfter}
		}
		reservation = next
		var stored venueReportBody
		err := tx.QueryRow(ctx, `
			insert into venue_reports (reporter_user_id, venue_id, reason, note, created_at)
			values ($1::uuid, $2::uuid, $3, $4, $5)
			on conflict (reporter_user_id, venue_id) where status = 'pending' do nothing
			returning id::text, venue_id::text, reason, note, status, created_at`,
			cmd.ReporterID, cmd.VenueID, reason, note, now).Scan(
			&stored.ID, &stored.VenueID, &stored.Reason, &stored.Note, &stored.Status, &stored.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return idempotency.Result{}, ErrReportPending
		}
		if err != nil {
			return idempotency.Result{}, fmt.Errorf("venue: report: %w", err)
		}
		stored.CreatedAt = stored.CreatedAt.UTC()
		if err := profile.RecordReportEvent(ctx, tx, cmd.ReporterID, now); err != nil {
			return idempotency.Result{}, err
		}
		body, err := json.Marshal(stored)
		if err != nil {
			return idempotency.Result{}, fmt.Errorf("venue: report: %w", err)
		}
		return idempotency.Result{Status: 201, Body: body}, nil
	})
	if err != nil {
		return VenueReport{}, err
	}
	var stored venueReportBody
	if err := json.Unmarshal(result.Body, &stored); err != nil {
		return VenueReport{}, fmt.Errorf("venue: report: %w", err)
	}
	if err := tx.Commit(txCtx); err != nil {
		return VenueReport{}, fmt.Errorf("venue: commit: %w", err)
	}
	if reservation != nil {
		reservation.Commit()
		committed = true
	}
	return VenueReport{ID: stored.ID, VenueID: stored.VenueID, Reason: stored.Reason, Note: stored.Note, Status: stored.Status, CreatedAt: stored.CreatedAt}, nil
}

func scanReportItem(row pgx.Row) (ReportItem, error) {
	var item ReportItem
	var reporter, submitter nullableProfile
	dest := []any{
		&item.ID, &item.Reason, &item.Note, &item.Status, &item.CreatedAt,
		&item.Venue.ID, &item.Venue.Name, &item.Venue.City, &item.Venue.CountryCode, &item.Venue.VerificationStatus,
	}
	dest = append(dest, reporter.dest()...)
	dest = append(dest, submitter.dest()...)
	if err := row.Scan(dest...); err != nil {
		return ReportItem{}, err
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.Reporter = reporter.profile()
	item.Submitter = submitter.profile()
	return item, nil
}

func (s *Service) ListVenueReports(ctx context.Context, limit int, cursor string) (ReportPage, error) {
	if err := ValidateLimit(limit); err != nil {
		return ReportPage{}, err
	}
	var cursorAt *time.Time
	var cursorID *string
	if cursor != "" {
		at, id, err := decodeReportCursor(cursor)
		if err != nil {
			return ReportPage{}, err
		}
		cursorAt, cursorID = &at, &id
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rows, err := s.pool.Query(queryCtx, reportListSQL, cursorAt, cursorID, limit+1)
	if err != nil {
		return ReportPage{}, fmt.Errorf("venue: reports: %w", err)
	}
	defer rows.Close()
	items := make([]ReportItem, 0)
	for rows.Next() {
		item, err := scanReportItem(rows)
		if err != nil {
			return ReportPage{}, fmt.Errorf("venue: reports: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ReportPage{}, fmt.Errorf("venue: reports: %w", err)
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		encoded := encodeReportCursor(items[limit-1].CreatedAt, items[limit-1].ID)
		next = &encoded
	}
	return ReportPage{Items: items, NextCursor: next}, nil
}

func (s *Service) DismissVenueReport(ctx context.Context, actorID, reportID string) (ReportItem, error) {
	// CanonicalVenueID is only a canonical-uuid check, so it serves report ids too.
	if !CanonicalVenueID(reportID) {
		return ReportItem{}, &ValidationError{Fields: map[string]string{"report_id": "must be a canonical uuid"}}
	}
	if s.writes == nil {
		return ReportItem{}, errWritesUnavailable
	}
	txCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(txCtx)
	if err != nil {
		return ReportItem{}, fmt.Errorf("venue: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var status string
	err = tx.QueryRow(txCtx, `select status from venue_reports where id = $1::uuid for update`, reportID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReportItem{}, ErrReportNotFound
	}
	if err != nil {
		return ReportItem{}, fmt.Errorf("venue: report: %w", err)
	}
	if status == "actioned" {
		return ReportItem{}, ErrReportResolved
	}
	var reservation *ratelimit.Reservation
	committed := false
	defer func() {
		if reservation != nil && !committed {
			reservation.Cancel()
		}
	}()
	// An already dismissed report is returned unchanged and reserves nothing.
	if status == "pending" {
		next, retryAfter, ok := s.writes.Reserve(actorID)
		if !ok {
			return ReportItem{}, &RateLimitError{RetryAfter: retryAfter}
		}
		reservation = next
		if _, err := tx.Exec(txCtx, `
			update venue_reports set status = 'dismissed', resolved_at = $2, resolved_by_user_id = $3::uuid
			where id = $1::uuid`, reportID, s.clock(), actorID); err != nil {
			return ReportItem{}, fmt.Errorf("venue: report: %w", err)
		}
	}
	item, err := scanReportItem(tx.QueryRow(txCtx, reportSelect+` where r.id = $1::uuid`, reportID))
	if err != nil {
		return ReportItem{}, fmt.Errorf("venue: report: %w", err)
	}
	if err := tx.Commit(txCtx); err != nil {
		return ReportItem{}, fmt.Errorf("venue: commit: %w", err)
	}
	if reservation != nil {
		reservation.Commit()
		committed = true
	}
	return item, nil
}

func encodeReportCursor(createdAt time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(reportCursorDiscriminator + "|" + createdAt.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeReportCursor(cursor string) (time.Time, string, error) {
	invalid := &ValidationError{Fields: map[string]string{"cursor": "invalid"}}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", invalid
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 || parts[0] != reportCursorDiscriminator || !CanonicalVenueID(parts[2]) {
		return time.Time{}, "", invalid
	}
	at, err := time.Parse(time.RFC3339Nano, parts[1])
	if err != nil {
		return time.Time{}, "", invalid
	}
	return at.UTC(), parts[2], nil
}
