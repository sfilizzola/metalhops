package checkin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

type Service struct {
	pool     *pgxpool.Pool
	profiles *profile.Service
	writes   *ratelimit.UserLimiter
	clock    func() time.Time
	spacing  time.Duration
}

// profiles and writes may be nil for read-only use; writes then fail.
func New(pool *pgxpool.Pool, profiles *profile.Service, writes *ratelimit.UserLimiter, clock func() time.Time, spacing time.Duration) *Service {
	return &Service{pool: pool, profiles: profiles, writes: writes, clock: clock, spacing: spacing}
}

var errWritesUnavailable = errors.New("checkin: writes are not configured")

func invalidVenueID() error {
	return &ValidationError{Fields: map[string]string{"venue_id": "must be a canonical uuid"}}
}

type createCommandJSON struct {
	VenueID   string  `json:"venue_id"`
	VisitedAt *string `json:"visited_at"`
}

type storedVenueSummary struct {
	Name               string  `json:"name"`
	City               string  `json:"city"`
	CountryCode        string  `json:"country_code"`
	VenueType          *string `json:"venue_type,omitempty"`
	VerificationStatus string  `json:"verification_status"`
}

type storedCheckIn struct {
	ID         string             `json:"id"`
	VenueID    string             `json:"venue_id"`
	VisitedAt  time.Time          `json:"visited_at"`
	RecordedAt time.Time          `json:"recorded_at"`
	Venue      storedVenueSummary `json:"venue"`
}

func toStored(c CheckIn) storedCheckIn {
	return storedCheckIn{
		ID:         c.ID,
		VenueID:    c.VenueID,
		VisitedAt:  c.VisitedAt,
		RecordedAt: c.RecordedAt,
		Venue: storedVenueSummary{
			Name:               c.Venue.Name,
			City:               c.Venue.City,
			CountryCode:        c.Venue.CountryCode,
			VenueType:          c.Venue.VenueType,
			VerificationStatus: c.Venue.VerificationStatus,
		},
	}
}

func fromStored(body storedCheckIn) CheckIn {
	return CheckIn{
		ID:         body.ID,
		VenueID:    body.VenueID,
		VisitedAt:  body.VisitedAt.UTC(),
		RecordedAt: body.RecordedAt.UTC(),
		Venue: VenueSummary{
			Name:               body.Venue.Name,
			City:               body.Venue.City,
			CountryCode:        body.Venue.CountryCode,
			VenueType:          body.Venue.VenueType,
			VerificationStatus: body.Venue.VerificationStatus,
		},
	}
}

func decodeStoredCheckIn(body []byte) (CheckIn, error) {
	var stored storedCheckIn
	if err := json.Unmarshal(body, &stored); err != nil {
		return CheckIn{}, fmt.Errorf("checkin: decode: %w", err)
	}
	return fromStored(stored), nil
}

// spacingSQL finds the nearest existing Check-in within the spacing window of visit, inclusive of
// the boundary. A tie in distance breaks on id so concurrent callers agree on the same conflict.
const spacingSQL = `
	select id::text, visited_at from check_ins
	where user_id = $1::uuid and venue_id = $2::uuid
	  and visited_at >= $3 and visited_at <= $4
	order by abs(extract(epoch from visited_at - $5::timestamptz)), id
	limit 1`

func (s *Service) Create(ctx context.Context, cmd CreateCommand) (CheckIn, error) {
	if !CanonicalID(cmd.VenueID) {
		return CheckIn{}, invalidVenueID()
	}
	requested, err := ParseVisitedAt(cmd.VisitedAt)
	if err != nil {
		return CheckIn{}, err
	}
	if s.profiles == nil || s.writes == nil {
		return CheckIn{}, errWritesUnavailable
	}
	now := s.clock()
	var visitedAtStr *string
	if requested != nil {
		formatted := requested.UTC().Format(time.RFC3339Nano)
		visitedAtStr = &formatted
	}
	command, err := json.Marshal(createCommandJSON{VenueID: cmd.VenueID, VisitedAt: visitedAtStr})
	if err != nil {
		return CheckIn{}, fmt.Errorf("checkin: command: %w", err)
	}

	txCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.profiles.NormalizeExpiry(txCtx, cmd.UserID); err != nil {
		return CheckIn{}, err
	}
	tx, err := s.pool.Begin(txCtx)
	if err != nil {
		return CheckIn{}, fmt.Errorf("checkin: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	req := idempotency.Request{UserID: cmd.UserID, Operation: "check_in", Key: cmd.Key, Command: command}
	stored, found, err := idempotency.Lookup(txCtx, tx, now, req)
	if err != nil {
		return CheckIn{}, err
	}
	if found {
		checkIn, err := decodeStoredCheckIn(stored.Body)
		if err != nil {
			return CheckIn{}, err
		}
		if err := tx.Commit(txCtx); err != nil {
			return CheckIn{}, fmt.Errorf("checkin: commit: %w", err)
		}
		return checkIn, nil
	}

	visit, err := resolveVisit(requested, now)
	if err != nil {
		return CheckIn{}, err
	}

	var summary VenueSummary
	err = tx.QueryRow(txCtx, `
		select v.name, v.city, v.country_code, v.venue_type, v.verification_status
		from venues v
		where v.id = $1::uuid
		  and v.verification_status in ('pending', 'verified')
		  and exists (select 1 from venue_genres g where g.venue_id = v.id)
		for share of v`, cmd.VenueID).Scan(
		&summary.Name, &summary.City, &summary.CountryCode, &summary.VenueType, &summary.VerificationStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return CheckIn{}, ErrVenueNotFound
	}
	if err != nil {
		return CheckIn{}, fmt.Errorf("checkin: venue: %w", err)
	}

	if err := profile.RequireContributor(txCtx, tx, cmd.UserID); err != nil {
		return CheckIn{}, err
	}

	if _, err := tx.Exec(txCtx, `
		select pg_advisory_xact_lock(hashtextextended('check_in' || chr(31) || $1 || chr(31) || $2, 0))`,
		cmd.UserID, cmd.VenueID); err != nil {
		return CheckIn{}, fmt.Errorf("checkin: lock: %w", err)
	}

	var reservation *ratelimit.Reservation
	committed := false
	defer func() {
		if reservation != nil && !committed {
			reservation.Cancel()
		}
	}()
	executed, err := idempotency.Execute(txCtx, tx, now, req, func(ctx context.Context) (idempotency.Result, error) {
		var conflictID string
		var conflictVisited time.Time
		err := tx.QueryRow(ctx, spacingSQL,
			cmd.UserID, cmd.VenueID, visit.Add(-s.spacing), visit.Add(s.spacing), visit,
		).Scan(&conflictID, &conflictVisited)
		if err == nil {
			return idempotency.Result{}, &SpacingError{CheckInID: conflictID, VisitedAt: conflictVisited.UTC()}
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return idempotency.Result{}, fmt.Errorf("checkin: spacing: %w", err)
		}
		next, retryAfter, ok := s.writes.Reserve(cmd.UserID)
		if !ok {
			return idempotency.Result{}, &RateLimitError{RetryAfter: retryAfter}
		}
		reservation = next
		recordedAt := now.Truncate(time.Microsecond)
		var id string
		if err := tx.QueryRow(ctx, `
			insert into check_ins (venue_id, user_id, visited_at, recorded_at)
			values ($1::uuid, $2::uuid, $3, $4)
			returning id::text`, cmd.VenueID, cmd.UserID, visit, recordedAt).Scan(&id); err != nil {
			return idempotency.Result{}, fmt.Errorf("checkin: insert: %w", err)
		}
		body, err := json.Marshal(toStored(CheckIn{
			ID: id, VenueID: cmd.VenueID, VisitedAt: visit, RecordedAt: recordedAt, Venue: summary,
		}))
		if err != nil {
			return idempotency.Result{}, fmt.Errorf("checkin: marshal: %w", err)
		}
		return idempotency.Result{Status: 201, Body: body}, nil
	})
	if err != nil {
		return CheckIn{}, err
	}
	checkIn, err := decodeStoredCheckIn(executed.Body)
	if err != nil {
		return CheckIn{}, err
	}
	if err := tx.Commit(txCtx); err != nil {
		return CheckIn{}, fmt.Errorf("checkin: commit: %w", err)
	}
	if reservation != nil {
		reservation.Commit()
		committed = true
	}
	return checkIn, nil
}

const historySQL = `
	select c.id::text, c.venue_id::text, c.visited_at, c.recorded_at,
	       v.name, v.city, v.country_code, v.venue_type, v.verification_status
	from check_ins c
	join venues v on v.id = c.venue_id
	where c.user_id = $1::uuid
	  and ($2::timestamptz is null or (c.visited_at, c.id) < ($2, $3::uuid))
	order by c.visited_at desc, c.id desc
	limit $4`

func (s *Service) ListOwn(ctx context.Context, q ListQuery) (Page, error) {
	if err := ValidateLimit(q.Limit); err != nil {
		return Page{}, err
	}
	var afterAt *time.Time
	var afterID *string
	if q.Cursor != "" {
		at, id, err := decodeCursor(q.Cursor)
		if err != nil {
			return Page{}, err
		}
		afterAt, afterID = &at, &id
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rows, err := s.pool.Query(queryCtx, historySQL, q.UserID, afterAt, afterID, q.Limit+1)
	if err != nil {
		return Page{}, fmt.Errorf("checkin: list: %w", err)
	}
	defer rows.Close()
	checkIns := []CheckIn{}
	for rows.Next() {
		var c CheckIn
		if err := rows.Scan(&c.ID, &c.VenueID, &c.VisitedAt, &c.RecordedAt,
			&c.Venue.Name, &c.Venue.City, &c.Venue.CountryCode, &c.Venue.VenueType, &c.Venue.VerificationStatus); err != nil {
			return Page{}, fmt.Errorf("checkin: list: %w", err)
		}
		c.VisitedAt, c.RecordedAt = c.VisitedAt.UTC(), c.RecordedAt.UTC()
		if c.Venue.VerificationStatus == "rejected" || c.Venue.VerificationStatus == "delisted" {
			c.Venue.VenueType = nil
		}
		checkIns = append(checkIns, c)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("checkin: list: %w", err)
	}
	page := Page{CheckIns: checkIns}
	if len(checkIns) > q.Limit {
		page.CheckIns = checkIns[:q.Limit]
		last := page.CheckIns[q.Limit-1]
		next := encodeCursor(last.VisitedAt, last.ID)
		page.NextCursor = &next
	}
	return page, nil
}

const feedSQL = `
	select c.id::text, c.visited_at, c.recorded_at, p.status, p.username, p.display_name
	from check_ins c
	join profiles p on p.id = c.user_id
	where c.venue_id = $1::uuid
	order by c.recorded_at desc, c.id desc
	limit $2`

func (s *Service) ListVenueFeed(ctx context.Context, venueID, callerUserID string) ([]FeedItem, error) {
	if !CanonicalID(venueID) {
		return nil, invalidVenueID()
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var one int
	err := s.pool.QueryRow(queryCtx, `
		select 1
		from venues v
		where v.id = $1::uuid
		  and v.verification_status in ('pending', 'verified')
		  and exists (select 1 from venue_genres g where g.venue_id = v.id)
		for share of v`, venueID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrVenueNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("checkin: venue: %w", err)
	}
	err = s.pool.QueryRow(queryCtx, `select 1 from profiles where id = $1::uuid`, callerUserID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, profile.ErrProfileRequired
	}
	if err != nil {
		return nil, fmt.Errorf("checkin: caller: %w", err)
	}
	rows, err := s.pool.Query(queryCtx, feedSQL, venueID, feedLimit)
	if err != nil {
		return nil, fmt.Errorf("checkin: feed: %w", err)
	}
	defer rows.Close()
	items := []FeedItem{}
	for rows.Next() {
		var item FeedItem
		var status, username, displayName string
		if err := rows.Scan(&item.ID, &item.VisitedAt, &item.RecordedAt, &status, &username, &displayName); err != nil {
			return nil, fmt.Errorf("checkin: feed: %w", err)
		}
		item.VisitedAt, item.RecordedAt = item.VisitedAt.UTC(), item.RecordedAt.UTC()
		item.Checker = profile.PublicIdentity(status, username, displayName)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("checkin: feed: %w", err)
	}
	return items, nil
}

func (s *Service) Delete(ctx context.Context, userID, checkInID string) error {
	if !CanonicalID(checkInID) {
		return &ValidationError{Fields: map[string]string{"check_in_id": "must be a canonical uuid"}}
	}
	if s.profiles == nil || s.writes == nil {
		return errWritesUnavailable
	}
	txCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.profiles.NormalizeExpiry(txCtx, userID); err != nil {
		return err
	}
	tx, err := s.pool.Begin(txCtx)
	if err != nil {
		return fmt.Errorf("checkin: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var one int
	err = tx.QueryRow(txCtx, `
		select 1 from check_ins where id = $1::uuid and user_id = $2::uuid for update`,
		checkInID, userID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCheckInNotFound
	}
	if err != nil {
		return fmt.Errorf("checkin: read: %w", err)
	}
	if err := profile.RequireContributor(txCtx, tx, userID); err != nil {
		return err
	}
	reservation, retryAfter, ok := s.writes.Reserve(userID)
	if !ok {
		return &RateLimitError{RetryAfter: retryAfter}
	}
	committed := false
	defer func() {
		if !committed {
			reservation.Cancel()
		}
	}()
	if _, err := tx.Exec(txCtx, `delete from check_ins where id = $1::uuid`, checkInID); err != nil {
		return fmt.Errorf("checkin: delete: %w", err)
	}
	if err := tx.Commit(txCtx); err != nil {
		return fmt.Errorf("checkin: commit: %w", err)
	}
	reservation.Commit()
	committed = true
	return nil
}
