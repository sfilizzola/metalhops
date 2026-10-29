package profile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

func TestReport(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	reporter := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	target := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	insertProfile(t, pool, reporter, "reporter", "Reporter", "active", nil, now)
	insertProfile(t, pool, target, "metalfan", "Metal Fan", "active", nil, now)
	limiter := ratelimit.NewUserLimiter(clock)
	service := New(pool, limiter, nil, clock, "")

	got, err := service.Report(ctx, ReportCommand{
		ReporterID: reporter, Username: "MetalFan", Reason: "spam", Key: "report-key",
	})
	if err != nil || got.Username != "metalfan" || got.Reason != "spam" || got.Status != "pending" || got.Note != nil || got.ID == "" || got.ID == target || got.ID == reporter {
		t.Fatalf("create %#v %v", got, err)
	}
	if strings.Contains(fmt.Sprintf("%#v", got), target) || strings.Contains(fmt.Sprintf("%#v", got), reporter) {
		t.Fatalf("user id %#v", got)
	}
	if countRows(t, pool, `select count(*) from profile_reports`) != 1 || countRows(t, pool, `select count(*) from report_quota_events where user_id = $1`, reporter) != 1 {
		t.Fatal("create counts")
	}
	var stored string
	if err := pool.QueryRow(ctx, `
		select response_body::text from idempotency_records
		where user_id = $1 and operation = 'profile_report' and key = 'report-key'`, reporter).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, target) || strings.Contains(stored, reporter) {
		t.Fatalf("stored %s", stored)
	}
	var targetName, targetStatus string
	if err := pool.QueryRow(ctx, `select username, status from profiles where id = $1`, target).Scan(&targetName, &targetStatus); err != nil || targetName != "metalfan" || targetStatus != "active" {
		t.Fatalf("target %s %s %v", targetName, targetStatus, err)
	}
	if left := tokensLeft(limiter, reporter); left != 59 {
		t.Fatalf("create tokens %d", left)
	}

	replay, err := service.Report(ctx, ReportCommand{
		ReporterID: reporter, Username: "metalfan", Reason: "spam", Key: "report-key",
	})
	if err != nil || replay.ID != got.ID || replay.Username != "metalfan" {
		t.Fatalf("replay %#v %v", replay, err)
	}
	if countRows(t, pool, `select count(*) from profile_reports`) != 1 || countRows(t, pool, `select count(*) from report_quota_events where user_id = $1`, reporter) != 1 {
		t.Fatal("replay counts")
	}
	if left := tokensLeft(limiter, reporter); left != 59 {
		t.Fatalf("replay tokens %d", left)
	}

	_, err = service.Report(ctx, ReportCommand{
		ReporterID: reporter, Username: "MetalFan", Reason: "hate_or_discrimination", Key: "report-key",
	})
	if !errors.Is(err, idempotency.ErrReused) {
		t.Fatalf("reused %v", err)
	}
	if _, err := pool.Exec(ctx, `update profiles set username = 'renamed' where id = $1`, target); err != nil {
		t.Fatal(err)
	}
	if countRows(t, pool, `select count(*) from profile_reports where reporter_user_id = $1 and target_user_id = $2 and status = 'pending'`, reporter, target) != 1 {
		t.Fatal("rename closed the report")
	}
	renamed, err := service.Report(ctx, ReportCommand{
		ReporterID: reporter, Username: "renamed", Reason: "spam", Key: "report-key",
	})
	if err != nil || renamed.ID != got.ID || renamed.Username != "metalfan" {
		t.Fatalf("replay after rename %#v %v", renamed, err)
	}
	_, err = service.Report(ctx, ReportCommand{
		ReporterID: reporter, Username: "renamed", Reason: "spam", Key: "report-key2",
	})
	if !errors.Is(err, ErrReportPending) {
		t.Fatalf("pending %v", err)
	}
	if countRows(t, pool, `select count(*) from profile_reports`) != 1 || countRows(t, pool, `select count(*) from report_quota_events where user_id = $1`, reporter) != 1 || tokensLeft(limiter, reporter) != 59 {
		t.Fatal("conflict spent a token or event")
	}

	if _, err := pool.Exec(ctx, `
		update profile_reports set status = 'dismissed', resolved_at = $2 where id = $1::uuid`, got.ID, now); err != nil {
		t.Fatal(err)
	}
	note := "  not this person \nsecond line  "
	again, err := service.Report(ctx, ReportCommand{
		ReporterID: reporter, Username: "renamed", Reason: "impersonation", Note: &note, Key: "report-key3",
	})
	if err != nil || again.ID == got.ID || again.Status != "pending" || again.Note == nil || *again.Note != "not this person \nsecond line" {
		t.Fatalf("again %#v %v", again, err)
	}
	if countRows(t, pool, `select count(*) from profile_reports where status = 'pending'`) != 1 || countRows(t, pool, `select count(*) from report_quota_events where user_id = $1`, reporter) != 2 {
		t.Fatal("resolved pair was not reported again")
	}

	_, err = service.Report(ctx, ReportCommand{
		ReporterID: reporter, Username: "reporter", Reason: "spam", Key: "report-self",
	})
	if !errors.Is(err, ErrSelfReport) {
		t.Fatalf("self %v", err)
	}
	_, err = service.Report(ctx, ReportCommand{
		ReporterID: reporter, Username: "renamed", Reason: "nope", Key: "report-bad1",
	})
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Fields["reason"] != "must be a known reason" {
		t.Fatalf("reason %v", err)
	}
	_, err = service.Report(ctx, ReportCommand{
		ReporterID: reporter, Username: "renamed", Reason: "impersonation", Key: "report-bad2",
	})
	if !errors.As(err, &validation) || validation.Fields["note"] != "required" {
		t.Fatalf("note %v", err)
	}
	_, err = service.Report(ctx, ReportCommand{
		ReporterID: reporter, Username: "nobody", Reason: "spam", Key: "report-miss",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing target %v", err)
	}
	_, err = service.Report(ctx, ReportCommand{
		ReporterID: reporter, Username: "renamed", Reason: "spam", Key: "short",
	})
	if !errors.Is(err, idempotency.ErrInvalid) {
		t.Fatalf("key %v", err)
	}
	if countRows(t, pool, `select count(*) from report_quota_events where user_id = $1`, reporter) != 2 || tokensLeft(limiter, reporter) != 58 {
		t.Fatal("rejection spent a token or event")
	}

	missing := "11111111-1111-4111-8111-111111111111"
	_, err = service.Report(ctx, ReportCommand{
		ReporterID: missing, Username: "renamed", Reason: "spam", Key: "report-key",
	})
	if !errors.Is(err, ErrProfileRequired) || tokensLeft(limiter, missing) != 60 {
		t.Fatalf("profile required %v", err)
	}

	renameID := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	insertProfile(t, pool, renameID, "needname", "Needs Name", "rename_required", nil, now)
	_, err = service.Report(ctx, ReportCommand{
		ReporterID: renameID, Username: "renamed", Reason: "spam", Key: "report-key",
	})
	if !errors.Is(err, ErrRenameRequired) || tokensLeft(limiter, renameID) != 60 {
		t.Fatalf("rename %v", err)
	}

	suspendedID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	until := now.Add(time.Hour)
	insertProfile(t, pool, suspendedID, "paused", "Paused", "suspended", &until, now)
	_, err = service.Report(ctx, ReportCommand{
		ReporterID: suspendedID, Username: "renamed", Reason: "spam", Key: "report-key",
	})
	var suspended *SuspendedError
	if !errors.As(err, &suspended) || suspended.Until == nil || !suspended.Until.Equal(until) || tokensLeft(limiter, suspendedID) != 60 {
		t.Fatalf("suspended %v", err)
	}

	expiredID := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	insertProfile(t, pool, expiredID, "expired", "Expired", "suspended", &now, now)
	expired, err := service.Report(ctx, ReportCommand{
		ReporterID: expiredID, Username: "renamed", Reason: "spam", Key: "report-key",
	})
	if err != nil || expired.Username != "renamed" || expired.Status != "pending" {
		t.Fatalf("expired %#v %v", expired, err)
	}
	var status string
	var action, actorType string
	if err := pool.QueryRow(ctx, `select status from profiles where id = $1`, expiredID).Scan(&status); err != nil || status != "active" {
		t.Fatalf("normalized %s %v", status, err)
	}
	if err := pool.QueryRow(ctx, `
		select action, actor_type from profile_moderation_actions where subject_user_id = $1`, expiredID).Scan(&action, &actorType); err != nil || action != "restored" || actorType != "system" {
		t.Fatalf("audit %s %s %v", action, actorType, err)
	}
	if tokensLeft(limiter, expiredID) != 59 || countRows(t, pool, `select count(*) from report_quota_events where user_id = $1`, expiredID) != 1 {
		t.Fatal("expired report token")
	}
}

func TestQuotaSurvivesTargetDeletion(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	reporter := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	target := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	other := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	insertProfile(t, pool, reporter, "metalfan", "Metal Fan", "active", nil, now)
	insertProfile(t, pool, target, "targetone", "Target", "active", nil, now)
	insertProfile(t, pool, other, "otherfan", "Other", "active", nil, now)
	recent := now.Add(-23 * time.Hour)
	for i := 0; i < 19; i++ {
		if _, err := pool.Exec(ctx, `insert into report_quota_events (user_id, created_at) values ($1, $2)`, reporter, recent); err != nil {
			t.Fatal(err)
		}
	}
	limiter := ratelimit.NewUserLimiter(clock)
	service := New(pool, limiter, nil, clock, "")
	created, err := service.Report(ctx, ReportCommand{
		ReporterID: reporter, Username: "targetone", Reason: "spam", Key: "report-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.Report(ctx, ReportCommand{
		ReporterID: reporter, Username: "TargetOne", Reason: "spam", Key: "report-key",
	})
	if err != nil || replay.ID != created.ID || countRows(t, pool, `select count(*) from report_quota_events where user_id = $1`, reporter) != 20 || tokensLeft(limiter, reporter) != 59 {
		t.Fatalf("replay at quota %#v %v", replay, err)
	}
	if _, err := pool.Exec(ctx, `delete from auth.users where id = $1`, target); err != nil {
		t.Fatal(err)
	}
	if countRows(t, pool, `select count(*) from profile_reports`) != 0 || countRows(t, pool, `select count(*) from report_quota_events where user_id = $1`, reporter) != 20 {
		t.Fatal("deletion changed quota")
	}
	_, err = service.Report(ctx, ReportCommand{
		ReporterID: reporter, Username: "otherfan", Reason: "spam", Key: "report-next",
	})
	var quota *QuotaError
	if !errors.As(err, &quota) || quota.RetryAfter < 1 {
		t.Fatalf("quota %v", err)
	}
	if quota.RetryAfter != 3600 {
		t.Fatalf("retry %d", quota.RetryAfter)
	}
	if countRows(t, pool, `select count(*) from report_quota_events where user_id = $1`, reporter) != 20 || tokensLeft(limiter, reporter) != 59 {
		t.Fatal("inserted event 21")
	}
}

func TestConcurrentReportQuota(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	reporter := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	first := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	second := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	insertProfile(t, pool, reporter, "metalfan", "Metal Fan", "active", nil, now)
	insertProfile(t, pool, first, "firstfan", "First", "active", nil, now)
	insertProfile(t, pool, second, "secondfan", "Second", "active", nil, now)
	for i := 0; i < 19; i++ {
		if _, err := pool.Exec(ctx, `insert into report_quota_events (user_id, created_at) values ($1, $2)`, reporter, now); err != nil {
			t.Fatal(err)
		}
	}
	limiter := ratelimit.NewUserLimiter(clock)
	service := New(pool, limiter, nil, clock, "")
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 2)
	names := []string{"firstfan", "secondfan"}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = service.Report(ctx, ReportCommand{
				ReporterID: reporter, Username: names[i], Reason: "spam", Key: fmt.Sprintf("report-%d", i),
			})
		}(i)
	}
	close(start)
	wg.Wait()
	successes, quotas := 0, 0
	for _, err := range errs {
		if err == nil {
			successes++
			continue
		}
		var quota *QuotaError
		if errors.As(err, &quota) && quota.RetryAfter >= 1 {
			quotas++
			continue
		}
		t.Fatal(err)
	}
	if successes != 1 || quotas != 1 {
		t.Fatalf("success %d quota %d", successes, quotas)
	}
	if countRows(t, pool, `select count(*) from report_quota_events where user_id = $1`, reporter) != 20 || countRows(t, pool, `select count(*) from profile_reports`) != 1 {
		t.Fatal("event count")
	}
	if tokensLeft(limiter, reporter) != 59 {
		t.Fatal("quota failure spent a token")
	}
}

func insertProfile(t *testing.T, pool *pgxpool.Pool, id, username, display, status string, until *time.Time, now time.Time) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `insert into auth.users (id) values ($1)`, id); err != nil {
		t.Fatal(err)
	}
	var reason *string
	if status != "active" {
		value := "spam"
		reason = &value
	}
	if _, err := pool.Exec(context.Background(), `
		insert into profiles (
			id, username, display_name, status, moderation_reason, suspended_until, created_at, updated_at
		) values ($1, $2, $3, $4, $5, $6, $7, $7)`, id, username, display, status, reason, until, now); err != nil {
		t.Fatal(err)
	}
}

func countRows(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func tokensLeft(limiter *ratelimit.UserLimiter, userID string) int {
	held := make([]*ratelimit.Reservation, 0, 60)
	for len(held) < 60 {
		reservation, _, ok := limiter.Reserve(userID)
		if !ok {
			break
		}
		held = append(held, reservation)
	}
	for _, reservation := range held {
		reservation.Cancel()
	}
	return len(held)
}

func TestReportQuotaHelpers(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	user := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, user); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for i := 0; i < 20; i++ {
		if err := CheckReportQuota(ctx, tx, user, now); err != nil {
			t.Fatalf("report %d: %v", i, err)
		}
		if err := RecordReportEvent(ctx, tx, user, now.Add(-time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	var quota *QuotaError
	if err := CheckReportQuota(ctx, tx, user, now); !errors.As(err, &quota) || quota.RetryAfter < 1 {
		t.Fatalf("expected quota error, got %v", err)
	}
	if err := CheckReportQuota(ctx, tx, user, now.Add(25*time.Hour)); err != nil {
		t.Fatalf("old events still counted: %v", err)
	}
}
