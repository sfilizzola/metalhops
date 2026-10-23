package database

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
)

func TestDataAPIClosed(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `grant usage on schema public to anon, authenticated, service_role`); err != nil {
		t.Fatal(err)
	}

	statements := []string{
		`select * from genres`,
		`insert into genres (name) values ('nope')`,
		`update genres set name = 'nope' where false`,
		`delete from genres where false`,
		`select * from venues`,
		`insert into venues (name, location) values ('nope', ST_MakePoint(0, 0)::geography)`,
		`update venues set name = 'nope' where false`,
		`delete from venues where false`,
		`select venue_name_key('Bar!')`,
		`select venue_search_key('Bar!')`,
		`select * from venue_genres`,
		`insert into venue_genres (venue_id, genre_id) values ('00000000-0000-4000-8000-000000000001', 1)`,
		`update venue_genres set genre_id = 1 where false`,
		`delete from venue_genres where false`,
		`select * from idempotency_records`,
		`insert into idempotency_records (user_id, operation, key, request_hash, response_status, response_body, expires_at) values ('00000000-0000-4000-8000-000000000001', 'access_test', 'access-key', repeat('ab', 32), 200, '{}', now())`,
		`update idempotency_records set operation = 'access_test' where false`,
		`delete from idempotency_records where false`,
		`select nextval('genres_id_seq')`,
		`select * from profiles`,
		`insert into profiles (id, username, display_name) values (
		  '00000000-0000-4000-8000-000000000001', 'blocked', 'Blocked'
		)`,
		`update profiles set display_name = 'Blocked' where false`,
		`delete from profiles where false`,
		`select * from platform_operators`,
		`insert into platform_operators (user_id) values ('00000000-0000-4000-8000-000000000001')`,
		`update platform_operators set created_at = now() where false`,
		`delete from platform_operators where false`,
		`select * from profile_reports`,
		`insert into profile_reports (reporter_user_id, target_user_id, reason) values ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000002', 'spam')`,
		`update profile_reports set status = 'pending' where false`,
		`delete from profile_reports where false`,
		`select * from venue_reports`,
		`insert into venue_reports (reporter_user_id, venue_id, reason) values ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000002', 'moved')`,
		`update venue_reports set status = 'pending' where false`,
		`delete from venue_reports where false`,
		`select * from venue_moderation_actions`,
		`insert into venue_moderation_actions (venue_id, action, prior_status, new_status, actor_user_id) values ('00000000-0000-4000-8000-000000000002', 'verify', 'pending', 'verified', '00000000-0000-4000-8000-000000000001')`,
		`update venue_moderation_actions set action = 'verify' where false`,
		`delete from venue_moderation_actions where false`,
		`select * from report_quota_events`,
		`insert into report_quota_events (user_id) values ('00000000-0000-4000-8000-000000000001')`,
		`update report_quota_events set created_at = now() where false`,
		`delete from report_quota_events where false`,
		`select * from profile_moderation_actions`,
		`insert into profile_moderation_actions (action, actor_type, reason) values ('suspended', 'system', 'spam')`,
		`update profile_moderation_actions set reason = 'spam' where false`,
		`delete from profile_moderation_actions where false`,
		`select * from reviews`,
		`insert into reviews (venue_id, user_id, rating, created_at, updated_at) values ('00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000001', 3, now(), now())`,
		`update reviews set rating = 3 where false`,
		`delete from reviews where false`,
		`select * from review_reports`,
		`insert into review_reports (reporter_user_id, review_id, reason) values ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000002', 'spam')`,
		`update review_reports set status = 'pending' where false`,
		`delete from review_reports where false`,
		`select * from review_moderation_actions`,
		`insert into review_moderation_actions (review_id, venue_id, action, reason, operator_user_id) values ('00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000003', 'commentary_removed', 'spam', '00000000-0000-4000-8000-000000000001')`,
		`update review_moderation_actions set reason = 'spam' where false`,
		`delete from review_moderation_actions where false`,
		`select * from review_commentary_restrictions`,
		`insert into review_commentary_restrictions (user_id, venue_id, source_action_id) values ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000003', '00000000-0000-4000-8000-000000000002')`,
		`update review_commentary_restrictions set created_at = now() where false`,
		`delete from review_commentary_restrictions where false`,
	}
	for _, table := range []string{"reviews", "review_reports", "review_moderation_actions", "review_commentary_restrictions"} {
		var rls bool
		if err := pool.QueryRow(ctx, `select relrowsecurity from pg_class where relname = $1`, table).Scan(&rls); err != nil || !rls {
			t.Fatalf("%s rls %v %v", table, rls, err)
		}
	}
	for _, role := range []string{"anon", "authenticated", "service_role"} {
		for _, sql := range statements {
			conn, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			func() {
				defer func() {
					_, _ = conn.Exec(ctx, "reset role")
					conn.Release()
				}()
				if _, err := conn.Exec(ctx, "set role "+role); err != nil {
					t.Fatal(err)
				}
				_, err = conn.Exec(ctx, sql)
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
					t.Fatalf("%s %s err %v", role, sql, err)
				}
			}()
		}
	}

	if _, err := pool.Exec(ctx, `insert into genres (name) values ('Access test')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into venues (name, location, address, city, country_code, submitted_by)
		values ('Access', ST_MakePoint(0, 0)::geography, '1 Metal Street', 'Oslo', 'NO', 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into idempotency_records (
			user_id, operation, key, request_hash, response_status, response_body, expires_at
		) values (
			'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'access_test', 'access-key',
			repeat('ab', 32), 200, '{"ok":true}', now() + interval '1 hour'
		)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into profiles (id, username, display_name)
		values ('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'accessuser', 'Access User')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `delete from auth.users where id = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `select count(*) from profiles`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("profiles %d %v", n, err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from idempotency_records`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("idempotency %d %v", n, err)
	}
	var venues int
	var submittedBy *string
	if err := pool.QueryRow(ctx, `select count(*), max(submitted_by::text) from venues`).Scan(&venues, &submittedBy); err != nil || venues != 1 || submittedBy != nil {
		t.Fatalf("venues %d submitted_by %v err %v", venues, submittedBy, err)
	}
}
