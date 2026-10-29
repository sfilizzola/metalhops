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

const profileColumns = `id::text, username, display_name, status, moderation_reason, moderation_note, suspended_until, created_at, updated_at,
	bio, gender, from_city, from_country, avatar_url`

// Counters stay out of profileColumns so moderation lists never count per row.
const statsQuery = `select count(*), count(distinct venue_id) from check_ins where user_id = $1::uuid`

type Service struct {
	pool        *pgxpool.Pool
	writes      *ratelimit.UserLimiter
	admin       *AdminClient
	clock       func() time.Time
	supabaseURL string
}

// supabaseURL has no trailing slash; avatar URLs must sit under its public
// avatars/<userID>/ Storage folder.
func New(pool *pgxpool.Pool, writes *ratelimit.UserLimiter, admin *AdminClient, clock func() time.Time, supabaseURL string) *Service {
	return &Service{pool: pool, writes: writes, admin: admin, clock: clock, supabaseURL: supabaseURL}
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Service) Get(ctx context.Context, userID string) (Profile, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.NormalizeExpiry(queryCtx, userID); err != nil {
		return Profile{}, err
	}
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
	if err := s.pool.QueryRow(queryCtx, statsQuery, userID).Scan(&profile.Stats.CheckIns, &profile.Stats.Venues); err != nil {
		return Profile{}, fmt.Errorf("profile: stats: %w", err)
	}
	return profile, nil
}

func (s *Service) Patch(ctx context.Context, userID string, patch Patch) (Profile, error) {
	txCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.NormalizeExpiry(txCtx, userID); err != nil {
		return Profile{}, err
	}
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

	if patch == (Patch{}) {
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

	next := current
	if patch.Username != nil {
		next.Username, err = normalizeUsername(*patch.Username)
		if err != nil {
			return Profile{}, err
		}
	}
	if patch.DisplayName != nil {
		next.DisplayName, err = normalizeDisplayName(*patch.DisplayName)
		if err != nil {
			return Profile{}, err
		}
	}
	if patch.Bio != nil {
		if next.Bio, err = normalizeBio(*patch.Bio); err != nil {
			return Profile{}, err
		}
	}
	if patch.Gender != nil {
		if next.Gender, err = normalizeGender(*patch.Gender); err != nil {
			return Profile{}, err
		}
	}
	if patch.FromCity != nil {
		if next.FromCity, err = normalizeCity(*patch.FromCity); err != nil {
			return Profile{}, err
		}
	}
	if patch.FromCountry != nil {
		if next.FromCountry, err = normalizeCountry(*patch.FromCountry); err != nil {
			return Profile{}, err
		}
	}
	if patch.AvatarURL != nil {
		if next.AvatarURL, err = normalizeAvatarURL(*patch.AvatarURL, s.supabaseURL, userID); err != nil {
			return Profile{}, err
		}
	}
	renamed := missing || next.Username != current.Username || next.DisplayName != current.DisplayName
	if !renamed && sameText(next.Bio, current.Bio) && sameText(next.Gender, current.Gender) &&
		sameText(next.FromCity, current.FromCity) && sameText(next.FromCountry, current.FromCountry) &&
		sameText(next.AvatarURL, current.AvatarURL) {
		return s.withStats(txCtx, tx, current)
	}
	if !missing && current.Status == "suspended" {
		return Profile{}, &SuspendedError{Until: current.SuspendedUntil}
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
				id, username, display_name, status, moderation_reason, moderation_note, suspended_until, created_at, updated_at,
				bio, gender, from_city, from_country, avatar_url
			) values ($1::uuid, $2, $3, 'active', null, null, null, $4, $4, $5, $6, $7, $8, $9)
			returning `+profileColumns, userID, next.Username, next.DisplayName, now,
			next.Bio, next.Gender, next.FromCity, next.FromCountry, next.AvatarURL))
	} else {
		stored, err = scanProfile(tx.QueryRow(txCtx, `
			update profiles
			set username = $2,
				display_name = $3,
				status = case when $10 then 'active' else status end,
				moderation_reason = case when $10 then null else moderation_reason end,
				moderation_note = case when $10 then null else moderation_note end,
				suspended_until = case when $10 then null else suspended_until end,
				updated_at = $4,
				bio = $5,
				gender = $6,
				from_city = $7,
				from_country = $8,
				avatar_url = $9
			where id = $1::uuid
			returning `+profileColumns, userID, next.Username, next.DisplayName, now,
			next.Bio, next.Gender, next.FromCity, next.FromCountry, next.AvatarURL, renamed))
	}
	if err != nil {
		return Profile{}, writeError(err)
	}
	// Only a name change answers a rename_required flag; other edits keep it.
	if !missing && renamed && current.Status == "rename_required" {
		if _, err := tx.Exec(txCtx, `
			insert into profile_moderation_actions (
				subject_user_id, action, actor_type, actor_user_id, reason, note
			) values ($1::uuid, 'restored', 'user', $1::uuid, $2, $3)`,
			userID, current.ModerationReason, current.ModerationNote); err != nil {
			return Profile{}, fmt.Errorf("profile: write: %w", err)
		}
	}
	if stored, err = s.withStats(txCtx, tx, stored); err != nil {
		return Profile{}, err
	}
	if err := tx.Commit(txCtx); err != nil {
		return Profile{}, writeError(err)
	}
	reservation.Commit()
	committed = true
	return stored, nil
}

func (s *Service) withStats(ctx context.Context, tx pgx.Tx, profile Profile) (Profile, error) {
	if err := tx.QueryRow(ctx, statsQuery, profile.ID).Scan(&profile.Stats.CheckIns, &profile.Stats.Venues); err != nil {
		return Profile{}, fmt.Errorf("profile: stats: %w", err)
	}
	return profile, nil
}

func sameText(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func (s *Service) NormalizeExpiry(ctx context.Context, userID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("profile: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := s.normalizeExpiry(ctx, tx, userID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("profile: commit: %w", err)
	}
	return nil
}

func (s *Service) normalizeExpiry(ctx context.Context, tx pgx.Tx, userID string) error {
	var status string
	var reason, note *string
	var until *time.Time
	err := tx.QueryRow(ctx, `
		select status, moderation_reason, moderation_note, suspended_until
		from profiles
		where id = $1::uuid
		for update`, userID).Scan(&status, &reason, &note, &until)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("profile: normalize: %w", err)
	}
	if status != "suspended" || until == nil || until.After(s.clock()) {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		insert into profile_moderation_actions (
			subject_user_id, action, actor_type, actor_user_id, reason, note
		) values ($1::uuid, 'restored', 'system', null, $2, $3)`, userID, reason, note); err != nil {
		return fmt.Errorf("profile: normalize: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		update profiles
		set status = 'active',
			moderation_reason = null,
			moderation_note = null,
			suspended_until = null,
			updated_at = $2
		where id = $1::uuid`, userID, s.clock()); err != nil {
		return fmt.Errorf("profile: normalize: %w", err)
	}
	return nil
}

func scanProfile(row rowScanner) (Profile, error) {
	var profile Profile
	err := row.Scan(
		&profile.ID, &profile.Username, &profile.DisplayName, &profile.Status,
		&profile.ModerationReason, &profile.ModerationNote, &profile.SuspendedUntil,
		&profile.CreatedAt, &profile.UpdatedAt,
		&profile.Bio, &profile.Gender, &profile.FromCity, &profile.FromCountry, &profile.AvatarURL,
	)
	return profile, err
}

func writeError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "profiles_username_identity_key" {
		return ErrUsernameTaken
	}
	return fmt.Errorf("profile: write: %w", err)
}
