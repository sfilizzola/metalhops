package profile

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/idempotency"
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
	service := New(pool, limiter, nil, clock, "")

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

func sameTime(got, want *time.Time) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return got.Equal(*want)
}

func TestModerationReplayIgnoresCurrentMatch(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 16, 14, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	limiter := ratelimit.NewUserLimiter(clock)
	service := New(pool, limiter, nil, clock, "")
	actor := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa10"
	target := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbb10"
	insertActor(t, pool, actor)
	insertProfile(t, pool, target, "tgt_replay", "Replay Target", "active", nil, now)
	until := now.Add(2 * time.Hour)
	cmd := ModerationCommand{
		ActorID: actor, TargetID: target, Action: "suspended",
		Reason: "spam", ReasonSet: true,
		SuspendedUntil: &until, UntilSet: true,
		Key: "replay-key",
	}
	first, err := service.Moderate(ctx, cmd)
	if err != nil || !first.Created || first.ActionID == nil || *first.ActionID == "" || first.Profile.Status != "suspended" || !sameTime(first.Profile.SuspendedUntil, &until) {
		t.Fatalf("first %#v %v", first, err)
	}
	second, err := service.Moderate(ctx, cmd)
	if err != nil || !second.Created || second.ActionID == nil || *second.ActionID != *first.ActionID {
		t.Fatalf("replay %#v %v", second, err)
	}
	if actions := countRows(t, pool, `select count(*) from profile_moderation_actions where subject_user_id = $1`, target); actions != 1 {
		t.Fatalf("audit rows %d", actions)
	}
	if records := countRows(t, pool, `select count(*) from idempotency_records where user_id = $1 and operation = 'profile_moderation_action'`, actor); records != 1 {
		t.Fatalf("idempotency rows %d", records)
	}
	if tokensLeft(limiter, actor) != 59 {
		t.Fatal("replay spent a token")
	}

	noop := cmd
	noop.Key = "noop-key"
	skipped, err := service.Moderate(ctx, noop)
	if err != nil || skipped.Created || skipped.ActionID != nil || skipped.Profile.Status != "suspended" {
		t.Fatalf("noop %#v %v", skipped, err)
	}
	if actions := countRows(t, pool, `select count(*) from profile_moderation_actions where subject_user_id = $1`, target); actions != 1 {
		t.Fatalf("noop audit rows %d", actions)
	}
	if records := countRows(t, pool, `select count(*) from idempotency_records where user_id = $1 and operation = 'profile_moderation_action' and key = 'noop-key'`, actor); records != 0 {
		t.Fatalf("noop stored %d", records)
	}
	if tokensLeft(limiter, actor) != 59 {
		t.Fatal("noop spent a token")
	}
	if _, err := pool.Exec(ctx, `
		update profiles
		set status = 'active', moderation_reason = null, moderation_note = null, suspended_until = null
		where id = $1`, target); err != nil {
		t.Fatal(err)
	}
	later, err := service.Moderate(ctx, noop)
	if err != nil || !later.Created || later.ActionID == nil || *later.ActionID == *first.ActionID || later.Profile.Status != "suspended" {
		t.Fatalf("later %#v %v", later, err)
	}
	if actions := countRows(t, pool, `select count(*) from profile_moderation_actions where subject_user_id = $1`, target); actions != 2 {
		t.Fatalf("later audit rows %d", actions)
	}
	if records := countRows(t, pool, `select count(*) from idempotency_records where user_id = $1 and operation = 'profile_moderation_action' and key = 'noop-key'`, actor); records != 1 {
		t.Fatalf("later idempotency rows %d", records)
	}
}

func TestModerationReplayAfterSuspensionExpiry(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 16, 18, 0, 0, 0, time.UTC)
	current := now
	clock := func() time.Time { return current }
	service := New(pool, ratelimit.NewUserLimiter(clock), nil, clock, "")
	actor := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa17"
	target := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbb17"
	insertActor(t, pool, actor)
	insertProfile(t, pool, target, "tgt_expire", "Expiry Target", "active", nil, now)
	until := now.Add(time.Hour)
	cmd := ModerationCommand{
		ActorID: actor, TargetID: target, Action: "suspended",
		Reason: "spam", ReasonSet: true,
		SuspendedUntil: &until, UntilSet: true,
		Key: "short-suspend",
	}
	first, err := service.Moderate(ctx, cmd)
	if err != nil || !first.Created || first.ActionID == nil {
		t.Fatalf("first %#v %v", first, err)
	}
	current = until.Add(time.Minute)
	second, err := service.Moderate(ctx, cmd)
	if err != nil || !second.Created || second.ActionID == nil || *second.ActionID != *first.ActionID {
		t.Fatalf("replay %#v %v", second, err)
	}
	if actions := countRows(t, pool, `select count(*) from profile_moderation_actions where subject_user_id = $1`, target); actions != 1 {
		t.Fatalf("audit rows %d", actions)
	}
	fresh := cmd
	fresh.Key = "past-expiry"
	_, err = service.Moderate(ctx, fresh)
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Fields["suspended_until"] == "" {
		t.Fatalf("new key %v", err)
	}
	if actions := countRows(t, pool, `select count(*) from profile_moderation_actions where subject_user_id = $1`, target); actions != 1 {
		t.Fatalf("past key audit rows %d", actions)
	}
	if records := countRows(t, pool, `select count(*) from idempotency_records where user_id = $1 and operation = 'profile_moderation_action' and key = 'past-expiry'`, actor); records != 0 {
		t.Fatalf("past key stored %d", records)
	}
}

func TestModerationTransitions(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 16, 14, 30, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	service := New(pool, ratelimit.NewUserLimiter(clock), nil, clock, "")
	actor := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa11"
	target := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbb11"
	insertActor(t, pool, actor)
	insertProfile(t, pool, target, "tgt_swap", "Swap Target", "active", nil, now)

	renamed, err := service.Moderate(ctx, ModerationCommand{
		ActorID: actor, TargetID: target, Action: "rename_required",
		Reason: "abusive_name", ReasonSet: true, Key: "rename-key-1",
	})
	if err != nil || !renamed.Created || renamed.Profile.Status != "rename_required" || !sameText(renamed.Profile.ModerationReason, strPtr("abusive_name")) || renamed.Profile.SuspendedUntil != nil {
		t.Fatalf("rename %#v %v", renamed, err)
	}
	until := now.Add(3 * time.Hour)
	suspended, err := service.Moderate(ctx, ModerationCommand{
		ActorID: actor, TargetID: target, Action: "suspended",
		Reason: "spam", ReasonSet: true, SuspendedUntil: &until, UntilSet: true, Key: "suspend-key-1",
	})
	if err != nil || !suspended.Created || suspended.Profile.Status != "suspended" || !sameText(suspended.Profile.ModerationReason, strPtr("spam")) || !sameTime(suspended.Profile.SuspendedUntil, &until) {
		t.Fatalf("suspend %#v %v", suspended, err)
	}
	back, err := service.Moderate(ctx, ModerationCommand{
		ActorID: actor, TargetID: target, Action: "rename_required",
		Reason: "hate_or_discrimination", ReasonSet: true, UntilSet: true, Key: "rename-key-2",
	})
	if err != nil || !back.Created || back.Profile.Status != "rename_required" || !sameText(back.Profile.ModerationReason, strPtr("hate_or_discrimination")) || back.Profile.SuspendedUntil != nil {
		t.Fatalf("rename again %#v %v", back, err)
	}
	open, err := service.Moderate(ctx, ModerationCommand{
		ActorID: actor, TargetID: target, Action: "suspended",
		Reason: "spam", ReasonSet: true, Key: "suspend-open",
	})
	if err != nil || !open.Created || open.Profile.Status != "suspended" || open.Profile.SuspendedUntil != nil {
		t.Fatalf("open suspension %#v %v", open, err)
	}

	past := now.Add(-time.Second)
	_, err = service.Moderate(ctx, ModerationCommand{
		ActorID: actor, TargetID: target, Action: "suspended",
		Reason: "spam", ReasonSet: true, SuspendedUntil: &past, UntilSet: true, Key: "suspend-past",
	})
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Fields["suspended_until"] == "" {
		t.Fatalf("past expiry %v", err)
	}
	future := now.Add(time.Hour)
	_, err = service.Moderate(ctx, ModerationCommand{
		ActorID: actor, TargetID: target, Action: "rename_required",
		Reason: "spam", ReasonSet: true, SuspendedUntil: &future, UntilSet: true, Key: "rename-until",
	})
	if !errors.As(err, &validation) || validation.Fields["suspended_until"] == "" {
		t.Fatalf("rename expiry %v", err)
	}
	_, err = service.Moderate(ctx, ModerationCommand{
		ActorID: actor, TargetID: target, Action: "suspended",
		Reason: "other", ReasonSet: true, Key: "other-empty",
	})
	if !errors.As(err, &validation) || validation.Fields["note"] == "" {
		t.Fatalf("other note %v", err)
	}
	spaced := "  kept  "
	noted, err := service.Moderate(ctx, ModerationCommand{
		ActorID: actor, TargetID: target, Action: "suspended",
		Reason: "other", ReasonSet: true, Note: &spaced, NoteSet: true, Key: "other-note",
	})
	if err != nil || !noted.Created || !sameText(noted.Profile.ModerationNote, strPtr("kept")) || !sameText(noted.Profile.ModerationReason, strPtr("other")) {
		t.Fatalf("normalized note %#v %v", noted, err)
	}
	_, err = service.Moderate(ctx, ModerationCommand{
		ActorID: actor, TargetID: "abc", Action: "suspended",
		Reason: "spam", ReasonSet: true, Key: "bad-user-id",
	})
	if !errors.As(err, &validation) || validation.Fields["user_id"] == "" || len(validation.Fields) != 1 {
		t.Fatalf("user id %v", err)
	}
	_, err = service.Moderate(ctx, ModerationCommand{
		ActorID: actor, TargetID: "99999999-9999-4999-8999-999999999999", Action: "suspended",
		Reason: "spam", ReasonSet: true, Key: "missing-user",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing %v", err)
	}
}

func TestModerationRestore(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 16, 15, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	service := New(pool, ratelimit.NewUserLimiter(clock), nil, clock, "")
	actor := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa12"
	target := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbb12"
	insertActor(t, pool, actor)
	insertProfile(t, pool, target, "tgt_restore", "Restore Target", "suspended", nil, now)
	note := "copied note"
	if _, err := pool.Exec(ctx, `update profiles set moderation_note = $2 where id = $1`, target, note); err != nil {
		t.Fatal(err)
	}
	reason := "spam"
	until := now.Add(time.Hour)
	for _, extra := range []ModerationCommand{
		{Reason: reason, ReasonSet: true},
		{Note: &note, NoteSet: true},
		{SuspendedUntil: &until, UntilSet: true},
	} {
		extra.ActorID, extra.TargetID, extra.Action, extra.Key = actor, target, "restored", "restore-invalid"
		_, err := service.Moderate(ctx, extra)
		var validation *ValidationError
		if !errors.As(err, &validation) || len(validation.Fields) == 0 {
			t.Fatalf("restore fields %#v %v", extra, err)
		}
	}
	restored, err := service.Moderate(ctx, ModerationCommand{
		ActorID: actor, TargetID: target, Action: "restored", Key: "restore-key",
	})
	if err != nil || !restored.Created || restored.Profile.Status != "active" || restored.Profile.ModerationReason != nil || restored.Profile.ModerationNote != nil || restored.Profile.SuspendedUntil != nil {
		t.Fatalf("restore %#v %v", restored, err)
	}
	var action, actorType, actorID string
	var storedReason, storedNote *string
	var storedUntil *time.Time
	if err := pool.QueryRow(ctx, `
		select action, actor_type, actor_user_id::text, reason, note, suspended_until
		from profile_moderation_actions
		where subject_user_id = $1`, target).Scan(&action, &actorType, &actorID, &storedReason, &storedNote, &storedUntil); err != nil {
		t.Fatal(err)
	}
	if action != "restored" || actorType != "operator" || actorID != actor || !sameText(storedReason, &reason) || !sameText(storedNote, &note) || storedUntil != nil {
		t.Fatalf("audit %s %s %s %v %v %v", action, actorType, actorID, storedReason, storedNote, storedUntil)
	}
}

func TestModerationActionsPendingReports(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 16, 15, 30, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	limiter := ratelimit.NewUserLimiter(clock)
	service := New(pool, limiter, nil, clock, "")
	actor := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa13"
	target := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbb13"
	other := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbb14"
	repA := "cccccccc-cccc-4ccc-8ccc-cccccccccc13"
	repB := "cccccccc-cccc-4ccc-8ccc-cccccccccc14"
	repC := "cccccccc-cccc-4ccc-8ccc-cccccccccc15"
	reportA := "dddddddd-dddd-4ddd-8ddd-dddddddddd13"
	reportB := "dddddddd-dddd-4ddd-8ddd-dddddddddd14"
	reportC := "dddddddd-dddd-4ddd-8ddd-dddddddddd15"
	reportD := "dddddddd-dddd-4ddd-8ddd-dddddddddd16"
	insertActor(t, pool, actor)
	insertProfile(t, pool, target, "tgt_queue", "Queue Target", "active", nil, now)
	insertProfile(t, pool, other, "tgt_other", "Other Target", "active", nil, now)
	insertProfile(t, pool, repA, "rep_queue_a", "Reporter A", "active", nil, now)
	insertProfile(t, pool, repB, "rep_queue_b", "Reporter B", "active", nil, now)
	insertProfile(t, pool, repC, "rep_queue_c", "Reporter C", "active", nil, now)
	insertReport(t, pool, reportA, repA, target, "spam", nil, "pending", now, nil)
	insertReport(t, pool, reportC, repC, target, "spam", nil, "dismissed", now.Add(-time.Hour), &now)
	insertReport(t, pool, reportD, repA, other, "spam", nil, "pending", now, nil)
	page, err := service.ListReports(ctx, 20, "")
	if err != nil || !reportListed(page, reportA) || reportListed(page, reportB) {
		t.Fatalf("list %#v %v", page, err)
	}
	insertReport(t, pool, reportB, repB, target, "abusive_name", nil, "pending", now.Add(time.Second), nil)
	result, err := service.Moderate(ctx, ModerationCommand{
		ActorID: actor, TargetID: target, Action: "rename_required",
		Reason: "spam", ReasonSet: true, Key: "queue-key",
	})
	if err != nil || !result.Created || result.Profile.Status != "rename_required" {
		t.Fatalf("action %#v %v", result, err)
	}
	assertReportResolution(t, pool, reportA, "actioned", actor, now)
	assertReportResolution(t, pool, reportB, "actioned", actor, now)
	assertReportResolution(t, pool, reportC, "dismissed", "", now)
	var otherStatus string
	if err := pool.QueryRow(ctx, `select status from profile_reports where id = $1`, reportD).Scan(&otherStatus); err != nil || otherStatus != "pending" {
		t.Fatalf("other report %s %v", otherStatus, err)
	}
	if tokensLeft(limiter, actor) != 59 {
		t.Fatal("action token")
	}
}

func TestModerationReusedKey(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 16, 16, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	service := New(pool, ratelimit.NewUserLimiter(clock), nil, clock, "")
	actor := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa15"
	target := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbb15"
	insertActor(t, pool, actor)
	insertProfile(t, pool, target, "tgt_reuse", "Reuse Target", "active", nil, now)
	cmd := ModerationCommand{
		ActorID: actor, TargetID: target, Action: "suspended",
		Reason: "spam", ReasonSet: true, Key: "reuse-key1",
	}
	if _, err := service.Moderate(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	cmd.Reason = "abusive_name"
	_, err := service.Moderate(ctx, cmd)
	if !errors.Is(err, idempotency.ErrReused) {
		t.Fatalf("reused %v", err)
	}
	if actions := countRows(t, pool, `select count(*) from profile_moderation_actions where subject_user_id = $1`, target); actions != 1 {
		t.Fatalf("audit rows %d", actions)
	}
	var reason string
	if err := pool.QueryRow(ctx, `select moderation_reason from profiles where id = $1`, target).Scan(&reason); err != nil || reason != "spam" {
		t.Fatalf("reason %s %v", reason, err)
	}
}

func TestDismissReport(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 16, 16, 30, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	limiter := ratelimit.NewUserLimiter(clock)
	service := New(pool, limiter, nil, clock, "")
	actor := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa16"
	target := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbb16"
	reporter := "cccccccc-cccc-4ccc-8ccc-cccccccccc16"
	pendingID := "dddddddd-dddd-4ddd-8ddd-dddddddddd17"
	actionedID := "dddddddd-dddd-4ddd-8ddd-dddddddddd18"
	insertActor(t, pool, actor)
	insertProfile(t, pool, target, "tgt_dismiss", "Dismiss Target", "active", nil, now)
	insertProfile(t, pool, reporter, "rep_dismiss", "Dismiss Reporter", "active", nil, now)
	insertReport(t, pool, pendingID, reporter, target, "spam", nil, "pending", now, nil)
	insertReport(t, pool, actionedID, reporter, target, "abusive_name", nil, "actioned", now.Add(-time.Minute), &now)
	var updatedBefore time.Time
	if err := pool.QueryRow(ctx, `select updated_at from profiles where id = $1`, target).Scan(&updatedBefore); err != nil {
		t.Fatal(err)
	}
	first, err := service.DismissReport(ctx, actor, pendingID)
	if err != nil || first.ID != pendingID || first.Status != "dismissed" || first.Reporter.ID != reporter || first.Target.ID != target || first.Target.Status != "active" {
		t.Fatalf("dismiss %#v %v", first, err)
	}
	assertReportResolution(t, pool, pendingID, "dismissed", actor, now)
	var updatedAfter time.Time
	var status string
	if err := pool.QueryRow(ctx, `select status, updated_at from profiles where id = $1`, target).Scan(&status, &updatedAfter); err != nil || status != "active" || !updatedAfter.Equal(updatedBefore) {
		t.Fatalf("profile changed %s %v", status, err)
	}
	if tokensLeft(limiter, actor) != 59 || countRows(t, pool, `select count(*) from report_quota_events where user_id = $1`, reporter) != 0 {
		t.Fatal("dismiss token or quota")
	}
	second, err := service.DismissReport(ctx, actor, pendingID)
	if err != nil || second.ID != first.ID || second.Status != first.Status || second.Reason != first.Reason || !second.CreatedAt.Equal(first.CreatedAt) || second.Reporter.ID != first.Reporter.ID || second.Target.ID != first.Target.ID {
		t.Fatalf("repeat %#v %v", second, err)
	}
	if tokensLeft(limiter, actor) != 59 {
		t.Fatal("repeat spent a token")
	}
	_, err = service.DismissReport(ctx, actor, actionedID)
	if !errors.Is(err, ErrReportResolved) {
		t.Fatalf("actioned %v", err)
	}
	_, err = service.DismissReport(ctx, actor, "99999999-9999-4999-8999-999999999999")
	if !errors.Is(err, ErrReportNotFound) {
		t.Fatalf("missing %v", err)
	}
	_, err = service.DismissReport(ctx, actor, "abc")
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Fields["report_id"] == "" || len(validation.Fields) != 1 {
		t.Fatalf("report id %v", err)
	}
	if tokensLeft(limiter, actor) != 59 {
		t.Fatal("failed dismiss spent a token")
	}
}

func insertActor(t *testing.T, pool *pgxpool.Pool, id string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `insert into auth.users (id) values ($1)`, id); err != nil {
		t.Fatal(err)
	}
}

func strPtr(value string) *string { return &value }

func reportListed(page ReportPage, id string) bool {
	for _, item := range page.Items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func assertReportResolution(t *testing.T, pool *pgxpool.Pool, id, status, actor string, resolved time.Time) {
	t.Helper()
	var gotStatus string
	var gotActor *string
	var gotResolved *time.Time
	if err := pool.QueryRow(context.Background(), `
		select status, resolved_at, resolved_by_user_id::text
		from profile_reports where id = $1`, id).Scan(&gotStatus, &gotResolved, &gotActor); err != nil {
		t.Fatal(err)
	}
	if gotStatus != status || !sameTime(gotResolved, &resolved) {
		t.Fatalf("report %s status %s resolved %v", id, gotStatus, gotResolved)
	}
	if actor == "" {
		if gotActor != nil {
			t.Fatalf("report %s actor %v", id, gotActor)
		}
		return
	}
	if gotActor == nil || *gotActor != actor {
		t.Fatalf("report %s actor %v", id, gotActor)
	}
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
