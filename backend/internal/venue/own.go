package venue

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const ownDiscriminator = "submission-v1"

const submissionColumns = `
	v.id::text, v.name, v.description,
	ST_Y(v.location::geometry), ST_X(v.location::geometry),
	v.address, v.city, v.country_code, v.venue_type, v.verification_status,
	` + genresJSON + `,
	v.created_at, v.updated_at`

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Service) ListOwn(ctx context.Context, q OwnQuery) (OwnPage, error) {
	if err := ValidateLimit(q.Limit); err != nil {
		return OwnPage{}, err
	}
	args := []any{q.UserID}
	after := q.Cursor != ""
	if after {
		updated, id, err := decodeOwnCursor(q.Cursor)
		if err != nil {
			return OwnPage{}, err
		}
		args = append(args, updated, id)
	}
	args = append(args, q.Limit+1)
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rows, err := s.pool.Query(queryCtx, ownListSQL(after), args...)
	if err != nil {
		return OwnPage{}, fmt.Errorf("venue: own list: %w", err)
	}
	defer rows.Close()
	items := []Submission{}
	for rows.Next() {
		got, err := scanSubmission(rows)
		if err != nil {
			return OwnPage{}, err
		}
		items = append(items, got)
	}
	if err := rows.Err(); err != nil {
		return OwnPage{}, fmt.Errorf("venue: own list: %w", err)
	}
	var next *string
	if len(items) > q.Limit {
		items = items[:q.Limit]
		last := items[q.Limit-1]
		encoded := encodeOwnCursor(last.UpdatedAt, last.ID)
		next = &encoded
	}
	return OwnPage{Submissions: items, NextCursor: next}, nil
}

func (s *Service) GetOwn(ctx context.Context, userID, venueID string) (Submission, error) {
	if !CanonicalVenueID(venueID) {
		return Submission{}, &ValidationError{Fields: map[string]string{"venue_id": "must be a canonical uuid"}}
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return ownSubmission(queryCtx, s.pool, userID, venueID)
}

func ownSubmission(ctx context.Context, q queryRower, userID, venueID string) (Submission, error) {
	got, err := scanSubmission(q.QueryRow(ctx, `select `+submissionColumns+`
	from venues v
	where v.id = $1::uuid and v.submitted_by = $2::uuid`, venueID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Submission{}, ErrNotFound
	}
	return got, err
}

func ownListSQL(after bool) string {
	sql := `select ` + submissionColumns + `
	from venues v
	where v.submitted_by = $1::uuid`
	limit := "$2"
	if after {
		sql += `
	  and (v.updated_at, v.id) < ($2::timestamptz, $3::uuid)`
		limit = "$4"
	}
	return sql + `
	order by v.updated_at desc, v.id desc
	limit ` + limit
}

func scanSubmission(row venueScanner) (Submission, error) {
	var got Submission
	var genres []byte
	if err := row.Scan(
		&got.ID, &got.Name, &got.Description, &got.Latitude, &got.Longitude,
		&got.Address, &got.City, &got.CountryCode, &got.VenueType, &got.VerificationStatus,
		&genres, &got.CreatedAt, &got.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Submission{}, err
		}
		return Submission{}, fmt.Errorf("venue: submission: %w", err)
	}
	got.Genres = []Genre{}
	if err := json.Unmarshal(genres, &got.Genres); err != nil {
		return Submission{}, fmt.Errorf("venue: genres: %w", err)
	}
	got.CreatedAt = got.CreatedAt.UTC()
	got.UpdatedAt = got.UpdatedAt.UTC()
	return got, nil
}

func encodeOwnCursor(updated time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(ownDiscriminator + "|" + updated.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeOwnCursor(cursor string) (time.Time, string, error) {
	invalid := &ValidationError{Fields: map[string]string{"cursor": "invalid"}}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || cursor == "" {
		return time.Time{}, "", invalid
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 || parts[0] != ownDiscriminator || !CanonicalVenueID(parts[2]) {
		return time.Time{}, "", invalid
	}
	updated, err := time.Parse(time.RFC3339Nano, parts[1])
	if err != nil {
		return time.Time{}, "", invalid
	}
	return updated, parts[2], nil
}
