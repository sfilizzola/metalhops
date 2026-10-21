package database

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
)

func vsCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func TestVenueSafetyConstraints(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	user := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	operator := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	venue := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1), ($2)`, user, operator); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into venues (id, name, location, address, city, country_code, venue_type)
		values ($1, 'Bar', ST_MakePoint(10, 60)::geography, '1 Street', 'Oslo', 'NO', 'bar')`, venue); err != nil {
		t.Fatal(err)
	}

	// Venue state checks.
	for _, c := range []struct{ name, sql string }{
		{"rejected without reason", `update venues set verification_status = 'rejected' where id = $1`},
		{"reason on a pending venue", `update venues set rejection_reason = 'duplicate' where id = $1`},
		{"rejection other without note", `update venues set verification_status = 'rejected', rejection_reason = 'other' where id = $1`},
		{"unknown rejection reason", `update venues set verification_status = 'rejected', rejection_reason = 'bad' where id = $1`},
		{"delisted without reason", `update venues set verification_status = 'delisted' where id = $1`},
		{"delisting other without note", `update venues set verification_status = 'delisted', delisting_reason = 'other' where id = $1`},
		{"empty note", `update venues set verification_status = 'delisted', delisting_reason = 'duplicate', delisting_note = '' where id = $1`},
	} {
		if _, err := pool.Exec(ctx, c.sql, venue); vsCode(err) != "23514" {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
	if _, err := pool.Exec(ctx, `
		update venues set verification_status = 'rejected', rejection_reason = 'duplicate' where id = $1`, venue); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		update venues set verification_status = 'pending', rejection_reason = null, rejection_note = null where id = $1`, venue); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		update venues set verification_status = 'delisted', delisting_reason = 'permanently_closed' where id = $1`, venue); err != nil {
		t.Fatal(err)
	}

	// Venue reports.
	for _, c := range []struct{ name, sql string }{
		{"incorrect_details without note", `insert into venue_reports (reporter_user_id, venue_id, reason) values ($1, $2, 'incorrect_details')`},
		{"other without note", `insert into venue_reports (reporter_user_id, venue_id, reason) values ($1, $2, 'other')`},
		{"unknown reason", `insert into venue_reports (reporter_user_id, venue_id, reason) values ($1, $2, 'spam')`},
	} {
		if _, err := pool.Exec(ctx, c.sql, user, venue); vsCode(err) != "23514" {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
	if _, err := pool.Exec(ctx, `insert into venue_reports (reporter_user_id, venue_id, reason) values ($1, $2, 'moved')`, user, venue); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into venue_reports (reporter_user_id, venue_id, reason) values ($1, $2, 'duplicate')`, user, venue); vsCode(err) != "23505" {
		t.Fatalf("second pending report: %v", err)
	}
	if _, err := pool.Exec(ctx, `update venue_reports set status = 'dismissed' where venue_id = $1`, venue); vsCode(err) != "23514" {
		t.Fatalf("resolved without time: %v", err)
	}
	if _, err := pool.Exec(ctx, `update venue_reports set status = 'dismissed', resolved_at = now() where venue_id = $1`, venue); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into venue_reports (reporter_user_id, venue_id, reason) values ($1, $2, 'duplicate')`, user, venue); err != nil {
		t.Fatalf("a resolved report blocked a new one: %v", err)
	}

	// Audit log.
	if _, err := pool.Exec(ctx, `
		insert into venue_moderation_actions (venue_id, action, prior_status, new_status, delisting_reason)
		values ($1, 'delist', 'verified', 'delisted', 'duplicate')`, venue); vsCode(err) != "23514" {
		t.Fatalf("audit without actor: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into venue_moderation_actions (venue_id, action, prior_status, new_status, actor_user_id)
		values ($1, 'delist', 'verified', 'delisted', $2)`, venue, operator); vsCode(err) != "23514" {
		t.Fatalf("delist without reason: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into venue_moderation_actions (venue_id, action, prior_status, new_status, actor_user_id)
		values ($1, 'verify', 'rejected', 'verified', $2)`, venue, operator); vsCode(err) != "23514" {
		t.Fatalf("verify from rejected: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into venue_moderation_actions (venue_id, action, prior_status, new_status, delisting_reason, actor_user_id)
		values ($1, 'delist', 'verified', 'delisted', 'duplicate', $2)`, venue, operator); err != nil {
		t.Fatal(err)
	}
	// Feedback changes on reject or delist and a Submitter resubmission are allowed.
	for _, c := range []struct{ name, sql string }{
		{"feedback-only reject", `insert into venue_moderation_actions (venue_id, action, prior_status, new_status, rejection_reason, actor_user_id)
			values ($1, 'reject', 'rejected', 'rejected', 'duplicate', $2)`},
		{"feedback-only delist", `insert into venue_moderation_actions (venue_id, action, prior_status, new_status, delisting_reason, delisting_note, actor_user_id)
			values ($1, 'delist', 'delisted', 'delisted', 'other', 'moved', $2)`},
		{"resubmit", `insert into venue_moderation_actions (venue_id, action, prior_status, new_status, actor_user_id)
			values ($1, 'resubmit', 'rejected', 'pending', $2)`},
	} {
		if _, err := pool.Exec(ctx, c.sql, venue, operator); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
	for _, c := range []struct{ name, sql string }{
		{"same-status verify", `insert into venue_moderation_actions (venue_id, action, prior_status, new_status, actor_user_id)
			values ($1, 'verify', 'verified', 'verified', $2)`},
		{"same-status reopen", `insert into venue_moderation_actions (venue_id, action, prior_status, new_status, actor_user_id)
			values ($1, 'reopen', 'verified', 'verified', $2)`},
		{"reject on a pending row without change", `insert into venue_moderation_actions (venue_id, action, prior_status, new_status, rejection_reason, actor_user_id)
			values ($1, 'reject', 'pending', 'pending', 'duplicate', $2)`},
		{"cross-status delist", `insert into venue_moderation_actions (venue_id, action, prior_status, new_status, delisting_reason, actor_user_id)
			values ($1, 'delist', 'rejected', 'rejected', 'duplicate', $2)`},
		{"resubmit from delisted", `insert into venue_moderation_actions (venue_id, action, prior_status, new_status, actor_user_id)
			values ($1, 'resubmit', 'delisted', 'pending', $2)`},
		{"resubmit with a reason", `insert into venue_moderation_actions (venue_id, action, prior_status, new_status, rejection_reason, actor_user_id)
			values ($1, 'resubmit', 'rejected', 'pending', 'duplicate', $2)`},
	} {
		if _, err := pool.Exec(ctx, c.sql, venue, operator); vsCode(err) != "23514" {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
	if _, err := pool.Exec(ctx, `
		insert into venue_moderation_actions (venue_id, action, prior_status, new_status, actor_user_id)
		values ($1, 'resubmit', 'rejected', 'pending', $2)`, venue, user); err != nil {
		t.Fatalf("resubmit by the Submitter: %v", err)
	}
	if _, err := pool.Exec(ctx, `update venue_moderation_actions set delisting_reason = 'other' where venue_id = $1`, venue); vsCode(err) != "55000" {
		t.Fatalf("audit update: %v", err)
	}
	if _, err := pool.Exec(ctx, `delete from venue_moderation_actions where venue_id = $1`, venue); vsCode(err) != "55000" {
		t.Fatalf("audit delete: %v", err)
	}
	if _, err := pool.Exec(ctx, `delete from auth.users where id in ($1, $2)`, operator, user); err != nil {
		t.Fatalf("deleting the actors: %v", err)
	}
	var total, withActor int
	if err := pool.QueryRow(ctx, `select count(*), count(actor_user_id) from venue_moderation_actions where venue_id = $1`, venue).Scan(&total, &withActor); err != nil || total != 5 || withActor != 0 {
		t.Fatalf("actors after delete: %d of %d kept, %v", withActor, total, err)
	}
}
