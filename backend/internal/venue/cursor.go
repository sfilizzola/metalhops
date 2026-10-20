package venue

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

type position struct {
	Tier     int
	NameKey  string
	Distance float64
	ID       string
}

var discriminators = map[sortMode]string{
	modeName:              "name-v2",
	modeDistance:          "distance-v1",
	modeRelevanceName:     "relevance-name-v1",
	modeRelevanceDistance: "relevance-distance-v1",
}

func filterHash(mode sortMode, q Query, textKey, cityKey string) string {
	genres := make([]string, len(q.GenreIDs))
	for i, id := range q.GenreIDs {
		genres[i] = strconv.Itoa(id)
	}
	latitude, longitude, radius := "", "", ""
	if q.Location != nil {
		latitude = formatFloat(q.Location.Latitude)
		longitude = formatFloat(q.Location.Longitude)
		radius = strconv.Itoa(q.Location.RadiusKM)
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		discriminators[mode], textKey, cityKey, strings.Join(genres, ","),
		q.CountryCode, q.VenueType, latitude, longitude, radius,
	}, "\n")))
	return hex.EncodeToString(sum[:8])
}

func encodeCursor(mode sortMode, hash string, p position) string {
	fields := []string{discriminators[mode], hash}
	switch mode {
	case modeName:
		fields = append(fields, p.NameKey)
	case modeDistance:
		fields = append(fields, formatFloat(p.Distance))
	case modeRelevanceName:
		fields = append(fields, strconv.Itoa(p.Tier), p.NameKey)
	case modeRelevanceDistance:
		fields = append(fields, strconv.Itoa(p.Tier), formatFloat(p.Distance))
	}
	fields = append(fields, p.ID)
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(fields, "|")))
}

func decodeCursor(cursor string, mode sortMode, hash string) (position, error) {
	invalid := &ValidationError{Fields: map[string]string{"cursor": "invalid"}}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || cursor == "" {
		return position{}, invalid
	}
	parts := strings.Split(string(raw), "|")
	keys := 1
	if mode == modeRelevanceName || mode == modeRelevanceDistance {
		keys = 2
	}
	if len(parts) != 3+keys || parts[0] != discriminators[mode] || parts[1] != hash || !CanonicalVenueID(parts[len(parts)-1]) {
		return position{}, invalid
	}
	p := position{ID: parts[len(parts)-1]}
	sortKeys := parts[2 : 2+keys]
	if keys == 2 {
		if len(sortKeys[0]) != 1 || sortKeys[0][0] < '0' || sortKeys[0][0] > '4' {
			return position{}, invalid
		}
		p.Tier = int(sortKeys[0][0] - '0')
		sortKeys = sortKeys[1:]
	}
	switch mode {
	case modeName, modeRelevanceName:
		if !utf8.ValidString(sortKeys[0]) || strings.ContainsRune(sortKeys[0], 0) {
			return position{}, invalid
		}
		p.NameKey = sortKeys[0]
	default:
		distance, err := strconv.ParseFloat(sortKeys[0], 64)
		if err != nil || math.IsNaN(distance) || math.IsInf(distance, 0) || distance < 0 {
			return position{}, invalid
		}
		p.Distance = distance
	}
	return p, nil
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}
