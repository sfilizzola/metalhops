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
		"/v1/me/venues",
		"/v1/me/venues/{venueID}",
		"SubmissionRequest",
		"Submission:",
		"venue_submission",
		"duplicate_venue",
		"venue_not_pending",
		"This venue can no longer be corrected.",
		"The submission limit has been reached.",
		"genre_ids",
		"/v1/venues/{venueID}/reports",
		"/v1/moderation/venue-reports",
		"/v1/moderation/venue-reports/{reportID}",
		"/v1/moderation/venues/{venueID}",
		"/v1/moderation/venues/{venueID}/actions",
		"venue_report",
		"venue_moderation_action",
		"venue_already_reported",
		"venue_report_not_found",
		"invalid_venue_transition",
		"rejection_reason",
		"delisting_reason",
		"delisted",
		"The report limit has been reached.",
		"venue_incomplete",
		"This venue needs at least one genre before it can be verified.",
		"actor_user_id",
		"resubmit",
		"/v1/venues/{venueID}/reviews",
		"/v1/venues/{venueID}/reviews/me",
		"listVenueReviews",
		"getMyReview",
		"putMyReview",
		"deleteMyReview",
		"review_not_found",
		"venue_not_verified",
		"Reviews open after this venue is verified.",
		"must be between 1 and 5",
		"Review:",
		"OwnReview:",
		"ReviewRequest:",
		"/v1/venues/{venueID}/reviews/{reviewID}/reports",
		"/v1/moderation/review-reports",
		"/v1/moderation/reviews/{reviewID}",
		"/v1/moderation/reviews/{reviewID}/actions",
		"/v1/moderation/review-reports/{reportID}",
		"review_already_reported",
		"commentary_not_public",
		"commentary_locked",
		"review_report_not_found",
		"commentary_removed",
		"UserCommentaryRestriction",
		"OperatorCommentaryRestriction",
	} {
		if !bytes.Contains(OpenAPI, []byte(want)) {
			t.Fatalf("missing %q", want)
		}
	}
	schemaBlock := func(name string) []byte {
		needle := []byte("\n    " + name + ":\n")
		start := bytes.Index(OpenAPI, needle)
		if start < 0 {
			return nil
		}
		rest := OpenAPI[start+1:]
		for i := 0; i < len(rest); {
			j := bytes.Index(rest[i:], []byte("\n    "))
			if j < 0 {
				return OpenAPI[start:]
			}
			abs := i + j
			k := abs + len("\n    ")
			if k < len(rest) && rest[k] >= 'A' && rest[k] <= 'Z' {
				return OpenAPI[start : start+1+abs]
			}
			i = abs + 1
		}
		return OpenAPI[start:]
	}
	ownBlock := schemaBlock("OwnReview")
	reviewBlock := schemaBlock("Review")
	if ownBlock == nil || reviewBlock == nil {
		t.Fatal("missing Review or OwnReview schema")
	}
	if !bytes.Contains(ownBlock, []byte("commentary_restriction")) || !bytes.Contains(ownBlock, []byte("commentary_removed")) {
		t.Fatal("OwnReview missing commentary fields")
	}
	if !bytes.Contains(reviewBlock, []byte("commentary_removed")) {
		t.Fatal("Review missing commentary_removed")
	}
	if bytes.Contains(reviewBlock, []byte("commentary_restriction")) {
		t.Fatal("public Review documents commentary_restriction")
	}
	if !bytes.Contains(OpenAPI, []byte("commentary_locked")) || !bytes.Contains(OpenAPI, []byte("UserCommentaryRestriction")) {
		t.Fatal("commentary_locked details missing UserCommentaryRestriction")
	}
	genres := bytes.Index(OpenAPI, []byte("/v1/genres"))
	if genres < 0 || bytes.Contains(OpenAPI[:genres], []byte("security:")) {
		t.Fatal("operational route requires security")
	}
	for _, op := range []string{
		"createVenueSubmission", "listMyVenues", "getMyVenue", "correctMyVenue",
		"reportVenue", "listVenueReports", "getModerationVenue", "applyVenueAction", "correctVenueAsOperator", "dismissVenueReport",
		"getMyReview", "putMyReview", "deleteMyReview",
		"reportReview", "listReviewReports", "getModerationReview", "applyReviewAction", "dismissReviewReport",
	} {
		start := bytes.Index(OpenAPI, []byte("operationId: "+op))
		if start < 0 {
			t.Fatalf("missing %s", op)
		}
		rest := OpenAPI[start+len("operationId: "+op):]
		if next := bytes.Index(rest, []byte("operationId:")); next >= 0 {
			rest = rest[:next]
		}
		if !bytes.Contains(rest, []byte("bearerAuth")) {
			t.Fatalf("%s has no bearer security", op)
		}
	}
	for _, op := range []string{"listGenres", "listVenues", "getVenue", "listVenueReviews"} {
		start := bytes.Index(OpenAPI, []byte("operationId: "+op))
		if start < 0 {
			t.Fatalf("missing %s", op)
		}
		rest := OpenAPI[start+len("operationId: "+op):]
		if next := bytes.Index(rest, []byte("operationId:")); next >= 0 {
			rest = rest[:next]
		}
		if bytes.Contains(rest, []byte("security:")) {
			t.Fatalf("%s became protected", op)
		}
	}
}
