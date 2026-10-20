package venue

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

func TestOwnCursorRoundTrip(t *testing.T) {
	updated := time.Date(2020, 1, 15, 12, 0, 0, 123456000, time.UTC)
	cursor := encodeOwnCursor(updated, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	gotTime, gotID, err := decodeOwnCursor(cursor)
	if err != nil || !gotTime.Equal(updated) || gotID != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" {
		t.Fatalf("%s %s %v", gotTime, gotID, err)
	}
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for _, bad := range []string{
		"",
		"!!!",
		enc("submission-v1|2020-01-15T12:00:00Z"),
		enc("submission-v2|2020-01-15T12:00:00Z|aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		enc("submission-v1|yesterday|aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		enc("submission-v1|2020-01-15T12:00:00Z|AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"),
		encodeCursor(modeName, "0123456789abcdef", position{NameKey: "x", ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}),
	} {
		_, _, err := decodeOwnCursor(bad)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Fields["cursor"] != "invalid" {
			t.Fatalf("%q err %v", bad, err)
		}
	}
}

func TestListAndGetOwn(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	insertUser(t, h.pool, userA, "rename_required", nil)
	insertUser(t, h.pool, userB, "active", nil)
	tie := submitNow.Add(-2 * time.Hour)
	seedVenue(t, h.pool, seed{id: vid(1), name: "Oldest", status: "rejected", submitter: ptr(userA), updated: submitNow.Add(-5 * time.Hour)})
	seedVenue(t, h.pool, seed{id: vid(2), name: "Tie Low", status: "pending", submitter: ptr(userA), updated: tie})
	seedVenue(t, h.pool, seed{id: vid(3), name: "Tie High", status: "verified", submitter: ptr(userA), updated: tie})
	seedVenue(t, h.pool, seed{id: vid(4), name: "Newest", status: "pending", submitter: ptr(userA), updated: submitNow})
	seedVenue(t, h.pool, seed{id: vid(5), name: "Other", status: "pending", submitter: ptr(userB), updated: submitNow})
	seedVenue(t, h.pool, seed{id: vid(6), name: "Orphan", status: "pending", updated: submitNow})

	var seen []string
	cursor := ""
	for {
		page, err := h.service.ListOwn(ctx, OwnQuery{UserID: userA, Limit: 1, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, got := range page.Submissions {
			seen = append(seen, got.ID)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	want := []string{vid(4), vid(3), vid(2), vid(1)}
	if len(seen) != len(want) {
		t.Fatalf("seen %v", seen)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("order %v", seen)
		}
	}

	full, err := h.service.ListOwn(ctx, OwnQuery{UserID: userA, Limit: 20})
	if err != nil || len(full.Submissions) != 4 || full.NextCursor != nil {
		t.Fatalf("full %+v %v", full, err)
	}
	first := full.Submissions[0]
	if first.CreatedAt.IsZero() || !first.UpdatedAt.Equal(submitNow) || len(first.Genres) != 1 || first.Genres[0].Name != "Punk" {
		t.Fatalf("document %+v", first)
	}

	none, err := h.service.ListOwn(ctx, OwnQuery{UserID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", Limit: 20})
	if err != nil || len(none.Submissions) != 0 {
		t.Fatalf("no profile %+v %v", none, err)
	}
	var validation *ValidationError
	if _, err := h.service.ListOwn(ctx, OwnQuery{UserID: userA, Limit: 0}); !errors.As(err, &validation) || validation.Fields["limit"] == "" {
		t.Fatalf("limit %v", err)
	}
	if _, err := h.service.ListOwn(ctx, OwnQuery{UserID: userA, Limit: 20, Cursor: "abc"}); !errors.As(err, &validation) || validation.Fields["cursor"] != "invalid" {
		t.Fatalf("cursor %v", err)
	}

	rejected, err := h.service.GetOwn(ctx, userA, vid(1))
	if err != nil || rejected.VerificationStatus != "rejected" {
		t.Fatalf("rejected %+v %v", rejected, err)
	}
	for _, id := range []string{vid(5), vid(6), vid(99)} {
		if _, err := h.service.GetOwn(ctx, userA, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s err %v", id, err)
		}
	}
	if _, err := h.service.GetOwn(ctx, userA, "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"); !errors.As(err, &validation) || validation.Fields["venue_id"] != "must be a canonical uuid" {
		t.Fatalf("id %v", err)
	}
}
