package profile

import (
	"errors"
	"time"
)

type Profile struct {
	ID               string     `json:"id"`
	Username         string     `json:"username"`
	DisplayName      string     `json:"display_name"`
	Status           string     `json:"status"`
	ModerationReason *string    `json:"moderation_reason"`
	ModerationNote   *string    `json:"moderation_note"`
	SuspendedUntil   *time.Time `json:"suspended_until"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	// Owner-only fields: api.profileView serves them on /v1/me, and the
	// moderation JSON, which marshals Profile directly, leaves them out.
	Bio         *string `json:"-"`
	Gender      *string `json:"-"`
	FromCity    *string `json:"-"`
	FromCountry *string `json:"-"`
	AvatarURL   *string `json:"-"`
	Stats       Stats   `json:"-"`
}

type Stats struct {
	CheckIns int `json:"check_ins"`
	Venues   int `json:"venues"`
}

// Patch fields are nil when absent. An empty string clears Bio, Gender,
// FromCity, FromCountry, and AvatarURL.
type Patch struct {
	Username    *string
	DisplayName *string
	Bio         *string
	Gender      *string
	FromCity    *string
	FromCountry *string
	AvatarURL   *string
}

type DeleteCommand struct {
	UserID           string
	SessionCreatedAt time.Time
	Confirmation     string
}

var ErrNotFound = errors.New("profile: not found")
var ErrUsernameTaken = errors.New("profile: username taken")
var ErrUsernameReserved = errors.New("profile: username reserved")
var ErrRecentAuthenticationRequired = errors.New("profile: recent authentication required")
var ErrAuthProvider = errors.New("profile: auth provider")

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string { return "profile: validation failed" }

type RateLimitError struct {
	RetryAfter int
}

func (e *RateLimitError) Error() string { return "profile: rate limited" }
