package database

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
)

func TestVenueSearchKey(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	cases := map[string]string{
		"Saint-Étienne":   "saintetienne",
		"Saint Etienne":   "saintetienne",
		"Iron Maiden Pub": "ironmaidenpub",
		"O'Connor's":      "oconnors",
		"Bar  One":        "barone",
		"Бар №1":          "барno1",
		"100%_off":        "100off",
		"!!!":             "",
	}
	for input, want := range cases {
		var got string
		if err := pool.QueryRow(ctx, `select venue_search_key($1)`, input).Scan(&got); err != nil || got != want {
			t.Fatalf("%q got %q want %q err %v", input, got, want, err)
		}
	}
}

func TestSearchKeyTriggerOwnsColumns(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var id, name, address, city string
	if err := pool.QueryRow(ctx, `
		insert into venues (name, location, address, city, country_code, search_name, search_address, search_city)
		values ('Iron-Maiden Pub', ST_MakePoint(4.39, 45.43)::geography, '12 Rue du Clocher', 'Saint-Étienne', 'FR', 'forged', 'forged', 'forged')
		returning id::text, search_name, search_address, search_city`).Scan(&id, &name, &address, &city); err != nil {
		t.Fatal(err)
	}
	if name != "ironmaidenpub" || address != "12rueduclocher" || city != "saintetienne" {
		t.Fatalf("insert keys %q %q %q", name, address, city)
	}
	if err := pool.QueryRow(ctx, `
		update venues set name = 'Black Hole' where id = $1 returning search_name`, id).Scan(&name); err != nil || name != "blackhole" {
		t.Fatalf("rename key %q err %v", name, err)
	}
	if err := pool.QueryRow(ctx, `
		update venues set search_city = 'forged' where id = $1 returning search_city`, id).Scan(&city); err != nil || city != "saintetienne" {
		t.Fatalf("forged city key %q err %v", city, err)
	}
}
