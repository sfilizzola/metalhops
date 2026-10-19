package venue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

func (s *Service) ListGenres(ctx context.Context) ([]Genre, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rows, err := s.pool.Query(queryCtx, `select id, name from genres order by name collate "C", id`)
	if err != nil {
		return nil, fmt.Errorf("venue: genres: %w", err)
	}
	defer rows.Close()
	genres := []Genre{}
	for rows.Next() {
		var genre Genre
		if err := rows.Scan(&genre.ID, &genre.Name); err != nil {
			return nil, fmt.Errorf("venue: genres: %w", err)
		}
		genres = append(genres, genre)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("venue: genres: %w", err)
	}
	return genres, nil
}

func (s *Service) ListVenues(ctx context.Context, limit int, cursor string) (Page, error) {
	if err := ValidateLimit(limit); err != nil {
		return Page{}, err
	}
	nameKey := ""
	id := "00000000-0000-4000-8000-000000000000"
	firstPage := cursor == ""
	if !firstPage {
		var err error
		nameKey, id, err = DecodeCursor(cursor)
		if err != nil {
			return Page{}, err
		}
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rows, err := s.pool.Query(queryCtx, venueSelect+`
		where v.verification_status in ('pending', 'verified')
		  and exists (select 1 from venue_genres link where link.venue_id = v.id)
		  and (
		    $1::bool
		    or (venue_name_key(v.name) collate "C", v.id)
		       > ($2::text collate "C", $3::uuid)
		  )
		order by venue_name_key(v.name) collate "C", v.id
		limit $4`, firstPage, nameKey, id, limit+1)
	if err != nil {
		return Page{}, fmt.Errorf("venue: list: %w", err)
	}
	defer rows.Close()
	venues := []Venue{}
	keys := []string{}
	for rows.Next() {
		got, key, err := scanVenue(rows)
		if err != nil {
			return Page{}, err
		}
		venues = append(venues, got)
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("venue: list: %w", err)
	}
	var next *string
	if len(venues) > limit {
		venues = venues[:limit]
		keys = keys[:limit]
		encoded := EncodeCursor(keys[len(keys)-1], venues[len(venues)-1].ID)
		next = &encoded
	}
	return Page{Venues: venues, NextCursor: next}, nil
}

func (s *Service) GetVenue(ctx context.Context, venueID string) (Venue, error) {
	if !CanonicalVenueID(venueID) {
		return Venue{}, &ValidationError{Fields: map[string]string{"venue_id": "must be a canonical uuid"}}
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	row := s.pool.QueryRow(queryCtx, venueSelect+`
		where v.id = $1::uuid
		  and v.verification_status in ('pending', 'verified')
		  and exists (select 1 from venue_genres link where link.venue_id = v.id)`, venueID)
	got, _, err := scanVenue(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Venue{}, ErrNotFound
	}
	if err != nil {
		return Venue{}, err
	}
	return got, nil
}

const venueSelect = `
	select v.id::text, v.name, v.description,
		ST_Y(v.location::geometry), ST_X(v.location::geometry),
		v.address, v.city, v.country_code, v.venue_type, v.verification_status,
		coalesce((
			select json_agg(
				json_build_object('id', g.id, 'name', g.name)
				order by g.name collate "C", g.id
			)
			from venue_genres vg
			join genres g on g.id = vg.genre_id
			where vg.venue_id = v.id
		), '[]'::json),
		null::float8, 0, 0,
		venue_name_key(v.name)
	from venues v`

type venueScanner interface {
	Scan(dest ...any) error
}

func scanVenue(row venueScanner) (Venue, string, error) {
	var got Venue
	var genres []byte
	var nameKey string
	err := row.Scan(
		&got.ID, &got.Name, &got.Description,
		&got.Latitude, &got.Longitude,
		&got.Address, &got.City, &got.CountryCode, &got.VenueType, &got.VerificationStatus,
		&genres, &got.AverageRating, &got.ReviewCount, &got.CheckInCount, &nameKey,
	)
	if err != nil {
		return Venue{}, "", err
	}
	got.Genres = []Genre{}
	if err := json.Unmarshal(genres, &got.Genres); err != nil {
		return Venue{}, "", fmt.Errorf("venue: genres: %w", err)
	}
	return got, nameKey, nil
}
