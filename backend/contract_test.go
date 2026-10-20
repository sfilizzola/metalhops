package contract

import (
	"bytes"
	"testing"
)

func TestOpenAPIDocument(t *testing.T) {
	for _, want := range []string{
		"openapi: 3.1.0", "/healthz", "/readyz", "http://localhost:8080",
		"/v1/me", "type: http", "scheme: bearer",
		"username_reserved", "recent_authentication_required", "auth_provider_error",
		"/v1/profiles/{username}/reports",
		"/v1/moderation/profile-reports",
		"/v1/moderation/profiles/{userID}",
		"/v1/moderation/profiles/{userID}/actions",
		"profile_report",
		"profile_moderation_action",
		"self_report",
		"profile_already_reported",
		"profile_required",
		"profile_rename_required",
		"profile_suspended",
		"quota_exceeded",
		"report_already_resolved",
		"profile_report_not_found",
		"moderation_reason",
		"suspended_until",
		"/v1/genres",
		"/v1/venues",
		"/v1/venues/{venueID}",
		"listGenres",
		"listVenues",
		"getVenue",
		"venue_not_found",
		"verification_status",
		"country_code",
		"average_rating",
		"genre_id",
		"radius_km",
		"distance_meters",
		"must be provided with latitude, longitude, and radius_km",
		"XK",
		"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$",
	} {
		if !bytes.Contains(OpenAPI, []byte(want)) {
			t.Fatalf("missing %q", want)
		}
	}
	me := bytes.Index(OpenAPI, []byte("/v1/me"))
	if me < 0 || bytes.Contains(OpenAPI[:me], []byte("security:")) {
		t.Fatal("operational route requires security")
	}
}
