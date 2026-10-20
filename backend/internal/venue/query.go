package venue

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

type RawQuery struct {
	Limit       int
	Cursor      string
	Query       string
	GenreIDs    []string
	City        string
	CountryCode string
	VenueType   string
	Latitude    string
	Longitude   string
	RadiusKM    string
}

type Location struct {
	Latitude  float64
	Longitude float64
	RadiusKM  int
}

type Query struct {
	Limit       int
	Cursor      string
	Text        string
	GenreIDs    []int
	City        string
	CountryCode string
	VenueType   string
	Location    *Location
}

type sortMode int

const (
	modeName sortMode = iota
	modeDistance
	modeRelevanceName
	modeRelevanceDistance
)

func (q Query) mode() sortMode {
	switch {
	case q.Text != "" && q.Location != nil:
		return modeRelevanceDistance
	case q.Text != "":
		return modeRelevanceName
	case q.Location != nil:
		return modeDistance
	}
	return modeName
}

const locationGroup = "must be provided with latitude, longitude, and radius_km"

func ParseQuery(raw RawQuery) (Query, error) {
	fields := map[string]string{}
	q := Query{Limit: raw.Limit, Cursor: raw.Cursor}
	if ValidateLimit(raw.Limit) != nil {
		fields["limit"] = "must be from 1 to 100"
	}
	q.Text = parseText(fields, "query", raw.Query, 100)
	q.City = parseText(fields, "city", raw.City, 120)
	q.GenreIDs = parseGenres(fields, raw.GenreIDs)
	if raw.CountryCode != "" {
		if _, ok := countryCodes[raw.CountryCode]; !ok {
			fields["country_code"] = "must be an ISO 3166-1 alpha-2 code"
		}
		q.CountryCode = raw.CountryCode
	}
	if raw.VenueType != "" {
		if raw.VenueType != "bar" && raw.VenueType != "pub" && raw.VenueType != "club" {
			fields["venue_type"] = "must be bar, pub, or club"
		}
		q.VenueType = raw.VenueType
	}
	q.Location = parseLocation(fields, raw)
	if len(fields) > 0 {
		return Query{}, &ValidationError{Fields: fields}
	}
	return q, nil
}

func parseText(fields map[string]string, name, raw string, max int) string {
	if raw == "" {
		return ""
	}
	if !utf8.ValidString(raw) || disallowedText(raw) {
		fields[name] = "must be a single line of text"
		return ""
	}
	value := norm.NFC.String(strings.TrimSpace(raw))
	if n := utf8.RuneCountInString(value); n < 1 || n > max {
		fields[name] = "must be 1 to " + strconv.Itoa(max) + " characters"
		return ""
	}
	return value
}

func disallowedText(value string) bool {
	for _, r := range value {
		if r == '\n' || r == '\r' || r == 0 || (unicode.IsControl(r) && !unicode.IsSpace(r)) {
			return true
		}
	}
	return false
}

func parseGenres(fields map[string]string, raw []string) []int {
	seen := map[int]bool{}
	var ids []int
	for _, value := range raw {
		if value == "" {
			continue
		}
		id, err := strconv.ParseInt(value, 10, 32)
		if err != nil || id < 1 {
			fields["genre_id"] = "must be a positive integer"
			return nil
		}
		if !seen[int(id)] {
			seen[int(id)] = true
			ids = append(ids, int(id))
		}
	}
	if len(ids) > 20 {
		fields["genre_id"] = "must be provided at most 20 times"
		return nil
	}
	sort.Ints(ids)
	return ids
}

func parseLocation(fields map[string]string, raw RawQuery) *Location {
	present := map[string]bool{
		"latitude":  raw.Latitude != "",
		"longitude": raw.Longitude != "",
		"radius_km": raw.RadiusKM != "",
	}
	if !present["latitude"] && !present["longitude"] && !present["radius_km"] {
		return nil
	}
	if !present["latitude"] || !present["longitude"] || !present["radius_km"] {
		for name, ok := range present {
			if !ok {
				fields[name] = locationGroup
			}
		}
		return nil
	}
	latitude, latitudeOK := parseCoordinate(raw.Latitude, 90)
	if !latitudeOK {
		fields["latitude"] = "must be from -90 to 90"
	}
	longitude, longitudeOK := parseCoordinate(raw.Longitude, 180)
	if !longitudeOK {
		fields["longitude"] = "must be from -180 to 180"
	}
	radius, err := strconv.Atoi(raw.RadiusKM)
	radiusOK := err == nil && radius >= 1 && radius <= 100
	if !radiusOK {
		fields["radius_km"] = "must be from 1 to 100"
	}
	if !latitudeOK || !longitudeOK || !radiusOK {
		return nil
	}
	return &Location{Latitude: latitude, Longitude: longitude, RadiusKM: radius}
}

func parseCoordinate(raw string, bound float64) (float64, bool) {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || value < -bound || value > bound {
		return 0, false
	}
	return value, true
}
