package venue

import (
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

type RawSubmission struct {
	Name        *string
	Description *string
	Latitude    *float64
	Longitude   *float64
	Address     *string
	City        *string
	CountryCode *string
	VenueType   *string
	GenreIDs    []int64
}

// Draft's JSON is the idempotency command for venue_submission.
type Draft struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Address     string  `json:"address"`
	City        string  `json:"city"`
	CountryCode string  `json:"country_code"`
	VenueType   string  `json:"venue_type"`
	GenreIDs    []int   `json:"genre_ids"`
}

type Submission struct {
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
	RejectionReason    *string
	RejectionNote      *string
	Genres             []Genre
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type SubmitCommand struct {
	UserID string
	Key    string
	Fields RawSubmission
}

type CorrectCommand struct {
	UserID  string
	VenueID string
	Fields  RawSubmission
}

type OwnQuery struct {
	UserID string
	Limit  int
	Cursor string
}

type OwnPage struct {
	Submissions []Submission
	NextCursor  *string
}

type NotPendingError struct{ Status string }

func (e *NotPendingError) Error() string { return "venue: not pending" }

var errWritesUnavailable = errors.New("venue: writes are not configured")

type DuplicateMatch struct {
	VenueID            string
	VerificationStatus string
}

type DuplicateError struct{ Match *DuplicateMatch }

func (e *DuplicateError) Error() string { return "venue: duplicate" }

type QuotaError struct{ RetryAfter int }

func (e *QuotaError) Error() string { return "venue: quota exceeded" }

type RateLimitError struct{ RetryAfter int }

func (e *RateLimitError) Error() string { return "venue: rate limited" }

func ParseSubmission(raw RawSubmission) (Draft, error) {
	fields := map[string]string{}
	d := Draft{
		Name:    requiredText(fields, "name", raw.Name, 150),
		Address: requiredText(fields, "address", raw.Address, 500),
		City:    requiredText(fields, "city", raw.City, 120),
	}
	if raw.Description != nil {
		value, failure := parseDescription(*raw.Description)
		if failure != "" {
			fields["description"] = failure
		} else {
			d.Description = &value
		}
	}
	if raw.CountryCode != nil {
		d.CountryCode = strings.ToUpper(strings.TrimSpace(*raw.CountryCode))
	}
	if _, ok := countryCodes[d.CountryCode]; !ok {
		fields["country_code"] = "must be an ISO 3166-1 alpha-2 code"
	}
	if raw.VenueType == nil || (*raw.VenueType != "bar" && *raw.VenueType != "pub" && *raw.VenueType != "club") {
		fields["venue_type"] = "must be bar, pub, or club"
	} else {
		d.VenueType = *raw.VenueType
	}
	if raw.Latitude == nil || *raw.Latitude < -90 || *raw.Latitude > 90 {
		fields["latitude"] = "must be from -90 to 90"
	} else {
		d.Latitude = *raw.Latitude
	}
	if raw.Longitude == nil || *raw.Longitude < -180 || *raw.Longitude > 180 {
		fields["longitude"] = "must be from -180 to 180"
	} else {
		d.Longitude = *raw.Longitude
	}
	d.GenreIDs = parseGenreIDs(fields, raw.GenreIDs)
	if len(fields) > 0 {
		return Draft{}, &ValidationError{Fields: fields}
	}
	return d, nil
}

func requiredText(fields map[string]string, name string, raw *string, max int) string {
	if raw == nil || *raw == "" {
		fields[name] = "must be 1 to " + strconv.Itoa(max) + " characters"
		return ""
	}
	return parseText(fields, name, *raw, max)
}

func parseDescription(raw string) (string, string) {
	if !utf8.ValidString(raw) {
		return "", "must be text"
	}
	for _, r := range raw {
		if r == 0 || (unicode.IsControl(r) && !unicode.IsSpace(r)) {
			return "", "must be text"
		}
	}
	value := norm.NFC.String(strings.TrimSpace(raw))
	if n := utf8.RuneCountInString(value); n < 1 || n > 2000 {
		return "", "must be 1 to 2000 characters"
	}
	return value, ""
}

func parseGenreIDs(fields map[string]string, raw []int64) []int {
	if len(raw) == 0 {
		fields["genre_ids"] = "must include a genre"
		return nil
	}
	ids := make([]int, 0, len(raw))
	for _, id := range raw {
		if id < 1 || id > math.MaxInt32 {
			fields["genre_ids"] = "must be a positive integer"
			return nil
		}
		ids = append(ids, int(id))
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

type duplicateCandidate struct {
	ID     string
	Status string
	Own    bool
}

// disclose expects candidates ordered by distance, then id.
func disclose(candidates []duplicateCandidate) *DuplicateMatch {
	for _, c := range candidates {
		if c.Own || c.Status == "verified" {
			return &DuplicateMatch{VenueID: c.ID, VerificationStatus: c.Status}
		}
	}
	return nil
}

func quotaRetryAfter(oldest, now time.Time) int {
	return max(1, int(math.Ceil(oldest.Add(24*time.Hour).Sub(now).Seconds())))
}

func sameSubmission(current Submission, d Draft) bool {
	if current.Name != d.Name || current.Latitude != d.Latitude || current.Longitude != d.Longitude ||
		current.Address != d.Address || current.City != d.City || current.CountryCode != d.CountryCode ||
		current.VenueType != d.VenueType || (current.Description == nil) != (d.Description == nil) ||
		(current.Description != nil && *current.Description != *d.Description) {
		return false
	}
	ids := make([]int, len(current.Genres))
	for i, genre := range current.Genres {
		ids[i] = genre.ID
	}
	slices.Sort(ids)
	return slices.Equal(ids, d.GenreIDs)
}

type genreBody struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type submissionBody struct {
	ID                 string      `json:"id"`
	Name               string      `json:"name"`
	Description        *string     `json:"description"`
	Latitude           float64     `json:"latitude"`
	Longitude          float64     `json:"longitude"`
	Address            string      `json:"address"`
	City               string      `json:"city"`
	CountryCode        string      `json:"country_code"`
	VenueType          string      `json:"venue_type"`
	Genres             []genreBody `json:"genres"`
	VerificationStatus string      `json:"verification_status"`
	RejectionReason    *string     `json:"rejection_reason"`
	RejectionNote      *string     `json:"rejection_note"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
}

func bodyOf(s Submission) submissionBody {
	genres := make([]genreBody, len(s.Genres))
	for i, genre := range s.Genres {
		genres[i] = genreBody{ID: genre.ID, Name: genre.Name}
	}
	return submissionBody{
		ID: s.ID, Name: s.Name, Description: s.Description, Latitude: s.Latitude, Longitude: s.Longitude,
		Address: s.Address, City: s.City, CountryCode: s.CountryCode, VenueType: s.VenueType,
		Genres: genres, VerificationStatus: s.VerificationStatus,
		RejectionReason: s.RejectionReason, RejectionNote: s.RejectionNote,
		CreatedAt: s.CreatedAt.UTC(), UpdatedAt: s.UpdatedAt.UTC(),
	}
}

func (b submissionBody) submission() Submission {
	genres := make([]Genre, len(b.Genres))
	for i, genre := range b.Genres {
		genres[i] = Genre{ID: genre.ID, Name: genre.Name}
	}
	return Submission{
		ID: b.ID, Name: b.Name, Description: b.Description, Latitude: b.Latitude, Longitude: b.Longitude,
		Address: b.Address, City: b.City, CountryCode: b.CountryCode, VenueType: b.VenueType,
		VerificationStatus: b.VerificationStatus, Genres: genres,
		RejectionReason: b.RejectionReason, RejectionNote: b.RejectionNote,
		CreatedAt: b.CreatedAt.UTC(), UpdatedAt: b.UpdatedAt.UTC(),
	}
}
