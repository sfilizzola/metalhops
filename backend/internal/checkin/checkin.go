package checkin

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/metalhops/metalhops/backend/internal/profile"
)

type VenueSummary struct {
	Name               string
	City               string
	CountryCode        string
	VenueType          *string // nil when the Venue is rejected or delisted
	VerificationStatus string
}

type CheckIn struct {
	ID         string
	VenueID    string
	VisitedAt  time.Time
	RecordedAt time.Time
	Venue      VenueSummary
}

type CreateCommand struct {
	UserID    string
	VenueID   string
	Key       string
	VisitedAt json.RawMessage // nil when omitted
}

type ListQuery struct {
	UserID string
	Limit  int
	Cursor string
}

type Page struct {
	CheckIns   []CheckIn
	NextCursor *string
}

type ValidationError struct{ Fields map[string]string }

func (e *ValidationError) Error() string { return "checkin: validation failed" }

type RateLimitError struct{ RetryAfter int }

func (e *RateLimitError) Error() string { return "checkin: rate limited" }

type SpacingError struct {
	CheckInID string
	VisitedAt time.Time
}

func (e *SpacingError) Error() string { return "checkin: spacing" }

type FeedItem struct {
	ID         string
	VisitedAt  time.Time
	RecordedAt time.Time
	Checker    profile.Identity
}

var ErrVenueNotFound = errors.New("checkin: venue not found")
var ErrCheckInNotFound = errors.New("checkin: not found")

const feedLimit = 20

var canonicalID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func CanonicalID(id string) bool {
	return canonicalID.MatchString(id)
}

func ValidateLimit(limit int) error {
	if limit < 1 || limit > 100 {
		return &ValidationError{Fields: map[string]string{"limit": "must be from 1 to 100"}}
	}
	return nil
}

func ParseVisitedAt(raw json.RawMessage) (*time.Time, error) {
	if raw == nil || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil
	}
	invalid := &ValidationError{Fields: map[string]string{"visited_at": "must be a timestamp"}}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, invalid
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil, invalid
	}
	t = t.UTC()
	return &t, nil
}

func resolveVisit(requested *time.Time, now time.Time) (time.Time, error) {
	if requested == nil {
		return now.Truncate(time.Microsecond), nil
	}
	if requested.Before(now.Add(-24 * time.Hour)) {
		return time.Time{}, &ValidationError{Fields: map[string]string{"visited_at": "must be within the last 24 hours"}}
	}
	if requested.After(now.Add(5 * time.Minute)) {
		return time.Time{}, &ValidationError{Fields: map[string]string{"visited_at": "must not be more than 5 minutes ahead"}}
	}
	if requested.After(now) {
		return now.Truncate(time.Microsecond), nil
	}
	return requested.Truncate(time.Microsecond), nil
}

const cursorDiscriminator = "check-in-v1"

func encodeCursor(visitedAt time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(cursorDiscriminator + "|" + visitedAt.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeCursor(cursor string) (time.Time, string, error) {
	invalid := &ValidationError{Fields: map[string]string{"cursor": "invalid"}}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || cursor == "" {
		return time.Time{}, "", invalid
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 || parts[0] != cursorDiscriminator || !CanonicalID(parts[2]) {
		return time.Time{}, "", invalid
	}
	visitedAt, err := time.Parse(time.RFC3339Nano, parts[1])
	if err != nil {
		return time.Time{}, "", invalid
	}
	return visitedAt.UTC(), parts[2], nil
}
