package database

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
)

func TestReviewSafetyConstraints(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	author := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	reporter := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	operator := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	venue := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1), ($2), ($3)`, author, reporter, operator); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into venues (id, name, location, address, city, country_code, venue_type)
		values ($1, 'Bar', ST_MakePoint(10, 60)::geography, '1 Street', 'Oslo', 'NO', 'bar')`, venue); err != nil {
		t.Fatal(err)
	}
	var reviewID string
	if err := pool.QueryRow(ctx, `
		insert into reviews (venue_id, user_id, rating, commentary, created_at, updated_at)
		values ($1, $2, 4, 'loud', now(), now())
		returning id`, venue, author).Scan(&reviewID); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `
		update reviews set commentary_removed_at = now() where id = $1`, reviewID); vsCode(err) != "23514" {
		t.Fatalf("commentary with removed_at: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		update reviews set commentary = null, commentary_removed_at = null where id = $1`, reviewID); err != nil {
		t.Fatalf("both null: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		update reviews set commentary = null, commentary_removed_at = now() where id = $1`, reviewID); err != nil {
		t.Fatalf("null commentary with removed_at: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		update reviews set commentary = 'loud', commentary_removed_at = null where id = $1`, reviewID); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct{ name, sql string }{
		{"unknown reason", `insert into review_reports (reporter_user_id, review_id, reason) values ($1, $2, 'nope')`},
		{"other without note", `insert into review_reports (reporter_user_id, review_id, reason) values ($1, $2, 'other')`},
		{"long note", `insert into review_reports (reporter_user_id, review_id, reason, note) values ($1, $2, 'spam', repeat('a', 1001))`},
	} {
		if _, err := pool.Exec(ctx, c.sql, reporter, reviewID); vsCode(err) != "23514" {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
	if _, err := pool.Exec(ctx, `
		insert into review_reports (reporter_user_id, review_id, reason)
		values ($1, $2, 'spam')`, reporter, reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into review_reports (reporter_user_id, review_id, reason)
		values ($1, $2, 'harassment')`, reporter, reviewID); vsCode(err) != "23505" {
		t.Fatalf("second pending: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		update review_reports set status = 'dismissed', resolved_at = now()
		where review_id = $1 and status = 'pending'`, reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into review_reports (reporter_user_id, review_id, reason)
		values ($1, $2, 'harassment')`, reporter, reviewID); err != nil {
		t.Fatalf("pending after dismiss: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		update review_reports set status = 'author_resolved', resolved_at = now(), resolved_by_user_id = $2
		where review_id = $1 and status = 'pending'`, reviewID, author); err != nil {
		t.Fatalf("author_resolved: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into review_reports (reporter_user_id, review_id, reason, status, resolved_by_user_id)
		values ($1, $2, 'spam', 'author_resolved', $3)`, reporter, reviewID, author); vsCode(err) != "23514" {
		t.Fatalf("author_resolved without resolved_at: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into review_reports (reporter_user_id, review_id, reason, status, resolved_at)
		values ($1, $2, 'spam', 'pending', now())`, reporter, reviewID); vsCode(err) != "23514" {
		t.Fatalf("pending with resolved_at: %v", err)
	}

	var commentaryCols int
	if err := pool.QueryRow(ctx, `
		select count(*) from information_schema.columns
		where table_schema = 'public'
		  and table_name in ('review_reports', 'review_moderation_actions')
		  and column_name like '%commentary%'`).Scan(&commentaryCols); err != nil || commentaryCols != 0 {
		t.Fatalf("commentary columns %d %v", commentaryCols, err)
	}

	if _, err := pool.Exec(ctx, `insert into report_quota_events (user_id) values ($1)`, reporter); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into review_reports (reporter_user_id, review_id, reason)
		values ($1, $2, 'spam')`, reporter, reviewID); err != nil {
		t.Fatal(err)
	}
	var actionID string
	if err := pool.QueryRow(ctx, `
		insert into review_moderation_actions (
			review_id, reviewer_user_id, venue_id, action, reason, operator_user_id
		) values ($1, $2, $3, 'commentary_removed', 'spam', $4)
		returning id`, reviewID, author, venue, operator).Scan(&actionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into review_commentary_restrictions (user_id, venue_id, source_action_id)
		values ($1, $2, $3)`, author, venue, actionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into review_commentary_restrictions (user_id, venue_id, source_action_id)
		values ($1, $2, 'eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee')`, reporter, venue); vsCode(err) != "23503" {
		t.Fatalf("unknown source_action_id: %v", err)
	}

	if _, err := pool.Exec(ctx, `delete from reviews where id = $1`, reviewID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `select count(*) from review_reports`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("reports after review delete %d %v", n, err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from report_quota_events where user_id = $1`, reporter).Scan(&n); err != nil || n != 1 {
		t.Fatalf("quota after review delete %d %v", n, err)
	}
	if err := pool.QueryRow(ctx, `
		select count(*) from review_moderation_actions where id = $1 and review_id = $2`, actionID, reviewID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit after review delete %d %v", n, err)
	}
	if err := pool.QueryRow(ctx, `
		select count(*) from review_commentary_restrictions
		where user_id = $1 and source_action_id = $2`, author, actionID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("restriction after review delete %d %v", n, err)
	}

	if _, err := pool.Exec(ctx, `delete from auth.users where id = $1`, reporter); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from report_quota_events where user_id = $1`, reporter).Scan(&n); err != nil || n != 0 {
		t.Fatalf("quota after reporter delete %d %v", n, err)
	}

	if _, err := pool.Exec(ctx, `delete from auth.users where id = $1`, author); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from review_commentary_restrictions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("restrictions after restricted user delete %d %v", n, err)
	}
}

func TestReviewAuditAppendOnly(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	author := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	operator := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	venue := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1), ($2)`, author, operator); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into venues (id, name, location, address, city, country_code, venue_type)
		values ($1, 'Bar', ST_MakePoint(10, 60)::geography, '1 Street', 'Oslo', 'NO', 'bar')`, venue); err != nil {
		t.Fatal(err)
	}
	var reviewID string
	if err := pool.QueryRow(ctx, `
		insert into reviews (venue_id, user_id, rating, commentary, created_at, updated_at)
		values ($1, $2, 4, 'loud', now(), now())
		returning id`, venue, author).Scan(&reviewID); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `
		insert into review_moderation_actions (
			review_id, reviewer_user_id, venue_id, action, reason
		) values ($1, $2, $3, 'commentary_removed', 'spam')`, reviewID, author, venue); vsCode(err) != "23514" {
		t.Fatalf("null operator: %v", err)
	}
	var actionID string
	if err := pool.QueryRow(ctx, `
		insert into review_moderation_actions (
			review_id, reviewer_user_id, venue_id, action, reason, operator_user_id
		) values ($1, $2, $3, 'commentary_removed', 'spam', $4)
		returning id`, reviewID, author, venue, operator).Scan(&actionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		update review_moderation_actions set reason = 'harassment' where id = $1`, actionID); vsCode(err) != "55000" {
		t.Fatalf("update reason: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		update review_moderation_actions set operator_user_id = null where id = $1`, actionID); err != nil {
		t.Fatalf("null operator_user_id: %v", err)
	}
	if _, err := pool.Exec(ctx, `delete from review_moderation_actions where id = $1`, actionID); vsCode(err) != "55000" {
		t.Fatalf("delete audit: %v", err)
	}
	if _, err := pool.Exec(ctx, `delete from reviews where id = $1`, reviewID); err != nil {
		t.Fatal(err)
	}
	var keptID string
	if err := pool.QueryRow(ctx, `
		select review_id::text from review_moderation_actions where id = $1`, actionID).Scan(&keptID); err != nil || keptID != reviewID {
		t.Fatalf("audit after review delete %q %v", keptID, err)
	}
}
