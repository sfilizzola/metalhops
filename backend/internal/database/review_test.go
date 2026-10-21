package database

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
)

func TestReviewConstraints(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	user := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	venue := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into venues (id, name, location, address, city, country_code, venue_type)
		values ($1, 'Bar', ST_MakePoint(10, 60)::geography, '1 Street', 'Oslo', 'NO', 'bar')`, venue); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct{ name, sql string }{
		{"rating 0", `insert into reviews (venue_id, user_id, rating, created_at, updated_at) values ($1, $2, 0, now(), now())`},
		{"rating 6", `insert into reviews (venue_id, user_id, rating, created_at, updated_at) values ($1, $2, 6, now(), now())`},
		{"empty commentary", `insert into reviews (venue_id, user_id, rating, commentary, created_at, updated_at) values ($1, $2, 3, '', now(), now())`},
		{"long commentary", `insert into reviews (venue_id, user_id, rating, commentary, created_at, updated_at) values ($1, $2, 3, repeat('a', 2001), now(), now())`},
	} {
		if _, err := pool.Exec(ctx, c.sql, venue, user); vsCode(err) != "23514" {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
	if _, err := pool.Exec(ctx, `
		insert into reviews (venue_id, user_id, rating, commentary, created_at, updated_at)
		values ($1, $2, 5, repeat('a', 2000), now(), now())`, venue, user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into reviews (venue_id, user_id, rating, created_at, updated_at)
		values ($1, $2, 4, now(), now())`, venue, user); vsCode(err) != "23505" {
		t.Fatalf("second review: %v", err)
	}

	var n int
	if _, err := pool.Exec(ctx, `delete from auth.users where id = $1`, user); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from reviews`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("reviews after user delete %d %v", n, err)
	}

	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into reviews (venue_id, user_id, rating, created_at, updated_at)
		values ($1, $2, 3, now(), now())`, venue, user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `delete from venues where id = $1`, venue); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from reviews`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("reviews after venue delete %d %v", n, err)
	}
}
