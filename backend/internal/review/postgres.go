package review

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

type Service struct {
	pool     *pgxpool.Pool
	profiles *profile.Service
	writes   *ratelimit.UserLimiter
	clock    func() time.Time
}

// profiles and writes may be nil for read-only use; writes then fail.
func New(pool *pgxpool.Pool, profiles *profile.Service, writes *ratelimit.UserLimiter, clock func() time.Time) *Service {
	return &Service{pool: pool, profiles: profiles, writes: writes, clock: clock}
}

func invalidVenueID() error {
	return &ValidationError{Fields: map[string]string{"venue_id": "must be a canonical uuid"}}
}

// listSQL walks reviews_venue_list_idx; $2 and $3 are the cursor position or both null.
const listSQL = `
	select r.id::text, r.rating, r.commentary, r.created_at, r.updated_at,
		p.status, p.username, p.display_name
	from reviews r
	join profiles p on p.id = r.user_id
	where r.venue_id = $1::uuid
	  and ($2::timestamptz is null or (r.created_at, r.id) < ($2, $3::uuid))
	order by r.created_at desc, r.id desc
	limit $4`

func (s *Service) List(ctx context.Context, q ListQuery) (Page, error) {
	if !CanonicalID(q.VenueID) {
		return Page{}, invalidVenueID()
	}
	if err := ValidateLimit(q.Limit); err != nil {
		return Page{}, err
	}
	var cursorAt *time.Time
	var cursorID *string
	if q.Cursor != "" {
		at, id, err := decodeCursor(q.Cursor, q.VenueID)
		if err != nil {
			return Page{}, err
		}
		cursorAt, cursorID = &at, &id
	}
	status, err := s.publicStatus(ctx, q.VenueID)
	if err != nil {
		return Page{}, err
	}
	if status != "verified" {
		return Page{Reviews: []Review{}}, nil
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rows, err := s.pool.Query(queryCtx, listSQL, q.VenueID, cursorAt, cursorID, q.Limit+1)
	if err != nil {
		return Page{}, fmt.Errorf("review: list: %w", err)
	}
	defer rows.Close()
	reviews := []Review{}
	for rows.Next() {
		var r Review
		var status, username, displayName string
		if err := rows.Scan(&r.ID, &r.Rating, &r.Commentary, &r.CreatedAt, &r.UpdatedAt, &status, &username, &displayName); err != nil {
			return Page{}, fmt.Errorf("review: list: %w", err)
		}
		r.CreatedAt, r.UpdatedAt = r.CreatedAt.UTC(), r.UpdatedAt.UTC()
		identity := profile.PublicIdentity(status, username, displayName)
		r.Author.DisplayName = identity.DisplayName
		if identity.Username != "" {
			r.Author.Username = &identity.Username
		}
		reviews = append(reviews, r)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("review: list: %w", err)
	}
	var next *string
	if len(reviews) > q.Limit {
		reviews = reviews[:q.Limit]
		last := reviews[q.Limit-1]
		encoded := encodeCursor(q.VenueID, last.CreatedAt, last.ID)
		next = &encoded
	}
	return Page{Reviews: reviews, NextCursor: next}, nil
}

// publicStatus returns the status of a public Venue, or ErrVenueNotFound when it is hidden.
func (s *Service) publicStatus(ctx context.Context, venueID string) (string, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var status string
	err := s.pool.QueryRow(queryCtx, `
		select v.verification_status from venues v
		where v.id = $1::uuid
		  and v.verification_status in ('pending', 'verified')
		  and exists (select 1 from venue_genres g where g.venue_id = v.id)`, venueID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrVenueNotFound
	}
	if err != nil {
		return "", fmt.Errorf("review: venue: %w", err)
	}
	return status, nil
}

func (s *Service) GetOwn(ctx context.Context, userID, venueID string) (Own, error) {
	if !CanonicalID(venueID) {
		return Own{}, invalidVenueID()
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var own Own
	err := s.pool.QueryRow(queryCtx, `
		select r.id::text, r.user_id::text, r.rating, r.commentary, p.username, p.display_name, r.created_at, r.updated_at
		from reviews r
		join profiles p on p.id = r.user_id
		join venues v on v.id = r.venue_id
		where r.venue_id = $1::uuid and r.user_id = $2::uuid
		  and v.verification_status = 'verified'
		  and exists (select 1 from venue_genres g where g.venue_id = v.id)`, venueID, userID).Scan(
		&own.ID, &own.UserID, &own.Rating, &own.Commentary, &own.Username, &own.DisplayName, &own.CreatedAt, &own.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Own{}, ErrReviewNotFound
	}
	if err != nil {
		return Own{}, fmt.Errorf("review: own: %w", err)
	}
	own.CreatedAt, own.UpdatedAt = own.CreatedAt.UTC(), own.UpdatedAt.UTC()
	return own, nil
}
