package profile

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

type ReportItem struct {
	ID        string
	Reason    string
	Note      *string
	Status    string
	CreatedAt time.Time
	Reporter  Profile
	Target    Profile
}

type ReportPage struct {
	Items      []ReportItem
	NextCursor *string
}

var canonicalUserID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (s *Service) ListReports(ctx context.Context, limit int, cursor string) (ReportPage, error) {
	if limit < 1 || limit > 100 {
		return ReportPage{}, &ValidationError{Fields: map[string]string{"limit": "must be from 1 to 100"}}
	}
	var cursorAt *time.Time
	var cursorID *string
	if cursor != "" {
		createdAt, id, err := decodeReportCursor(cursor)
		if err != nil {
			return ReportPage{}, err
		}
		cursorAt = &createdAt
		cursorID = &id
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rows, err := s.pool.Query(queryCtx, `
		select r.id::text, r.reason, r.note, r.status, r.created_at,
		       reporter.id::text, reporter.username, reporter.display_name, reporter.status,
		       reporter.moderation_reason, reporter.moderation_note, reporter.suspended_until,
		       reporter.created_at, reporter.updated_at,
		       target.id::text, target.username, target.display_name, target.status,
		       target.moderation_reason, target.moderation_note, target.suspended_until,
		       target.created_at, target.updated_at
		from profile_reports r
		join profiles reporter on reporter.id = r.reporter_user_id
		join profiles target on target.id = r.target_user_id
		where r.status = 'pending'
		  and ($1::timestamptz is null or (r.created_at, r.id) > ($1::timestamptz, $2::uuid))
		order by r.created_at, r.id
		limit $3`, cursorAt, cursorID, limit+1)
	if err != nil {
		return ReportPage{}, fmt.Errorf("profile: read: %w", err)
	}
	defer rows.Close()
	items := make([]ReportItem, 0)
	for rows.Next() {
		item, err := scanReportItem(rows)
		if err != nil {
			return ReportPage{}, fmt.Errorf("profile: read: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ReportPage{}, fmt.Errorf("profile: read: %w", err)
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		encoded := encodeReportCursor(items[len(items)-1].CreatedAt, items[len(items)-1].ID)
		next = &encoded
	}
	return ReportPage{Items: items, NextCursor: next}, nil
}

func (s *Service) GetModerationProfile(ctx context.Context, userID string) (Profile, error) {
	if !canonicalUserID.MatchString(userID) {
		return Profile{}, &ValidationError{Fields: map[string]string{"user_id": "must be a canonical uuid"}}
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	got, err := scanProfile(s.pool.QueryRow(queryCtx, `
		select `+profileColumns+`
		from profiles
		where id = $1::uuid`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("profile: read: %w", err)
	}
	return got, nil
}

func scanReportItem(row rowScanner) (ReportItem, error) {
	var item ReportItem
	err := row.Scan(
		&item.ID, &item.Reason, &item.Note, &item.Status, &item.CreatedAt,
		&item.Reporter.ID, &item.Reporter.Username, &item.Reporter.DisplayName, &item.Reporter.Status,
		&item.Reporter.ModerationReason, &item.Reporter.ModerationNote, &item.Reporter.SuspendedUntil,
		&item.Reporter.CreatedAt, &item.Reporter.UpdatedAt,
		&item.Target.ID, &item.Target.Username, &item.Target.DisplayName, &item.Target.Status,
		&item.Target.ModerationReason, &item.Target.ModerationNote, &item.Target.SuspendedUntil,
		&item.Target.CreatedAt, &item.Target.UpdatedAt,
	)
	return item, err
}

func encodeReportCursor(createdAt time.Time, id string) string {
	payload := createdAt.UTC().Format(time.RFC3339Nano) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

type ModerationCommand struct {
	ActorID        string
	TargetID       string
	Action         string
	Reason         string
	ReasonSet      bool
	Note           *string
	NoteSet        bool
	SuspendedUntil *time.Time
	UntilSet       bool
	Key            string
}

type ModerationResult struct {
	Created  bool
	ActionID *string
	Profile  Profile
}

var ErrReportNotFound = errors.New("profile: report not found")
var ErrReportResolved = errors.New("profile: report resolved")

type actionState struct {
	status string
	reason *string
	note   *string
	until  *time.Time
}

type actionCommand struct {
	UserID         string     `json:"user_id"`
	Action         string     `json:"action"`
	Reason         *string    `json:"reason"`
	Note           *string    `json:"note"`
	SuspendedUntil *time.Time `json:"suspended_until"`
}

type storedAction struct {
	ActionID string  `json:"action_id"`
	Profile  Profile `json:"profile"`
}

func (s *Service) Moderate(ctx context.Context, cmd ModerationCommand) (ModerationResult, error) {
	now := s.clock()
	state, err := normalizeAction(cmd, now)
	if err != nil {
		return ModerationResult{}, err
	}
	txCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(txCtx)
	if err != nil {
		return ModerationResult{}, fmt.Errorf("profile: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	current, err := scanProfile(tx.QueryRow(txCtx, `
		select `+profileColumns+`
		from profiles
		where id = $1::uuid
		for update`, cmd.TargetID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ModerationResult{}, ErrNotFound
	}
	if err != nil {
		return ModerationResult{}, fmt.Errorf("profile: read: %w", err)
	}
	command, err := json.Marshal(actionCommand{
		UserID: cmd.TargetID, Action: cmd.Action,
		Reason: state.reason, Note: state.note, SuspendedUntil: state.until,
	})
	if err != nil {
		return ModerationResult{}, fmt.Errorf("profile: moderate: %w", err)
	}
	req := idempotency.Request{
		UserID: cmd.ActorID, Operation: "profile_moderation_action", Key: cmd.Key, Command: command,
	}
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
			return ModerationResult{}, fmt.Errorf("profile: commit: %w", err)
		}
		return result, nil
	}
	if sameModeration(current, state) {
		if err := tx.Commit(txCtx); err != nil {
			return ModerationResult{}, fmt.Errorf("profile: commit: %w", err)
		}
		return ModerationResult{Created: false, Profile: current}, nil
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
		updated, err := scanProfile(tx.QueryRow(ctx, `
			update profiles
			set status = $2,
				moderation_reason = $3,
				moderation_note = $4,
				suspended_until = $5,
				updated_at = $6
			where id = $1::uuid
			returning `+profileColumns, cmd.TargetID, state.status, state.reason, state.note, state.until, now))
		if err != nil {
			return idempotency.Result{}, fmt.Errorf("profile: write: %w", err)
		}
		auditReason, auditNote, auditUntil := state.reason, state.note, state.until
		if cmd.Action == "restored" {
			auditReason, auditNote, auditUntil = current.ModerationReason, current.ModerationNote, nil
		}
		var actionID string
		if err := tx.QueryRow(ctx, `
			insert into profile_moderation_actions (
				subject_user_id, action, actor_type, actor_user_id, reason, note, suspended_until, created_at
			) values ($1::uuid, $2, 'operator', $3::uuid, $4, $5, $6, $7)
			returning id::text`,
			cmd.TargetID, cmd.Action, cmd.ActorID, auditReason, auditNote, auditUntil, now).Scan(&actionID); err != nil {
			return idempotency.Result{}, fmt.Errorf("profile: write: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			update profile_reports
			set status = 'actioned', resolved_at = $3, resolved_by_user_id = $2::uuid
			where target_user_id = $1::uuid and status = 'pending'`,
			cmd.TargetID, cmd.ActorID, now); err != nil {
			return idempotency.Result{}, fmt.Errorf("profile: write: %w", err)
		}
		body, err := json.Marshal(storedAction{ActionID: actionID, Profile: updated})
		if err != nil {
			return idempotency.Result{}, fmt.Errorf("profile: moderate: %w", err)
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
		return ModerationResult{}, fmt.Errorf("profile: commit: %w", err)
	}
	if reservation != nil {
		reservation.Commit()
		committed = true
	}
	return result, nil
}

func (s *Service) DismissReport(ctx context.Context, actorID, reportID string) (ReportItem, error) {
	if !canonicalUserID.MatchString(reportID) {
		return ReportItem{}, &ValidationError{Fields: map[string]string{"report_id": "must be a canonical uuid"}}
	}
	txCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(txCtx)
	if err != nil {
		return ReportItem{}, fmt.Errorf("profile: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var status string
	err = tx.QueryRow(txCtx, `
		select status
		from profile_reports
		where id = $1::uuid
		for update`, reportID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReportItem{}, ErrReportNotFound
	}
	if err != nil {
		return ReportItem{}, fmt.Errorf("profile: read: %w", err)
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
	if status == "pending" {
		next, retryAfter, ok := s.writes.Reserve(actorID)
		if !ok {
			return ReportItem{}, &RateLimitError{RetryAfter: retryAfter}
		}
		reservation = next
		if _, err := tx.Exec(txCtx, `
			update profile_reports
			set status = 'dismissed', resolved_at = $2, resolved_by_user_id = $3::uuid
			where id = $1::uuid and status = 'pending'`, reportID, s.clock(), actorID); err != nil {
			return ReportItem{}, fmt.Errorf("profile: write: %w", err)
		}
	}
	item, err := scanReportItem(tx.QueryRow(txCtx, `
		select r.id::text, r.reason, r.note, r.status, r.created_at,
		       reporter.id::text, reporter.username, reporter.display_name, reporter.status,
		       reporter.moderation_reason, reporter.moderation_note, reporter.suspended_until,
		       reporter.created_at, reporter.updated_at,
		       target.id::text, target.username, target.display_name, target.status,
		       target.moderation_reason, target.moderation_note, target.suspended_until,
		       target.created_at, target.updated_at
		from profile_reports r
		join profiles reporter on reporter.id = r.reporter_user_id
		join profiles target on target.id = r.target_user_id
		where r.id = $1::uuid`, reportID))
	if err != nil {
		return ReportItem{}, fmt.Errorf("profile: read: %w", err)
	}
	if err := tx.Commit(txCtx); err != nil {
		return ReportItem{}, fmt.Errorf("profile: commit: %w", err)
	}
	if reservation != nil {
		reservation.Commit()
		committed = true
	}
	return item, nil
}

func normalizeAction(cmd ModerationCommand, now time.Time) (actionState, error) {
	if !canonicalUserID.MatchString(cmd.TargetID) {
		return actionState{}, &ValidationError{Fields: map[string]string{"user_id": "must be a canonical uuid"}}
	}
	switch cmd.Action {
	case "rename_required", "suspended":
		return normalizeRestrictive(cmd, now)
	case "restored":
		fields := map[string]string{}
		if cmd.ReasonSet {
			fields["reason"] = "must be omitted"
		}
		if cmd.NoteSet {
			fields["note"] = "must be omitted"
		}
		if cmd.UntilSet {
			fields["suspended_until"] = "must be omitted"
		}
		if len(fields) > 0 {
			return actionState{}, &ValidationError{Fields: fields}
		}
		return actionState{status: "active"}, nil
	default:
		return actionState{}, &ValidationError{Fields: map[string]string{
			"action": "must be rename_required, suspended, or restored",
		}}
	}
}

func normalizeRestrictive(cmd ModerationCommand, now time.Time) (actionState, error) {
	if !cmd.ReasonSet || !ValidReason(cmd.Reason) {
		message := "must be a known reason"
		if !cmd.ReasonSet {
			message = "required"
		}
		return actionState{}, &ValidationError{Fields: map[string]string{"reason": message}}
	}
	var note *string
	if cmd.Note != nil {
		normalized, err := NormalizeNote(*cmd.Note)
		if err != nil {
			return actionState{}, err
		}
		note = &normalized
	}
	if NoteRequired(cmd.Reason, false) && note == nil {
		return actionState{}, &ValidationError{Fields: map[string]string{"note": "required"}}
	}
	var until *time.Time
	if cmd.Action == "rename_required" {
		if cmd.SuspendedUntil != nil {
			return actionState{}, &ValidationError{Fields: map[string]string{"suspended_until": "must be omitted"}}
		}
	} else if cmd.SuspendedUntil != nil {
		at := cmd.SuspendedUntil.UTC()
		if !at.After(now) {
			return actionState{}, &ValidationError{Fields: map[string]string{"suspended_until": "must be after the current time"}}
		}
		until = &at
	}
	reason := cmd.Reason
	return actionState{status: cmd.Action, reason: &reason, note: note, until: until}, nil
}

func sameModeration(current Profile, state actionState) bool {
	return current.Status == state.status && textEqual(current.ModerationReason, state.reason) && textEqual(current.ModerationNote, state.note) && timeEqual(current.SuspendedUntil, state.until)
}

func textEqual(got, want *string) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

func timeEqual(got, want *time.Time) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return got.Equal(*want)
}

func moderationFromStored(result idempotency.Result) (ModerationResult, error) {
	var body storedAction
	if err := json.Unmarshal(result.Body, &body); err != nil {
		return ModerationResult{}, fmt.Errorf("profile: moderate: %w", err)
	}
	id := body.ActionID
	return ModerationResult{Created: true, ActionID: &id, Profile: body.Profile}, nil
}

func decodeReportCursor(cursor string) (time.Time, string, error) {
	invalid := &ValidationError{Fields: map[string]string{"cursor": "invalid"}}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", invalid
	}
	createdAt, id, ok := strings.Cut(string(raw), "|")
	if !ok || !canonicalUserID.MatchString(id) {
		return time.Time{}, "", invalid
	}
	at, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return time.Time{}, "", invalid
	}
	return at.UTC(), id, nil
}
