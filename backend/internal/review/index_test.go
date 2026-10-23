package review

import (
	"context"
	"strings"
	"testing"
)

func TestReviewReportListPlan(t *testing.T) {
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
	if _, err := pool.Exec(ctx, `
		insert into venue_genres (venue_id, genre_id)
		select id, (select id from genres where name = 'Punk') from venues`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		with users as (select id, row_number() over (order by id) - 1 as n from auth.users),
		     venues_n as (select id, row_number() over (order by id) - 1 as n from venues)
		insert into reviews (venue_id, user_id, rating, commentary, created_at, updated_at)
		select venues_n.id, users.id, 4, 'text', now(), now()
		from generate_series(0, 499) as i
		join users on users.n = i
		join venues_n on venues_n.n = i`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		with users as (select id, row_number() over (order by id) - 1 as n from auth.users),
		     reviews_n as (select id, row_number() over (order by id) - 1 as n from reviews)
		insert into review_reports (reporter_user_id, review_id, reason, created_at)
		select users.id, reviews_n.id, 'spam', now() - (i || ' minutes')::interval
		from generate_series(0, 4999) as i
		join users on users.n = i % 500
		join reviews_n on reviews_n.n = i / 500`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `vacuum analyze review_reports`); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, "explain "+reportListSQL, nil, nil, 21)
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
	if !strings.Contains(plan.String(), "review_reports_pending_list_key") {
		t.Fatalf("list plan does not use review_reports_pending_list_key:\n%s", plan.String())
	}
}
