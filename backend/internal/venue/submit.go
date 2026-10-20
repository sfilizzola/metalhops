package venue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

const quotaSQL = `
	select count(*), min(created_at)
	from venues
	where submitted_by = $1::uuid and created_at > $2`

const duplicateSQL = `
	select v.id::text, v.verification_status, coalesce(v.submitted_by = $2::uuid, false)
	from venues v
	where venue_name_key(v.name) = $1
	  and v.verification_status in ('pending', 'verified')
	  -- shortcut: ST_Distance, geography ST_DWithin excludes exactly 100 m; use ST_DWithin if a plan needs the GiST index
	  and ST_Distance(v.location, ST_SetSRID(ST_MakePoint($3::float8, $4::float8), 4326)::geography) <= 100
	  and v.id is distinct from $5::uuid
	order by ST_Distance(v.location, ST_SetSRID(ST_MakePoint($3::float8, $4::float8), 4326)::geography), v.id`

func (s *Service) Submit(ctx context.Context, cmd SubmitCommand) (Submission, error) {
	draft, err := ParseSubmission(cmd.Fields)
	if err != nil {
		return Submission{}, err
	}
	if s.profiles == nil || s.writes == nil {
		return Submission{}, errWritesUnavailable
	}
	command, err := json.Marshal(draft)
	if err != nil {
		return Submission{}, fmt.Errorf("venue: submit: %w", err)
	}
	txCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.profiles.NormalizeExpiry(txCtx, cmd.UserID); err != nil {
		return Submission{}, err
	}
	tx, err := s.pool.Begin(txCtx)
	if err != nil {
		return Submission{}, fmt.Errorf("venue: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := profile.RequireContributor(txCtx, tx, cmd.UserID); err != nil {
		return Submission{}, err
	}
	if err := checkGenres(txCtx, tx, draft.GenreIDs); err != nil {
		return Submission{}, err
	}
	now := s.clock()
	var reservation *ratelimit.Reservation
	committed := false
	defer func() {
		if reservation != nil && !committed {
			reservation.Cancel()
		}
	}()
	result, err := idempotency.Execute(txCtx, tx, now, idempotency.Request{
		UserID: cmd.UserID, Operation: "venue_submission", Key: cmd.Key, Command: command,
	}, func(ctx context.Context) (idempotency.Result, error) {
		if err := checkQuota(ctx, tx, cmd.UserID, now); err != nil {
			return idempotency.Result{}, err
		}
		if err := checkDuplicate(ctx, tx, cmd.UserID, nil, draft); err != nil {
			return idempotency.Result{}, err
		}
		next, retryAfter, ok := s.writes.Reserve(cmd.UserID)
		if !ok {
			return idempotency.Result{}, &RateLimitError{RetryAfter: retryAfter}
		}
		reservation = next
		var id string
		if err := tx.QueryRow(ctx, `
			insert into venues (
				name, description, location, address, city, country_code, venue_type,
				verification_status, submitted_by, created_at, updated_at
			) values (
				$1, $2, ST_SetSRID(ST_MakePoint($3::float8, $4::float8), 4326)::geography,
				$5, $6, $7, $8, 'pending', $9::uuid, $10, $10
			)
			returning id::text`,
			draft.Name, draft.Description, draft.Longitude, draft.Latitude,
			draft.Address, draft.City, draft.CountryCode, draft.VenueType, cmd.UserID, now,
		).Scan(&id); err != nil {
			return idempotency.Result{}, fmt.Errorf("venue: submit: %w", err)
		}
		if err := replaceGenres(ctx, tx, id, draft.GenreIDs); err != nil {
			return idempotency.Result{}, err
		}
		got, err := ownSubmission(ctx, tx, cmd.UserID, id)
		if err != nil {
			return idempotency.Result{}, err
		}
		body, err := json.Marshal(bodyOf(got))
		if err != nil {
			return idempotency.Result{}, fmt.Errorf("venue: submit: %w", err)
		}
		return idempotency.Result{Status: 201, Body: body}, nil
	})
	if err != nil {
		return Submission{}, err
	}
	var stored submissionBody
	if err := json.Unmarshal(result.Body, &stored); err != nil {
		return Submission{}, fmt.Errorf("venue: submit: %w", err)
	}
	if err := tx.Commit(txCtx); err != nil {
		return Submission{}, fmt.Errorf("venue: commit: %w", err)
	}
	if reservation != nil {
		reservation.Commit()
		committed = true
	}
	return stored.submission(), nil
}

func checkGenres(ctx context.Context, tx pgx.Tx, ids []int) error {
	var found int
	if err := tx.QueryRow(ctx, `select count(*) from genres where id = any($1::int[])`, ids).Scan(&found); err != nil {
		return fmt.Errorf("venue: genres: %w", err)
	}
	if found != len(ids) {
		return &ValidationError{Fields: map[string]string{"genre_ids": "must be an existing genre"}}
	}
	return nil
}

func checkQuota(ctx context.Context, tx pgx.Tx, userID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('venue_submissions' || chr(31) || $1, 0))`, userID); err != nil {
		return fmt.Errorf("venue: quota lock: %w", err)
	}
	var count int
	var oldest *time.Time
	if err := tx.QueryRow(ctx, quotaSQL, userID, now.Add(-24*time.Hour)).Scan(&count, &oldest); err != nil {
		return fmt.Errorf("venue: quota: %w", err)
	}
	if count < 10 {
		return nil
	}
	retryAfter := 1
	if oldest != nil {
		retryAfter = quotaRetryAfter(*oldest, now)
	}
	return &QuotaError{RetryAfter: retryAfter}
}

// The lock and the match query must be separate statements: a statement's snapshot
// starts with the statement, so only a later statement sees the other writer's commit.
func checkDuplicate(ctx context.Context, tx pgx.Tx, userID string, self *string, d Draft) error {
	var key string
	if err := tx.QueryRow(ctx, `select venue_name_key($1)`, d.Name).Scan(&key); err != nil {
		return fmt.Errorf("venue: name key: %w", err)
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('venue_name' || chr(31) || $1, 0))`, key); err != nil {
		return fmt.Errorf("venue: name lock: %w", err)
	}
	rows, err := tx.Query(ctx, duplicateSQL, key, userID, d.Longitude, d.Latitude, self)
	if err != nil {
		return fmt.Errorf("venue: duplicates: %w", err)
	}
	defer rows.Close()
	var candidates []duplicateCandidate
	for rows.Next() {
		var c duplicateCandidate
		if err := rows.Scan(&c.ID, &c.Status, &c.Own); err != nil {
			return fmt.Errorf("venue: duplicates: %w", err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("venue: duplicates: %w", err)
	}
	if len(candidates) == 0 {
		return nil
	}
	return &DuplicateError{Match: disclose(candidates)}
}

func replaceGenres(ctx context.Context, tx pgx.Tx, venueID string, ids []int) error {
	if _, err := tx.Exec(ctx, `delete from venue_genres where venue_id = $1::uuid`, venueID); err != nil {
		return fmt.Errorf("venue: genres: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into venue_genres (venue_id, genre_id)
		select $1::uuid, unnest($2::int[])`, venueID, ids); err != nil {
		return fmt.Errorf("venue: genres: %w", err)
	}
	return nil
}
