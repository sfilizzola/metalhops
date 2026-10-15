package profile

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

const profileColumns = `id::text, username, display_name, status, created_at, updated_at`

type Service struct {
	pool   *pgxpool.Pool
	writes *ratelimit.UserLimiter
	admin  *AdminClient
	clock  func() time.Time
}

func New(pool *pgxpool.Pool, writes *ratelimit.UserLimiter, admin *AdminClient, clock func() time.Time) *Service {
	return &Service{pool: pool, writes: writes, admin: admin, clock: clock}
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Service) Get(ctx context.Context, userID string) (Profile, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	profile, err := scanProfile(s.pool.QueryRow(queryCtx, `
		select `+profileColumns+`
		from profiles
		where id = $1::uuid`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("profile: read: %w", err)
	}
	return profile, nil
}

func (s *Service) Patch(ctx context.Context, userID string, patch Patch) (Profile, error) {
	txCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(txCtx)
	if err != nil {
		return Profile{}, fmt.Errorf("profile: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	if _, err := tx.Exec(txCtx, `
		select pg_advisory_xact_lock(hashtextextended('profile' || chr(31) || $1, 0))`, userID); err != nil {
		return Profile{}, fmt.Errorf("profile: lock: %w", err)
	}
	current, err := scanProfile(tx.QueryRow(txCtx, `
		select `+profileColumns+`
		from profiles
		where id = $1::uuid
		for update`, userID))
	missing := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !missing {
		return Profile{}, fmt.Errorf("profile: read: %w", err)
	}

	if patch.Username == nil && patch.DisplayName == nil {
		return Profile{}, &ValidationError{Fields: map[string]string{
			"username":     "required",
			"display_name": "required",
		}}
	}
	if missing && (patch.Username == nil || patch.DisplayName == nil) {
		fields := map[string]string{}
		if patch.Username == nil {
			fields["username"] = "required"
		}
		if patch.DisplayName == nil {
			fields["display_name"] = "required"
		}
		return Profile{}, &ValidationError{Fields: fields}
	}

	username, display := "", ""
	if !missing {
		username, display = current.Username, current.DisplayName
	}
	if patch.Username != nil {
		username, err = normalizeUsername(*patch.Username)
		if err != nil {
			return Profile{}, err
		}
	}
	if patch.DisplayName != nil {
		display, err = normalizeDisplayName(*patch.DisplayName)
		if err != nil {
			return Profile{}, err
		}
	}
	if !missing && username == current.Username && display == current.DisplayName {
		return current, nil
	}

	reservation, retryAfter, ok := s.writes.Reserve(userID)
	if !ok {
		return Profile{}, &RateLimitError{RetryAfter: retryAfter}
	}
	committed := false
	defer func() {
		if !committed {
			reservation.Cancel()
		}
	}()

	now := s.clock()
	var stored Profile
	if missing {
		stored, err = scanProfile(tx.QueryRow(txCtx, `
			insert into profiles (
				id, username, display_name, status, moderation_reason, moderation_note, suspended_until, created_at, updated_at
			) values ($1::uuid, $2, $3, 'active', null, null, null, $4, $4)
			returning `+profileColumns, userID, username, display, now))
	} else {
		stored, err = scanProfile(tx.QueryRow(txCtx, `
			update profiles
			set username = $2,
				display_name = $3,
				status = 'active',
				moderation_reason = null,
				moderation_note = null,
				suspended_until = null,
				updated_at = $4
			where id = $1::uuid
			returning `+profileColumns, userID, username, display, now))
	}
	if err != nil {
		return Profile{}, writeError(err)
	}
	if err := tx.Commit(txCtx); err != nil {
		return Profile{}, writeError(err)
	}
	reservation.Commit()
	committed = true
	return stored, nil
}

func scanProfile(row rowScanner) (Profile, error) {
	var profile Profile
	err := row.Scan(&profile.ID, &profile.Username, &profile.DisplayName, &profile.Status, &profile.CreatedAt, &profile.UpdatedAt)
	return profile, err
}

func writeError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "profiles_username_identity_key" {
		return ErrUsernameTaken
	}
	return fmt.Errorf("profile: write: %w", err)
}
