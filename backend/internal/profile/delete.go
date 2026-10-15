package profile

import (
	"context"
	"time"
)

func (s *Service) Delete(ctx context.Context, cmd DeleteCommand) error {
	if cmd.Confirmation != "DELETE MY ACCOUNT" {
		return &ValidationError{Fields: map[string]string{
			"confirmation": "must equal DELETE MY ACCOUNT",
		}}
	}
	if cmd.SessionCreatedAt.Before(s.clock().Add(-10 * time.Minute)) {
		return ErrRecentAuthenticationRequired
	}
	reservation, retryAfter, ok := s.writes.Reserve(cmd.UserID)
	if !ok {
		return &RateLimitError{RetryAfter: retryAfter}
	}
	committed := false
	defer func() {
		if !committed {
			reservation.Cancel()
		}
	}()
	if err := s.admin.DeleteUser(ctx, cmd.UserID); err != nil {
		return ErrAuthProvider
	}
	reservation.Commit()
	committed = true
	return nil
}
