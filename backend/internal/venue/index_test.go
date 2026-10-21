package venue

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
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
		if _, err := pool.Exec(ctx, "vacuum analyze "+table); err != nil {
			t.Fatal(err)
		}
	}
	service := New(pool, nil, nil, time.Now)
	cases := []struct {
		name    string
		raw     RawQuery
		indexes []string
	}{
		{"text", RawQuery{Query: "venue4321"}, []string{"venues_search_name_trgm_idx", "venues_search_address_trgm_idx", "venues_search_city_trgm_idx"}},
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

func TestSubmissionIndexes(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if _, err := pool.Exec(ctx, `insert into auth.users (id) select gen_random_uuid() from generate_series(1, 500)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		with users as (select id, row_number() over (order by id) as n from auth.users)
		insert into venues (name, location, address, city, country_code, verification_status, submitted_by, created_at, updated_at)
		select 'Venue ' || i,
		       ST_SetSRID(ST_MakePoint((i % 360) - 179.5, ((i / 360) % 170) - 85), 4326)::geography,
		       i || ' Main Street', 'City', 'DE', 'pending', users.id,
		       now() - (i || ' minutes')::interval, now() - (i || ' minutes')::interval
		from generate_series(1, 5000) as i
		join users on users.n = (i % 500) + 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "vacuum analyze venues"); err != nil {
		t.Fatal(err)
	}
	var user string
	if err := pool.QueryRow(ctx, `select id::text from auth.users order by id limit 1`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		sql   string
		args  []any
		index string
	}{
		{"quota", quotaSQL, []any{user, time.Now().Add(-24 * time.Hour)}, "venues_submitted_by_created_at_idx"},
		{"own list", ownListSQL(false), []any{user, 21}, "venues_submitted_by_updated_at_idx"},
		{"own list after", ownListSQL(true), []any{user, time.Now(), "ffffffff-ffff-4fff-8fff-ffffffffffff", 21}, "venues_submitted_by_updated_at_idx"},
	}
	for _, c := range cases {
		rows, err := pool.Query(ctx, "explain "+c.sql, c.args...)
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
		if plan := strings.Join(lines, "\n"); !strings.Contains(plan, c.index) {
			t.Fatalf("%s plan does not use %s:\n%s", c.name, c.index, plan)
		}
	}
}

func TestVenueReportIndexes(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if _, err := pool.Exec(ctx, `insert into auth.users (id) select gen_random_uuid() from generate_series(1, 500)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into venues (id, name, location, address, city, country_code, verification_status)
		select gen_random_uuid(), 'Venue ' || i, ST_SetSRID(ST_MakePoint((i % 360) - 179.5, ((i / 360) % 170) - 85), 4326)::geography,
		       i || ' Main Street', 'City', 'DE', 'verified'
		from generate_series(1, 1000) as i`); err != nil {
		t.Fatal(err)
	}
	// Pair (i % 500, (i / 5) % 1000) is unique for i in 0..4999, so the pending-pair index never collides.
	if _, err := pool.Exec(ctx, `
		with users as (select id, row_number() over (order by id) - 1 as n from auth.users),
		     venues_n as (select id, row_number() over (order by id) - 1 as n from venues)
		insert into venue_reports (reporter_user_id, venue_id, reason, created_at)
		select users.id, venues_n.id, 'moved', now() - (i || ' minutes')::interval
		from generate_series(0, 4999) as i
		join users on users.n = i % 500
		join venues_n on venues_n.n = (i / 5) % 1000`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		with venues_n as (select id, row_number() over (order by id) - 1 as n from venues)
		insert into venue_moderation_actions (venue_id, action, prior_status, new_status, actor_user_id, created_at)
		select venues_n.id, 'correct', 'verified', 'verified', (select id from auth.users limit 1), now() - (i || ' minutes')::interval
		from generate_series(0, 4999) as i
		join venues_n on venues_n.n = i % 1000`); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"venue_reports", "venue_moderation_actions"} {
		if _, err := pool.Exec(ctx, "vacuum analyze "+table); err != nil {
			t.Fatal(err)
		}
	}
	var venueID string
	if err := pool.QueryRow(ctx, `select venue_id::text from venue_reports limit 1`).Scan(&venueID); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, sql, index string
		args             []any
	}{
		{"pending list", reportListSQL, "venue_reports_pending_list_key", []any{nil, nil, 21}},
		{"venue reports", venueReportsSQL, "venue_reports_pending_venue_key", []any{venueID, 100}},
		{"moderation history", venueHistorySQL, "venue_moderation_actions_venue_key", []any{venueID, 20}},
	} {
		rows, err := pool.Query(ctx, "explain "+c.sql, c.args...)
		if err != nil {
			t.Fatal(err)
		}
		var plan strings.Builder
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			plan.WriteString(line + "\n")
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(plan.String(), c.index) {
			t.Fatalf("%s plan does not use %s:\n%s", c.name, c.index, plan.String())
		}
	}
}
