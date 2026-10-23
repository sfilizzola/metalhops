package review

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

type Author struct {
	Username    *string // nil when public identity is redacted
	DisplayName string
}

type Review struct {
	ID         string
	Rating     int
	Commentary *string
	Author     Author
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Own struct {
	ID          string
	UserID      string
	Rating      int
	Commentary  *string
	Username    string
	DisplayName string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Page struct {
	Reviews    []Review
	NextCursor *string
}

type ListQuery struct {
	VenueID string
	Limit   int
	Cursor  string
}

type SaveCommand struct {
	UserID     string
	VenueID    string
	Rating     json.RawMessage // nil when omitted
	Commentary json.RawMessage // nil when omitted
}

type SaveResult struct {
	Review  Own
	Created bool
}

type Draft struct {
	Rating     int
	Commentary *string
}

var (
	ErrVenueNotFound       = errors.New("review: venue not found")
	ErrReviewNotFound      = errors.New("review: not found")
	ErrVenueNotVerified    = errors.New("review: venue not verified")
	ErrSelfReport          = errors.New("review: self report")
	ErrCommentaryNotPublic = errors.New("review: commentary not public")
	ErrReportPending       = errors.New("review: report pending")
	ErrCommentaryLocked    = errors.New("review: commentary locked")
	ErrReportNotFound      = errors.New("review: report not found")
	ErrReportResolved      = errors.New("review: report resolved")
)

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string { return "review: validation failed" }

type RateLimitError struct {
	RetryAfter int
}

func (e *RateLimitError) Error() string { return "review: rate limited" }

var (
	canonicalID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	validRating = regexp.MustCompile(`^[1-5]$`)
)

func CanonicalID(id string) bool {
	return canonicalID.MatchString(id)
}

func ValidateLimit(limit int) error {
	if limit < 1 || limit > 100 {
		return &ValidationError{Fields: map[string]string{"limit": "must be from 1 to 100"}}
	}
	return nil
}

func ParseDraft(rating, commentary json.RawMessage) (Draft, error) {
	fields := map[string]string{}
	var d Draft
	if r := bytes.TrimSpace(rating); validRating.Match(r) {
		d.Rating = int(r[0] - '0')
	} else {
		fields["rating"] = "must be between 1 and 5"
	}
	if c, msg := parseCommentary(commentary); msg != "" {
		fields["commentary"] = msg
	} else {
		d.Commentary = c
	}
	if len(fields) > 0 {
		return Draft{}, &ValidationError{Fields: fields}
	}
	return d, nil
}

func parseCommentary(raw json.RawMessage) (*string, string) {
	if raw == nil || string(bytes.TrimSpace(raw)) == "null" {
		return nil, ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return nil, "must be text"
	}
	for _, r := range text {
		if r == 0 || (unicode.IsControl(r) && !unicode.IsSpace(r)) {
			return nil, "must be text"
		}
	}
	value := norm.NFC.String(strings.TrimSpace(text))
	if value == "" {
		return nil, ""
	}
	if utf8.RuneCountInString(value) > 2000 {
		return nil, "must be at most 2000 characters"
	}
	return &value, ""
}

const cursorDiscriminator = "review-v1"

func encodeCursor(venueID string, createdAt time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(cursorDiscriminator + "|" + venueID + "|" + createdAt.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeCursor(cursor, venueID string) (time.Time, string, error) {
	invalid := &ValidationError{Fields: map[string]string{"cursor": "invalid"}}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || cursor == "" {
		return time.Time{}, "", invalid
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 4 || parts[0] != cursorDiscriminator || parts[1] != venueID || !CanonicalID(parts[3]) {
		return time.Time{}, "", invalid
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[2])
	if err != nil {
		return time.Time{}, "", invalid
	}
	return createdAt.UTC(), parts[3], nil
}
