package venue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

type moderatedBody struct {
	submissionBody
	DelistingReason *string `json:"delisting_reason"`
	DelistingNote   *string `json:"delisting_note"`
}

type storedModeration struct {
	ActionID string        `json:"action_id"`
	Venue    moderatedBody `json:"venue"`
}

type moderationCommandJSON struct {
	VenueID string  `json:"venue_id"`
	Action  string  `json:"action"`
	Reason  *string `json:"reason"`
	Note    *string `json:"note"`
}

type auditRow struct {
	venueID, action, prior, next, actorID                          string
	rejectionReason, rejectionNote, delistingReason, delistingNote *string
	at                                                             time.Time
}

func moderated(ctx context.Context, q queryRower, venueID string) (Moderated, error) {
	sub, err := scanSubmission(q.QueryRow(ctx, `select `+submissionColumns+` from venues v where v.id = $1::uuid`, venueID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Moderated{}, ErrNotFound
	}
	if err != nil {
		return Moderated{}, err
	}
	out := Moderated{Submission: sub}
	if err := q.QueryRow(ctx, `select delisting_reason, delisting_note from venues where id = $1::uuid`, venueID).
		Scan(&out.DelistingReason, &out.DelistingNote); err != nil {
		return Moderated{}, fmt.Errorf("venue: delisting: %w", err)
	}
	return out, nil
}

func moderatedToBody(m Moderated) moderatedBody {
	return moderatedBody{submissionBody: bodyOf(m.Submission), DelistingReason: m.DelistingReason, DelistingNote: m.DelistingNote}
}

func (b moderatedBody) moderated() Moderated {
	return Moderated{Submission: b.submission(), DelistingReason: b.DelistingReason, DelistingNote: b.DelistingNote}
}

func insertAudit(ctx context.Context, tx pgx.Tx, a auditRow) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `
		insert into venue_moderation_actions (
			venue_id, action, prior_status, new_status,
			rejection_reason, rejection_note, delisting_reason, delisting_note,
			actor_user_id, created_at
		) values ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9::uuid, $10)
		returning id::text`,
		a.venueID, a.action, a.prior, a.next,
		a.rejectionReason, a.rejectionNote, a.delistingReason, a.delistingNote,
		a.actorID, a.at).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("venue: audit: %w", err)
	}
	return id, nil
}

func resolveReports(ctx context.Context, tx pgx.Tx, venueID, actorID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `
		update venue_reports
		set status = 'actioned', resolved_at = $3, resolved_by_user_id = $2::uuid
		where venue_id = $1::uuid and status = 'pending'`, venueID, actorID, now); err != nil {
		return fmt.Errorf("venue: reports: %w", err)
	}
	return nil
}

func textEqual(got, want *string) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

func (s *Service) ModerateVenue(ctx context.Context, cmd ModerationCommand) (ModerationResult, error) {
	state, err := normalizeModeration(cmd)
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
		return ModerationResult{}, fmt.Errorf("venue: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var status string
	var rejReason, rejNote, delReason, delNote *string
	err = tx.QueryRow(txCtx, `
		select verification_status, rejection_reason, rejection_note, delisting_reason, delisting_note
		from venues where id = $1::uuid for update`, cmd.VenueID).Scan(&status, &rejReason, &rejNote, &delReason, &delNote)
	if errors.Is(err, pgx.ErrNoRows) {
		return ModerationResult{}, ErrNotFound
	}
	if err != nil {
		return ModerationResult{}, fmt.Errorf("venue: moderate: %w", err)
	}
	command, err := json.Marshal(moderationCommandJSON{VenueID: cmd.VenueID, Action: cmd.Action, Reason: state.reason, Note: state.note})
	if err != nil {
		return ModerationResult{}, fmt.Errorf("venue: moderate: %w", err)
	}
	req := idempotency.Request{UserID: cmd.ActorID, Operation: "venue_moderation_action", Key: cmd.Key, Command: command}
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
			return ModerationResult{}, fmt.Errorf("venue: commit: %w", err)
		}
		return result, nil
	}
	if status == state.to {
		curReason, curNote := rejReason, rejNote
		if state.to == "delisted" {
			curReason, curNote = delReason, delNote
		}
		if state.to == "verified" {
			curReason, curNote = nil, nil
		}
		if !textEqual(curReason, state.reason) || !textEqual(curNote, state.note) {
			return ModerationResult{}, &InvalidTransitionError{Action: cmd.Action, Status: status}
		}
		current, err := moderated(txCtx, tx, cmd.VenueID)
		if err != nil {
			return ModerationResult{}, err
		}
		if err := tx.Commit(txCtx); err != nil {
			return ModerationResult{}, fmt.Errorf("venue: commit: %w", err)
		}
		return ModerationResult{Created: false, Venue: current}, nil
	}
	if status != state.from {
		return ModerationResult{}, &InvalidTransitionError{Action: cmd.Action, Status: status}
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
		var newRejReason, newRejNote, newDelReason, newDelNote *string
		switch state.to {
		case "rejected":
			newRejReason, newRejNote = state.reason, state.note
		case "delisted":
			newDelReason, newDelNote = state.reason, state.note
		}
		if _, err := tx.Exec(ctx, `
			update venues set verification_status = $2,
				rejection_reason = $3, rejection_note = $4, delisting_reason = $5, delisting_note = $6
			where id = $1::uuid`, cmd.VenueID, state.to, newRejReason, newRejNote, newDelReason, newDelNote); err != nil {
			return idempotency.Result{}, fmt.Errorf("venue: moderate: %w", err)
		}
		actionID, err := insertAudit(ctx, tx, auditRow{
			venueID: cmd.VenueID, action: cmd.Action, prior: status, next: state.to, actorID: cmd.ActorID,
			rejectionReason: newRejReason, rejectionNote: newRejNote, delistingReason: newDelReason, delistingNote: newDelNote, at: now,
		})
		if err != nil {
			return idempotency.Result{}, err
		}
		if err := resolveReports(ctx, tx, cmd.VenueID, cmd.ActorID, now); err != nil {
			return idempotency.Result{}, err
		}
		view, err := moderated(ctx, tx, cmd.VenueID)
		if err != nil {
			return idempotency.Result{}, err
		}
		body, err := json.Marshal(storedModeration{ActionID: actionID, Venue: moderatedToBody(view)})
		if err != nil {
			return idempotency.Result{}, fmt.Errorf("venue: moderate: %w", err)
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
		return ModerationResult{}, fmt.Errorf("venue: commit: %w", err)
	}
	if reservation != nil {
		reservation.Commit()
		committed = true
	}
	return result, nil
}

func moderationFromStored(r idempotency.Result) (ModerationResult, error) {
	var body storedModeration
	if err := json.Unmarshal(r.Body, &body); err != nil {
		return ModerationResult{}, fmt.Errorf("venue: moderate: %w", err)
	}
	id := body.ActionID
	return ModerationResult{Created: true, ActionID: &id, Venue: body.Venue.moderated()}, nil
}

func (s *Service) CorrectVenue(ctx context.Context, cmd OperatorCorrectCommand) (Moderated, error) {
	if !CanonicalVenueID(cmd.VenueID) {
		return Moderated{}, &ValidationError{Fields: map[string]string{"venue_id": "must be a canonical uuid"}}
	}
	draft, err := ParseSubmission(cmd.Fields)
	if err != nil {
		return Moderated{}, err
	}
	if s.writes == nil {
		return Moderated{}, errWritesUnavailable
	}
	now := s.clock()
	txCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(txCtx)
	if err != nil {
		return Moderated{}, fmt.Errorf("venue: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var status string
	err = tx.QueryRow(txCtx, `select verification_status from venues where id = $1::uuid for update`, cmd.VenueID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Moderated{}, ErrNotFound
	}
	if err != nil {
		return Moderated{}, fmt.Errorf("venue: correct: %w", err)
	}
	if status != "pending" && status != "verified" {
		return Moderated{}, &InvalidTransitionError{Action: "correct", Status: status}
	}
	if err := checkGenres(txCtx, tx, draft.GenreIDs); err != nil {
		return Moderated{}, err
	}
	current, err := moderated(txCtx, tx, cmd.VenueID)
	if err != nil {
		return Moderated{}, err
	}
	if sameSubmission(current.Submission, draft) {
		if err := tx.Commit(txCtx); err != nil {
			return Moderated{}, fmt.Errorf("venue: commit: %w", err)
		}
		return current, nil
	}
	// The operator's own id is not a Submitter, so only Verified matches are disclosed to them.
	if err := checkDuplicate(txCtx, tx, cmd.ActorID, &cmd.VenueID, draft); err != nil {
		return Moderated{}, err
	}
	reservation, retryAfter, ok := s.writes.Reserve(cmd.ActorID)
	if !ok {
		return Moderated{}, &RateLimitError{RetryAfter: retryAfter}
	}
	committed := false
	defer func() {
		if !committed {
			reservation.Cancel()
		}
	}()
	if err := applyDraft(txCtx, tx, cmd.VenueID, draft); err != nil {
		return Moderated{}, err
	}
	if _, err := insertAudit(txCtx, tx, auditRow{
		venueID: cmd.VenueID, action: "correct", prior: status, next: status, actorID: cmd.ActorID, at: now,
	}); err != nil {
		return Moderated{}, err
	}
	if err := resolveReports(txCtx, tx, cmd.VenueID, cmd.ActorID, now); err != nil {
		return Moderated{}, err
	}
	got, err := moderated(txCtx, tx, cmd.VenueID)
	if err != nil {
		return Moderated{}, err
	}
	if err := tx.Commit(txCtx); err != nil {
		return Moderated{}, fmt.Errorf("venue: commit: %w", err)
	}
	reservation.Commit()
	committed = true
	return got, nil
}

// shortcut: the detail lists at most 100 pending reports, oldest first; add a cursor if a Venue ever collects more.
const detailReportLimit = 100

func (s *Service) GetModerationVenue(ctx context.Context, venueID string) (ModerationVenue, error) {
	if !CanonicalVenueID(venueID) {
		return ModerationVenue{}, &ValidationError{Fields: map[string]string{"venue_id": "must be a canonical uuid"}}
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	base, err := moderated(queryCtx, s.pool, venueID)
	if err != nil {
		return ModerationVenue{}, err
	}
	out := ModerationVenue{Moderated: base, Reports: []PendingReport{}}
	var submitter nullableProfile
	if err := s.pool.QueryRow(queryCtx, `
		select `+profileColumns("sp")+`
		from venues v left join profiles sp on sp.id = v.submitted_by
		where v.id = $1::uuid`, venueID).Scan(submitter.dest()...); err != nil {
		return ModerationVenue{}, fmt.Errorf("venue: submitter: %w", err)
	}
	out.Submitter = submitter.profile()
	rows, err := s.pool.Query(queryCtx, `
		select r.id::text, r.reason, r.note, r.created_at, `+profileColumns("rp")+`
		from venue_reports r left join profiles rp on rp.id = r.reporter_user_id
		where r.venue_id = $1::uuid and r.status = 'pending'
		order by r.created_at, r.id
		limit $2`, venueID, detailReportLimit)
	if err != nil {
		return ModerationVenue{}, fmt.Errorf("venue: reports: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item PendingReport
		var reporter nullableProfile
		if err := rows.Scan(append([]any{&item.ID, &item.Reason, &item.Note, &item.CreatedAt}, reporter.dest()...)...); err != nil {
			return ModerationVenue{}, fmt.Errorf("venue: reports: %w", err)
		}
		item.CreatedAt = item.CreatedAt.UTC()
		item.Reporter = reporter.profile()
		out.Reports = append(out.Reports, item)
	}
	if err := rows.Err(); err != nil {
		return ModerationVenue{}, fmt.Errorf("venue: reports: %w", err)
	}
	return out, nil
}
