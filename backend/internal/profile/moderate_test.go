package profile

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

func TestListReports(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 16, 11, 0, 0, 0, time.UTC)
	same := now
	later := now.Add(time.Second)
	earlier := now.Add(-time.Hour)
	until := now.Add(3 * time.Hour)
	clock := func() time.Time { return now }
	limiter := ratelimit.NewUserLimiter(clock)
	service := New(pool, limiter, nil, clock)

	repA := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa1"
	tgtA := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa2"
	repB := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbb1"
	tgtB := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbb2"
	repC := "cccccccc-cccc-4ccc-8ccc-ccccccccccc1"
	tgtC := "cccccccc-cccc-4ccc-8ccc-ccccccccccc2"
	repD := "dddddddd-dddd-4ddd-8ddd-ddddddddddd1"
	tgtD := "dddddddd-dddd-4ddd-8ddd-ddddddddddd2"
	reportA := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeee1"
	reportB := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeee2"
	reportC := "ffffffff-ffff-4fff-8fff-fffffffffff1"
	reportD := "11111111-1111-4111-8111-111111111111"

	insertProfile(t, pool, repA, "rep_one", "Reporter One", "active", nil, now)
	insertProfile(t, pool, tgtA, "tgt_one", "Target One", "suspended", &until, now)
	insertProfile(t, pool, repB, "rep_two", "Reporter Two", "active", nil, now)
	insertProfile(t, pool, tgtB, "tgt_two", "Target Two", "active", nil, now)
	insertProfile(t, pool, repC, "rep_three", "Reporter Three", "active", nil, now)
	insertProfile(t, pool, tgtC, "tgt_three", "Target Three", "active", nil, now)
	insertProfile(t, pool, repD, "rep_four", "Reporter Four", "active", nil, now)
	insertProfile(t, pool, tgtD, "tgt_four", "Target Four", "active", nil, now)
	note := "see this"
	if _, err := pool.Exec(ctx, `update profiles set moderation_note = $2 where id = $1`, tgtA, note); err != nil {
		t.Fatal(err)
	}
	reportNote := "saw the show"
	insertReport(t, pool, reportD, repD, tgtD, "spam", nil, "dismissed", earlier, &now)
	insertReport(t, pool, reportA, repA, tgtA, "spam", nil, "pending", same, nil)
	insertReport(t, pool, reportB, repB, tgtB, "abusive_name", &reportNote, "pending", same, nil)
	insertReport(t, pool, reportC, repC, tgtC, "hate_or_discrimination", nil, "pending", later, nil)

	reason := "spam"
	want := map[string]Profile{
		repA: {ID: repA, Username: "rep_one", DisplayName: "Reporter One", Status: "active", CreatedAt: now, UpdatedAt: now},
		tgtA: {ID: tgtA, Username: "tgt_one", DisplayName: "Target One", Status: "suspended", ModerationReason: &reason, ModerationNote: &note, SuspendedUntil: &until, CreatedAt: now, UpdatedAt: now},
		repB: {ID: repB, Username: "rep_two", DisplayName: "Reporter Two", Status: "active", CreatedAt: now, UpdatedAt: now},
		tgtB: {ID: tgtB, Username: "tgt_two", DisplayName: "Target Two", Status: "active", CreatedAt: now, UpdatedAt: now},
		repC: {ID: repC, Username: "rep_three", DisplayName: "Reporter Three", Status: "active", CreatedAt: now, UpdatedAt: now},
		tgtC: {ID: tgtC, Username: "tgt_three", DisplayName: "Target Three", Status: "active", CreatedAt: now, UpdatedAt: now},
	}

	page, err := service.ListReports(ctx, 2, "")
	if err != nil || len(page.Items) != 2 || page.NextCursor == nil || *page.NextCursor == "" {
		t.Fatalf("first %#v %v", page, err)
	}
	if page.Items[0].ID != reportA || page.Items[1].ID != reportB {
		t.Fatalf("order %s %s", page.Items[0].ID, page.Items[1].ID)
	}
	if page.Items[0].Reason != "spam" || page.Items[0].Note != nil || page.Items[0].Status != "pending" || !page.Items[0].CreatedAt.Equal(same) {
		t.Fatalf("first item %#v", page.Items[0])
	}
	if page.Items[1].Reason != "abusive_name" || page.Items[1].Note == nil || *page.Items[1].Note != reportNote || !page.Items[1].CreatedAt.Equal(same) {
		t.Fatalf("second item %#v", page.Items[1])
	}
	for _, item := range page.Items {
		assertFullProfile(t, item.Reporter, want[item.Reporter.ID])
		assertFullProfile(t, item.Target, want[item.Target.ID])
	}

	next, err := service.ListReports(ctx, 2, *page.NextCursor)
	if err != nil || len(next.Items) != 1 || next.NextCursor != nil || next.Items[0].ID != reportC || next.Items[0].Status != "pending" || !next.Items[0].CreatedAt.Equal(later) {
		t.Fatalf("second %#v %v", next, err)
	}
	assertFullProfile(t, next.Items[0].Reporter, want[repC])
	assertFullProfile(t, next.Items[0].Target, want[tgtC])

	all, err := service.ListReports(ctx, 100, "")
	if err != nil || len(all.Items) != 3 || all.NextCursor != nil || all.Items[0].ID != reportA || all.Items[2].ID != reportC {
		t.Fatalf("max %#v %v", all, err)
	}
	one, err := service.ListReports(ctx, 1, "")
	if err != nil || len(one.Items) != 1 || one.Items[0].ID != reportA || one.NextCursor == nil {
		t.Fatalf("limit one %#v %v", one, err)
	}

	_, err = service.ListReports(ctx, 2, "not-a-cursor")
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Fields["cursor"] == "" || len(validation.Fields) != 1 {
		t.Fatalf("cursor %v", err)
	}
	_, err = service.ListReports(ctx, 0, "")
	if !errors.As(err, &validation) || validation.Fields["limit"] != "must be from 1 to 100" {
		t.Fatalf("low limit %v", err)
	}
	_, err = service.ListReports(ctx, 101, "")
	if !errors.As(err, &validation) || validation.Fields["limit"] != "must be from 1 to 100" {
		t.Fatalf("high limit %v", err)
	}

	got, err := service.GetModerationProfile(ctx, tgtA)
	if err != nil {
		t.Fatal(err)
	}
	assertFullProfile(t, got, want[tgtA])
	_, err = service.GetModerationProfile(ctx, "99999999-9999-4999-8999-999999999999")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing %v", err)
	}
	_, err = service.GetModerationProfile(ctx, "abc")
	if !errors.As(err, &validation) || validation.Fields["user_id"] == "" || len(validation.Fields) != 1 {
		t.Fatalf("user id %v", err)
	}
	if left := tokensLeft(limiter, repA); left != 60 {
		t.Fatalf("read spent a token %d", left)
	}
}

func assertFullProfile(t *testing.T, got, want Profile) {
	t.Helper()
	if got.ID != want.ID || got.Username != want.Username || got.DisplayName != want.DisplayName || got.Status != want.Status || !sameText(got.ModerationReason, want.ModerationReason) || !sameText(got.ModerationNote, want.ModerationNote) || !sameTime(got.SuspendedUntil, want.SuspendedUntil) || !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("profile %#v want %#v", got, want)
	}
}

func sameText(got, want *string) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

func sameTime(got, want *time.Time) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return got.Equal(*want)
}

func insertReport(t *testing.T, pool *pgxpool.Pool, id, reporter, target, reason string, note *string, status string, created time.Time, resolved *time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		insert into profile_reports (
			id, reporter_user_id, target_user_id, reason, note, status, created_at, resolved_at
		) values ($1, $2, $3, $4, $5, $6, $7, $8)`, id, reporter, target, reason, note, status, created, resolved)
	if err != nil {
		t.Fatal(err)
	}
}
