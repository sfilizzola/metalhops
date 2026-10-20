package venue

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

func TestDiscoveryIndexes(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	punk := genreID(t, pool, "Punk")
	if _, err := pool.Exec(ctx, `insert into genres (name) values ('Rare test')`); err != nil {
		t.Fatal(err)
	}
	rare := genreID(t, pool, "Rare test")
	if _, err := pool.Exec(ctx, `
		insert into venues (name, location, address, city, country_code, verification_status)
		select 'Venue ' || i,
		       ST_SetSRID(ST_MakePoint((i % 360) - 179.5, ((i / 360) % 170) - 85), 4326)::geography,
		       i || ' Main Street',
		       'City ' || (i % 500),
		       case when i % 1000 = 0 then 'NO' else 'DE' end,
		       'verified'
		from generate_series(1, 5000) as i`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into venue_genres (venue_id, genre_id) select id, $1 from venues`, punk); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into venue_genres (venue_id, genre_id)
		select id, $1 from venues where name in ('Venue 7', 'Venue 8', 'Venue 9')`, rare); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"venues", "venue_genres"} {
		if _, err := pool.Exec(ctx, "analyze "+table); err != nil {
			t.Fatal(err)
		}
	}
	service := New(pool)
	cases := []struct {
		name    string
		raw     RawQuery
		indexes []string
	}{
		{"city", RawQuery{City: "City 7"}, []string{"venues_search_city_idx"}},
		{"country", RawQuery{CountryCode: "NO"}, []string{"venues_country_code_idx"}},
		{"genre", RawQuery{GenreIDs: []string{strconv.Itoa(rare)}}, []string{"venue_genres_genre_id_idx"}},
		{"radius", RawQuery{Latitude: "-85", Longitude: "0", RadiusKM: "5"}, []string{"venues_location_idx"}},
	}
	for _, c := range cases {
		c.raw.Limit = 20
		q, err := ParseQuery(c.raw)
		if err != nil {
			t.Fatal(err)
		}
		textKey, cityKey, err := service.searchKeys(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		sql, args := listSQL(q.mode(), q, textKey, cityKey, nil)
		rows, err := pool.Query(ctx, "explain "+sql, args...)
		if err != nil {
			t.Fatal(err)
		}
		var lines []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			lines = append(lines, line)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		plan := strings.Join(lines, "\n")
		for _, index := range c.indexes {
			if !strings.Contains(plan, index) {
				t.Fatalf("%s plan does not use %s:\n%s", c.name, index, plan)
			}
		}
	}
}
