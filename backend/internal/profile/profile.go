package profile

import (
	"errors"
	"time"
)

type Profile struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Patch struct {
	Username    *string
	DisplayName *string
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
