package profile

import (
	"errors"
	"testing"
	"time"
)

func TestPublicIdentity(t *testing.T) {
	active := PublicIdentity("active", "MetalFan", "Metal Fan")
	if active.Username != "MetalFan" || active.DisplayName != "Metal Fan" {
		t.Fatalf("%+v", active)
	}
	for _, status := range []string{"rename_required", "suspended"} {
		got := PublicIdentity(status, "MetalFan", "Metal Fan")
		if got.Username != "" || got.DisplayName != "MetalHops User" {
			t.Fatalf("%s %+v", status, got)
		}
	}
}

func TestCommunityAccess(t *testing.T) {
	if !errors.Is(CommunityAccess(false, "", nil), ErrProfileRequired) {
		t.Fatal("missing profile")
	}
	if !errors.Is(CommunityAccess(true, "rename_required", nil), ErrRenameRequired) {
		t.Fatal("rename")
	}
	until := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	err := CommunityAccess(true, "suspended", &until)
	var suspended *SuspendedError
	if !errors.As(err, &suspended) || suspended.Until == nil || !suspended.Until.Equal(until) {
		t.Fatal(err)
	}
	if err := CommunityAccess(true, "suspended", nil); !errors.As(err, &suspended) || suspended.Until != nil {
		t.Fatal(err)
	}
	if err := CommunityAccess(true, "active", nil); err != nil {
		t.Fatal(err)
	}
}
