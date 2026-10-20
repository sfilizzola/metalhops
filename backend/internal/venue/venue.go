package venue

import (
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

type Genre struct {
	ID   int
	Name string
}

type Venue struct {
	ID                 string
	Name               string
	Description        *string
	Latitude           float64
	Longitude          float64
	Address            string
	City               string
	CountryCode        string
	VenueType          string
	VerificationStatus string
	Genres             []Genre
	AverageRating      *float64
	ReviewCount        int
	CheckInCount       int
}

type Page struct {
	Venues     []Venue
	NextCursor *string
}

var ErrNotFound = errors.New("venue: not found")

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string { return "venue: validation failed" }

var canonicalVenueID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func CanonicalVenueID(id string) bool {
	return canonicalVenueID.MatchString(id)
}

func ValidateLimit(limit int) error {
	if limit < 1 || limit > 100 {
		return &ValidationError{Fields: map[string]string{"limit": "must be from 1 to 100"}}
	}
	return nil
}

func EncodeCursor(nameKey, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte("name-v1|" + nameKey + "|" + id))
}

func DecodeCursor(cursor string) (string, string, error) {
	invalid := &ValidationError{Fields: map[string]string{"cursor": "invalid"}}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || cursor == "" {
		return "", "", invalid
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 || parts[0] != "name-v1" || !CanonicalVenueID(parts[2]) {
		return "", "", invalid
	}
	if !utf8.ValidString(parts[1]) || strings.ContainsRune(parts[1], 0) {
		return "", "", invalid
	}
	return parts[1], parts[2], nil
}
