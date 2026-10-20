package venue

import (
	"errors"
	"regexp"
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
	DistanceMeters     *int
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
