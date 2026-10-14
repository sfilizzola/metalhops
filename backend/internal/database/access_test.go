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
		`select * from venue_genres`,
		`insert into venue_genres (venue_id, genre_id) values ('00000000-0000-4000-8000-000000000001', 1)`,
		`update venue_genres set genre_id = 1 where false`,
		`delete from venue_genres where false`,
		`select * from idempotency_records`,
		`insert into idempotency_records (user_id, operation, key, request_hash, response_status, response_body, expires_at) values ('00000000-0000-4000-8000-000000000001', 'access_test', 'access-key', repeat('ab', 32), 200, '{}', now())`,
		`update idempotency_records set operation = 'access_test' where false`,
		`delete from idempotency_records where false`,
		`select nextval('genres_id_seq')`,
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
	if _, err := pool.Exec(ctx, `insert into venues (name, location) values ('Access', ST_MakePoint(0, 0)::geography)`); err != nil {
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
}
