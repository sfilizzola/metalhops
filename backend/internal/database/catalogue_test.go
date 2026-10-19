package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
)

func TestVenueCatalogueConstraints(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	_, err = pool.Exec(ctx, `
		insert into venues (name, location, address, city)
		values ('Rocker', ST_MakePoint(10.75, 59.91)::geography, '1 Metal Street', 'Oslo')`)
	if err == nil {
		t.Fatal("missing country_code accepted")
	}
	for _, code := range []string{"de", "UK", "ZZ"} {
		_, err = pool.Exec(ctx, `
			insert into venues (name, location, address, city, country_code)
			values ('Rocker', ST_MakePoint(10.75, 59.91)::geography, '1 Metal Street', 'Oslo', $1)`, code)
		if err == nil {
			t.Fatalf("country %s accepted", code)
		}
	}
	_, err = pool.Exec(ctx, `
		insert into venues (name, location, address, city, country_code)
		values ('Kosovo Rock', ST_MakePoint(21.16, 42.66)::geography, '1 Metal Street', 'Pristina', 'XK')`)
	if err != nil {
		t.Fatalf("country XK rejected: %v", err)
	}
	_, err = pool.Exec(ctx, `
		insert into venues (name, location, address, city, country_code)
		values (' Rocker', ST_MakePoint(10.75, 59.91)::geography, '1 Metal Street', 'Oslo', 'DE')`)
	if err == nil {
		t.Fatal("untrimmed name accepted")
	}
	_, err = pool.Exec(ctx, `
		insert into venues (name, location, address, city, country_code)
		values (E'Rock\ner', ST_MakePoint(10.75, 59.91)::geography, '1 Metal Street', 'Oslo', 'DE')`)
	if err == nil {
		t.Fatal("name line break accepted")
	}

	var id string
	err = pool.QueryRow(ctx, `
		insert into venues (name, description, location, address, city, country_code)
		values ('Rocker', E'line\nbreak', ST_MakePoint(10.75, 59.91)::geography, '1 Metal Street', 'Oslo', 'DE')
		returning id::text`).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	var genres int
	if err = pool.QueryRow(ctx, `select count(*) from venue_genres where venue_id = $1`, id).Scan(&genres); err != nil || genres != 0 {
		t.Fatalf("genre-less venue %d %v", genres, err)
	}
}

func TestVenueNameKey(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	cases := map[string]string{
		"Bar!":        "bar",
		"bar":         "bar",
		"The Bar":     "the bar",
		"Röcker":      "rocker",
		"O'Connor":    "oconnor",
		"black-metal": "blackmetal",
		"C++":         "c",
		"Бар №1":      "бар no1",
		"東京":          "東京",
		"bar  one":    "bar one",
		"!!!":         "",
	}
	for input, want := range cases {
		var got string
		if err := pool.QueryRow(ctx, `select venue_name_key($1)`, input).Scan(&got); err != nil || got != want {
			t.Fatalf("%q got %q want %q err %v", input, got, want, err)
		}
	}
}

func TestUpdatedAtTriggerOverwritesSuppliedTime(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var id string
	var created time.Time
	if err := pool.QueryRow(ctx, `
		insert into venues (name, location, address, city, country_code)
		values ('Rocker', ST_MakePoint(10.75, 59.91)::geography, '1 Metal Street', 'Oslo', 'DE')
		returning id::text, updated_at`).Scan(&id, &created); err != nil {
		t.Fatal(err)
	}
	var punk int
	if err := pool.QueryRow(ctx, `select id from genres where name = 'Punk'`).Scan(&punk); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into venue_genres (venue_id, genre_id) values ($1, $2)`, id, punk); err != nil {
		t.Fatal(err)
	}
	var afterGenre time.Time
	if err := pool.QueryRow(ctx, `select updated_at from venues where id = $1`, id).Scan(&afterGenre); err != nil {
		t.Fatal(err)
	}
	if !afterGenre.Equal(created) {
		t.Fatalf("genre link moved updated_at from %s to %s", created, afterGenre)
	}
	var renamed time.Time
	if err := pool.QueryRow(ctx, `
		update venues set name = 'Renamed' where id = $1 returning updated_at`, id).Scan(&renamed); err != nil {
		t.Fatal(err)
	}
	if !renamed.After(created) {
		t.Fatalf("rename left updated_at at %s", renamed)
	}
	var forced time.Time
	if err := pool.QueryRow(ctx, `
		update venues set updated_at = '2000-01-01T00:00:00Z' where id = $1 returning updated_at`, id).Scan(&forced); err != nil {
		t.Fatal(err)
	}
	if forced.Equal(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) || !forced.After(renamed) {
		t.Fatalf("supplied updated_at stored as %s", forced)
	}
}

func TestDataAPIStillDenied(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), "reset role")
		conn.Release()
	}()
	if _, err = conn.Exec(ctx, `grant usage on schema public to anon`); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, `set role anon`); err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(ctx, `select venue_name_key('Bar!')`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("name key err %v", err)
	}
}
