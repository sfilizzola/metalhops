package profile

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type Identity struct {
	Username    string
	DisplayName string
}

func PublicIdentity(status, username, displayName string) Identity {
	if status == "rename_required" || status == "suspended" {
		return Identity{Username: "", DisplayName: "MetalHops User"}
	}
	return Identity{Username: username, DisplayName: displayName}
}

var ErrProfileRequired = errors.New("profile: profile required")
var ErrRenameRequired = errors.New("profile: rename required")

type SuspendedError struct{ Until *time.Time }

func (e *SuspendedError) Error() string { return "profile: suspended" }

func CommunityAccess(found bool, status string, until *time.Time) error {
	if !found {
		return ErrProfileRequired
	}
	switch status {
	case "rename_required":
		return ErrRenameRequired
	case "suspended":
		return &SuspendedError{Until: until}
	default:
		return nil
	}
}

// RequireContributor locks the Profile row for the rest of tx.
func RequireContributor(ctx context.Context, tx pgx.Tx, userID string) error {
	var status string
	var until *time.Time
	err := tx.QueryRow(ctx, `
		select status, suspended_until
		from profiles
		where id = $1::uuid
		for update`, userID).Scan(&status, &until)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommunityAccess(false, "", nil)
	}
	if err != nil {
		return fmt.Errorf("profile: read: %w", err)
	}
	return CommunityAccess(true, status, until)
}
