package profile

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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
