package database

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
)

func TestProfileSafetyConstraints(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	user := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	other := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1), ($2)`, user, other); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into profiles (id, username, display_name) values ($1, 'metal_fan', 'Metal Fan')`, user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into platform_operators (user_id) values ($1)`, user); err == nil {
		t.Fatal("operator row overlapped a profile")
	}
	if _, err := pool.Exec(ctx, `insert into platform_operators (user_id) values ($1)`, other); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into profiles (id, username, display_name) values ($1, 'mod_fan', 'Mod')`, other); err == nil {
		t.Fatal("profile overlapped an operator")
	}
	if _, err := pool.Exec(ctx, `
		update profiles
		set status = 'rename_required', moderation_reason = 'abusive_name', suspended_until = now()
		where id = $1`, user); err == nil {
		t.Fatal("rename_required accepted an expiry")
	}
	if _, err := pool.Exec(ctx, `
		insert into profile_reports (reporter_user_id, target_user_id, reason)
		values ($1, $1, 'spam')`, user); err == nil {
		t.Fatal("self-report accepted")
	}
	if _, err := pool.Exec(ctx, `
		insert into profile_moderation_actions (action, actor_type, reason)
		values ('suspended', 'operator', 'spam')`); err == nil {
		t.Fatal("operator action accepted a null actor")
	}
	var action string
	if err := pool.QueryRow(ctx, `
		insert into profile_moderation_actions (subject_user_id, action, actor_type, actor_user_id, reason)
		values ($1, 'rename_required', 'operator', $2, 'spam')
		returning action`, user, other).Scan(&action); err != nil || action != "rename_required" {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `delete from profile_moderation_actions`); err == nil {
		t.Fatal("audit delete succeeded")
	}
	if _, err := pool.Exec(ctx, `delete from auth.users where id = $1`, user); err != nil {
		t.Fatal(err)
	}
	var subject *string
	if err := pool.QueryRow(ctx, `select subject_user_id::text from profile_moderation_actions`).Scan(&subject); err != nil || subject != nil {
		t.Fatalf("audit subject %v %v", subject, err)
	}
	if _, err := pool.Exec(ctx, `delete from auth.users where id = $1`, other); err != nil {
		t.Fatal(err)
	}
	var actor *string
	if err := pool.QueryRow(ctx, `select actor_user_id::text from profile_moderation_actions`).Scan(&actor); err != nil || actor != nil {
		t.Fatalf("audit actor %v %v", actor, err)
	}
}
