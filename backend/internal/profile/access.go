package profile

import (
	"errors"
	"time"
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
