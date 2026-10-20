package venue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
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

func (s *Service) ListVenues(ctx context.Context, q Query) (Page, error) {
	if err := ValidateLimit(q.Limit); err != nil {
		return Page{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	textKey, cityKey, err := s.searchKeys(queryCtx, q)
	if err != nil {
		return Page{}, err
	}
	mode := q.mode()
	hash := filterHash(mode, q, textKey, cityKey)
	var after *position
	if q.Cursor != "" {
		p, err := decodeCursor(q.Cursor, mode, hash)
		if err != nil {
			return Page{}, err
		}
		after = &p
	}
	sql, args := listSQL(mode, q, textKey, cityKey, after)
	rows, err := s.pool.Query(queryCtx, sql, args...)
	if err != nil {
		return Page{}, fmt.Errorf("venue: list: %w", err)
	}
	defer rows.Close()
	venues := []Venue{}
	positions := []position{}
	for rows.Next() {
		got, p, err := scanVenue(rows)
		if err != nil {
			return Page{}, err
		}
		venues = append(venues, got)
		positions = append(positions, p)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("venue: list: %w", err)
	}
	var next *string
	if len(venues) > q.Limit {
		venues = venues[:q.Limit]
		encoded := encodeCursor(mode, hash, positions[q.Limit-1])
		next = &encoded
	}
	return Page{Venues: venues, NextCursor: next}, nil
}

func (s *Service) searchKeys(ctx context.Context, q Query) (string, string, error) {
	if q.Text == "" && q.City == "" {
		return "", "", nil
	}
	var textKey, cityKey string
	if err := s.pool.QueryRow(ctx, `select venue_search_key($1), venue_search_key($2)`, q.Text, q.City).Scan(&textKey, &cityKey); err != nil {
		return "", "", fmt.Errorf("venue: search keys: %w", err)
	}
	fields := map[string]string{}
	if q.Text != "" && textKey == "" {
		fields["query"] = "must contain a letter or digit"
	}
	if q.City != "" && cityKey == "" {
		fields["city"] = "must contain a letter or digit"
	}
	if len(fields) > 0 {
		return "", "", &ValidationError{Fields: fields}
	}
	return textKey, cityKey, nil
}

type sqlArgs []any

func (a *sqlArgs) add(value any) string {
	*a = append(*a, value)
	return "$" + strconv.Itoa(len(*a))
}

func listSQL(mode sortMode, q Query, textKey, cityKey string, after *position) (string, []any) {
	var args sqlArgs
	where := []string{
		`v.verification_status in ('pending', 'verified')`,
		`exists (select 1 from venue_genres link where link.venue_id = v.id)`,
	}
	distance, tier := "null::float8", "null::int"
	if q.Location != nil {
		point := fmt.Sprintf("ST_SetSRID(ST_MakePoint(%s::float8, %s::float8), 4326)::geography",
			args.add(q.Location.Longitude), args.add(q.Location.Latitude))
		distance = "ST_Distance(v.location, " + point + ")"
		where = append(where, fmt.Sprintf("ST_DWithin(v.location, %s, %s::float8)",
			point, args.add(float64(q.Location.RadiusKM)*1000)))
	}
	if textKey != "" {
		key := args.add(textKey) + "::text"
		contains := "'%' || " + key + " || '%'"
		tier = fmt.Sprintf(`case
			when v.search_name = %[1]s then 0
			when v.search_name like %[1]s || '%%' then 1
			when v.search_name like %[2]s then 2
			when v.search_city = %[1]s then 3
			else 4 end`, key, contains)
		where = append(where, fmt.Sprintf(
			"(v.search_name like %[1]s or v.search_address like %[1]s or v.search_city like %[1]s)", contains))
	}
	if cityKey != "" {
		where = append(where, "v.search_city = "+args.add(cityKey)+"::text")
	}
	if len(q.GenreIDs) > 0 {
		where = append(where, "exists (select 1 from venue_genres pick where pick.venue_id = v.id and pick.genre_id = any("+args.add(q.GenreIDs)+"::int[]))")
	}
	if q.CountryCode != "" {
		where = append(where, "v.country_code = "+args.add(q.CountryCode)+"::text")
	}
	if q.VenueType != "" {
		where = append(where, "v.venue_type = "+args.add(q.VenueType)+"::text")
	}

	nameKey := `venue_name_key(v.name) collate "C"`
	var order []string
	switch mode {
	case modeName:
		order = []string{nameKey, "v.id"}
	case modeDistance:
		order = []string{distance, "v.id"}
	case modeRelevanceName:
		order = []string{tier, nameKey, "v.id"}
	case modeRelevanceDistance:
		order = []string{tier, distance, "v.id"}
	}
	if after != nil {
		var bound []string
		switch mode {
		case modeName:
			bound = []string{args.add(after.NameKey) + `::text collate "C"`}
		case modeDistance:
			bound = []string{args.add(after.Distance) + "::float8"}
		case modeRelevanceName:
			bound = []string{args.add(after.Tier) + "::int", args.add(after.NameKey) + `::text collate "C"`}
		case modeRelevanceDistance:
			bound = []string{args.add(after.Tier) + "::int", args.add(after.Distance) + "::float8"}
		}
		bound = append(bound, args.add(after.ID)+"::uuid")
		where = append(where, "("+strings.Join(order, ", ")+") > ("+strings.Join(bound, ", ")+")")
	}
	sql := venueSelect(distance, tier) +
		"\n\twhere " + strings.Join(where, "\n\t  and ") +
		"\n\torder by " + strings.Join(order, ", ") +
		"\n\tlimit " + args.add(q.Limit+1)
	return sql, args
}

func (s *Service) GetVenue(ctx context.Context, venueID string) (Venue, error) {
	if !CanonicalVenueID(venueID) {
		return Venue{}, &ValidationError{Fields: map[string]string{"venue_id": "must be a canonical uuid"}}
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	row := s.pool.QueryRow(queryCtx, venueSelect("null::float8", "null::int")+`
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

func venueSelect(distance, tier string) string {
	return `
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
		venue_name_key(v.name), ` + distance + `, ` + tier + `
	from venues v`
}

type venueScanner interface {
	Scan(dest ...any) error
}

func scanVenue(row venueScanner) (Venue, position, error) {
	var got Venue
	var genres []byte
	var p position
	var distance *float64
	var tier *int
	err := row.Scan(
		&got.ID, &got.Name, &got.Description,
		&got.Latitude, &got.Longitude,
		&got.Address, &got.City, &got.CountryCode, &got.VenueType, &got.VerificationStatus,
		&genres, &got.AverageRating, &got.ReviewCount, &got.CheckInCount,
		&p.NameKey, &distance, &tier,
	)
	if err != nil {
		return Venue{}, position{}, err
	}
	got.Genres = []Genre{}
	if err := json.Unmarshal(genres, &got.Genres); err != nil {
		return Venue{}, position{}, fmt.Errorf("venue: genres: %w", err)
	}
	p.ID = got.ID
	if distance != nil {
		p.Distance = *distance
		meters := int(math.Floor(*distance + 0.5))
		got.DistanceMeters = &meters
	}
	if tier != nil {
		p.Tier = *tier
	}
	return got, p, nil
}
