package profile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

func TestGetAndPatch(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	userID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, userID); err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.NewUserLimiter(clock)
	service := New(pool, limiter, nil, clock, "")

	if _, err := service.Get(ctx, userID); err != ErrNotFound {
		t.Fatalf("missing %v", err)
	}
	username := "metal_fan"
	if _, err := service.Patch(ctx, userID, Patch{Username: &username}); err == nil {
		t.Fatal("create without display name")
	}
	var n int
	if err := pool.QueryRow(ctx, `select count(*) from profiles`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows %d %v", n, err)
	}

	display := " Metal Fan "
	created, err := service.Patch(ctx, userID, Patch{Username: &username, DisplayName: &display})
	if err != nil || created.Username != "metal_fan" || created.DisplayName != "Metal Fan" || created.Status != "active" {
		t.Fatalf("create %#v %v", created, err)
	}
	got, err := service.Get(ctx, userID)
	if err != nil || got.Username != created.Username {
		t.Fatalf("get %#v %v", got, err)
	}

	next := "Metal Fan"
	unchanged, err := service.Patch(ctx, userID, Patch{DisplayName: &next})
	if err != nil || !unchanged.UpdatedAt.Equal(created.UpdatedAt) {
		t.Fatalf("unchanged %#v %v", unchanged, err)
	}
	held := make([]*ratelimit.Reservation, 0, 59)
	for i := 0; i < 59; i++ {
		reservation, _, ok := limiter.Reserve(userID)
		if !ok {
			t.Fatalf("no-op spent a token at %d", i)
		}
		held = append(held, reservation)
	}
	if _, _, ok := limiter.Reserve(userID); ok {
		t.Fatal("create did not spend a token")
	}
	for _, reservation := range held {
		reservation.Cancel()
	}

	now = now.Add(time.Minute)
	partial := "Partial"
	updated, err := service.Patch(ctx, userID, Patch{DisplayName: &partial})
	if err != nil || updated.DisplayName != "Partial" || updated.Username != "metal_fan" || !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Fatalf("partial %#v %v", updated, err)
	}
}

func TestPatchKeepsStoredModerationUntilChange(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	userID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into profiles (
			id, username, display_name, status, moderation_reason, moderation_note, created_at, updated_at
		) values ($1, 'keptname', 'Kept', 'rename_required', 'abusive_name', 'note', $2, $2)`, userID, now); err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.NewUserLimiter(clock)
	service := New(pool, limiter, nil, clock, "")
	username, display := "keptname", "Kept"
	got, err := service.Patch(ctx, userID, Patch{Username: &username, DisplayName: &display})
	if err != nil || got.Status != "rename_required" || !got.UpdatedAt.Equal(now) {
		t.Fatalf("noop %#v %v", got, err)
	}
	var reason, note *string
	if err := pool.QueryRow(ctx, `select moderation_reason, moderation_note from profiles where id = $1`, userID).Scan(&reason, &note); err != nil || reason == nil || *reason != "abusive_name" {
		t.Fatalf("moderation %v %v %v", reason, note, err)
	}

	now = now.Add(time.Minute)
	corrected := "KeptName"
	changed, err := service.Patch(ctx, userID, Patch{Username: &corrected})
	if err != nil || changed.Username != "KeptName" || changed.Status != "active" || !changed.UpdatedAt.Equal(now) {
		t.Fatalf("correction %#v %v", changed, err)
	}
	if err := pool.QueryRow(ctx, `select moderation_reason, moderation_note from profiles where id = $1`, userID).Scan(&reason, &note); err != nil || reason != nil || note != nil {
		t.Fatalf("cleared %v %v %v", reason, note, err)
	}
	if left := drain(limiter, userID); left != 59 {
		t.Fatalf("correction tokens left %d", left)
	}
}

func TestPatchUsernameTakenAndLimit(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	owner := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	other := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1), ($2)`, owner, other); err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.NewUserLimiter(clock)
	service := New(pool, limiter, nil, clock, "")
	username, display := "metal_fan", "Owner"
	if _, err := service.Patch(ctx, owner, Patch{Username: &username, DisplayName: &display}); err != nil {
		t.Fatal(err)
	}
	taken := "Metal_Fan"
	if _, err := service.Patch(ctx, other, Patch{Username: &taken, DisplayName: &display}); err != ErrUsernameTaken {
		t.Fatalf("taken %v", err)
	}
	if left := drain(limiter, other); left != 60 {
		t.Fatalf("failed write left %d", left)
	}

	bad := "no"
	if _, err := service.Patch(ctx, other, Patch{Username: &bad, DisplayName: &display}); err == nil {
		t.Fatal("invalid username reached SQL")
	}
	var n int
	if err := pool.QueryRow(ctx, `select count(*) from profiles where id = $1`, other).Scan(&n); err != nil || n != 0 {
		t.Fatalf("invalid row %d %v", n, err)
	}

	limited := ratelimit.NewUserLimiter(clock)
	limitedService := New(pool, limited, nil, clock, "")
	for i := 0; i < 60; i++ {
		name := fmt.Sprintf("Name %d", i)
		if _, err := limitedService.Patch(ctx, owner, Patch{DisplayName: &name}); err != nil {
			t.Fatalf("change %d %v", i, err)
		}
	}
	overflow := "overflow"
	_, err := limitedService.Patch(ctx, owner, Patch{DisplayName: &overflow})
	var limitedErr *RateLimitError
	if !errors.As(err, &limitedErr) || limitedErr.RetryAfter < 1 {
		t.Fatalf("limit %v", err)
	}
}

func TestExpiredSuspensionNormalizesBeforePatch(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	userID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	other := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1), ($2)`, userID, other); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into profiles (
			id, username, display_name, status, moderation_reason, moderation_note, suspended_until, created_at, updated_at
		) values
			($1, 'expired_user', 'Old Name', 'suspended', 'spam', 'bulk mail', $3, $3, $3),
			($2, 'reader_user', 'Reader', 'suspended', 'spam', 'bulk mail', $3, $3, $3)`, userID, other, now); err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.NewUserLimiter(clock)
	service := New(pool, limiter, nil, clock, "")
	display := "New Name"
	got, err := service.Patch(ctx, userID, Patch{DisplayName: &display})
	if err != nil || got.Status != "active" || got.DisplayName != "New Name" || got.ModerationReason != nil || got.ModerationNote != nil || got.SuspendedUntil != nil {
		t.Fatalf("normalized %#v %v", got, err)
	}
	var n int
	var action, actorType string
	var actorID, reason, note *string
	if err := pool.QueryRow(ctx, `select count(*) from profile_moderation_actions where subject_user_id = $1`, userID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("actions %d %v", n, err)
	}
	if err := pool.QueryRow(ctx, `
		select action, actor_type, actor_user_id::text, reason, note
		from profile_moderation_actions
		where subject_user_id = $1`, userID).Scan(&action, &actorType, &actorID, &reason, &note); err != nil || action != "restored" || actorType != "system" || actorID != nil || reason == nil || *reason != "spam" || note == nil || *note != "bulk mail" {
		t.Fatalf("audit %s %s %v %v %v %v", action, actorType, actorID, reason, note, err)
	}
	if left := drain(limiter, userID); left != 59 {
		t.Fatalf("patch tokens left %d", left)
	}

	read, err := service.Get(ctx, other)
	if err != nil || read.Status != "active" || read.ModerationReason != nil || read.ModerationNote != nil || read.SuspendedUntil != nil {
		t.Fatalf("get %#v %v", read, err)
	}
	if left := drain(limiter, other); left != 60 {
		t.Fatalf("get tokens left %d", left)
	}
}

func TestIndefiniteSuspensionRejectsChange(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	userID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into profiles (
			id, username, display_name, status, moderation_reason, created_at, updated_at
		) values ($1, 'keptname', 'Kept', 'suspended', 'spam', $2, $2)`, userID, now); err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.NewUserLimiter(clock)
	service := New(pool, limiter, nil, clock, "")
	display := "New Name"
	_, err := service.Patch(ctx, userID, Patch{DisplayName: &display})
	var suspended *SuspendedError
	if !errors.As(err, &suspended) || suspended.Until != nil {
		t.Fatalf("suspended %v", err)
	}
	var status, name string
	if err := pool.QueryRow(ctx, `select status, display_name from profiles where id = $1`, userID).Scan(&status, &name); err != nil || status != "suspended" || name != "Kept" {
		t.Fatalf("row %s %s %v", status, name, err)
	}
	if left := drain(limiter, userID); left != 60 {
		t.Fatalf("tokens left %d", left)
	}
}

func TestSuspendedUnchangedPatchReturnsStored(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	userID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into profiles (
			id, username, display_name, status, moderation_reason, created_at, updated_at
		) values ($1, 'keptname', 'Kept', 'suspended', 'spam', $2, $2)`, userID, now); err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.NewUserLimiter(clock)
	service := New(pool, limiter, nil, clock, "")
	username, display := "keptname", "Kept"
	got, err := service.Patch(ctx, userID, Patch{Username: &username, DisplayName: &display})
	if err != nil || got.Status != "suspended" || got.Username != "keptname" || got.DisplayName != "Kept" || got.ModerationReason == nil || *got.ModerationReason != "spam" || got.SuspendedUntil != nil {
		t.Fatalf("stored %#v %v", got, err)
	}
	if left := drain(limiter, userID); left != 60 {
		t.Fatalf("tokens left %d", left)
	}
}

func TestRenameRequiredCasingAppendsUserRestore(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	userID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into profiles (
			id, username, display_name, status, moderation_reason, moderation_note, created_at, updated_at
		) values ($1, 'keptname', 'Kept', 'rename_required', 'abusive_name', 'old label', $2, $2)`, userID, now); err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.NewUserLimiter(clock)
	service := New(pool, limiter, nil, clock, "")
	corrected := "KeptName"
	got, err := service.Patch(ctx, userID, Patch{Username: &corrected})
	if err != nil || got.Status != "active" || got.Username != "KeptName" || got.ModerationReason != nil || got.ModerationNote != nil || got.SuspendedUntil != nil {
		t.Fatalf("restored %#v %v", got, err)
	}
	var n int
	var action, actorType, actorID, reason, note string
	if err := pool.QueryRow(ctx, `select count(*) from profile_moderation_actions where subject_user_id = $1`, userID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("actions %d %v", n, err)
	}
	if err := pool.QueryRow(ctx, `
		select action, actor_type, actor_user_id::text, reason, note
		from profile_moderation_actions
		where subject_user_id = $1`, userID).Scan(&action, &actorType, &actorID, &reason, &note); err != nil || action != "restored" || actorType != "user" || actorID != userID || reason != "abusive_name" || note != "old label" {
		t.Fatalf("audit %s %s %s %s %s %v", action, actorType, actorID, reason, note, err)
	}
	if left := drain(limiter, userID); left != 59 {
		t.Fatalf("tokens left %d", left)
	}
}

func TestConcurrentProfileCreate(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	userID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, userID); err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.NewUserLimiter(func() time.Time { return now })
	service := New(pool, limiter, nil, func() time.Time { return now }, "")
	username, display := "raceuser", "Race"
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = service.Patch(ctx, userID, Patch{Username: &username, DisplayName: &display})
		}(i)
	}
	close(start)
	wg.Wait()
	for _, err := range errs {
		if err != nil && !errors.Is(err, ErrUsernameTaken) {
			t.Fatal(err)
		}
		if err != nil && (strings.Contains(err.Error(), "SQLSTATE") || strings.Contains(err.Error(), "duplicate")) {
			t.Fatal(err)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, `select count(*) from profiles`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows %d %v", n, err)
	}
	if left := drain(limiter, userID); left != 59 {
		t.Fatalf("lost reservation restored to %d", left)
	}
}

func TestPatchOptionalFields(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	userID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, userID); err != nil {
		t.Fatal(err)
	}
	base := "https://abc.supabase.co"
	limiter := ratelimit.NewUserLimiter(clock)
	service := New(pool, limiter, nil, clock, base)
	username, display := "metal_fan", "Metal Fan"
	created, err := service.Patch(ctx, userID, Patch{Username: &username, DisplayName: &display})
	if err != nil || created.Bio != nil || created.Gender != nil || created.FromCity != nil || created.FromCountry != nil || created.AvatarURL != nil {
		t.Fatalf("create %#v %v", created, err)
	}

	bio := "Line one\nLine two"
	got, err := service.Patch(ctx, userID, Patch{Bio: &bio})
	if err != nil || got.Bio == nil || *got.Bio != bio || got.Username != "metal_fan" || got.DisplayName != "Metal Fan" {
		t.Fatalf("bio only %#v %v", got, err)
	}
	tooLong, exact := strings.Repeat("é", 281), strings.Repeat("é", 280)
	_, err = service.Patch(ctx, userID, Patch{Bio: &tooLong})
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Fields["bio"] == "" {
		t.Fatalf("bio 281 %v", err)
	}
	if got, err = service.Patch(ctx, userID, Patch{Bio: &exact}); err != nil || *got.Bio != exact {
		t.Fatalf("bio 280 %v", err)
	}

	for _, gender := range []string{"woman", "man", "non_binary", "prefer_not_to_say"} {
		if got, err = service.Patch(ctx, userID, Patch{Gender: &gender}); err != nil || *got.Gender != gender {
			t.Fatalf("gender %s %v", gender, err)
		}
	}
	bad := "x"
	if _, err = service.Patch(ctx, userID, Patch{Gender: &bad}); !errors.As(err, &validation) || validation.Fields["gender"] == "" {
		t.Fatalf("gender x %v", err)
	}
	for _, country := range []string{"de", "DEU"} {
		if _, err = service.Patch(ctx, userID, Patch{FromCountry: &country}); !errors.As(err, &validation) || validation.Fields["from_country"] == "" {
			t.Fatalf("country %s %v", country, err)
		}
	}
	city, longCity, country := "Hamburg", strings.Repeat("a", 81), "DE"
	if _, err = service.Patch(ctx, userID, Patch{FromCity: &longCity}); !errors.As(err, &validation) || validation.Fields["from_city"] == "" {
		t.Fatalf("city 81 %v", err)
	}
	avatar := base + "/storage/v1/object/public/avatars/" + userID + "/a1b2.webp"
	foreign := base + "/storage/v1/object/public/avatars/bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb/a1b2.webp"
	if _, err = service.Patch(ctx, userID, Patch{AvatarURL: &foreign}); !errors.As(err, &validation) || validation.Fields["avatar_url"] == "" {
		t.Fatalf("foreign avatar %v", err)
	}
	got, err = service.Patch(ctx, userID, Patch{FromCity: &city, FromCountry: &country, AvatarURL: &avatar})
	if err != nil || *got.FromCity != "Hamburg" || *got.FromCountry != "DE" || *got.AvatarURL != avatar || *got.Gender != "prefer_not_to_say" || *got.Bio != exact {
		t.Fatalf("place and avatar %#v %v", got, err)
	}

	limiter = ratelimit.NewUserLimiter(clock)
	service = New(pool, limiter, nil, clock, base)
	now = now.Add(time.Minute)
	unchanged, err := service.Patch(ctx, userID, Patch{FromCity: &city, AvatarURL: &avatar})
	if err != nil || !unchanged.UpdatedAt.Equal(got.UpdatedAt) || drain(limiter, userID) != 60 {
		t.Fatalf("no-op %#v %v", unchanged, err)
	}
	limiter = ratelimit.NewUserLimiter(clock)
	service = New(pool, limiter, nil, clock, base)

	empty := ""
	cleared, err := service.Patch(ctx, userID, Patch{Bio: &empty, Gender: &empty, FromCity: &empty, FromCountry: &empty, AvatarURL: &empty})
	if err != nil || cleared.Bio != nil || cleared.Gender != nil || cleared.FromCity != nil || cleared.FromCountry != nil || cleared.AvatarURL != nil || cleared.Username != "metal_fan" {
		t.Fatalf("cleared %#v %v", cleared, err)
	}
	read, err := service.Get(ctx, userID)
	if err != nil || read.Bio != nil || read.Gender != nil || read.FromCity != nil || read.FromCountry != nil || read.AvatarURL != nil {
		t.Fatalf("read %#v %v", read, err)
	}
}

func TestOptionalEditKeepsRenameRequired(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	userID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into profiles (
			id, username, display_name, status, moderation_reason, moderation_note, created_at, updated_at
		) values ($1, 'keptname', 'Kept', 'rename_required', 'abusive_name', 'note', $2, $2)`, userID, now); err != nil {
		t.Fatal(err)
	}
	service := New(pool, ratelimit.NewUserLimiter(clock), nil, clock, "")
	bio, gender := "New bio", "woman"
	got, err := service.Patch(ctx, userID, Patch{Bio: &bio, Gender: &gender})
	if err != nil || got.Status != "rename_required" || got.ModerationReason == nil || *got.ModerationReason != "abusive_name" ||
		got.ModerationNote == nil || *got.ModerationNote != "note" || *got.Bio != "New bio" {
		t.Fatalf("bio edit %#v %v", got, err)
	}
	var n int
	if err := pool.QueryRow(ctx, `select count(*) from profile_moderation_actions where subject_user_id = $1`, userID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("bio edit audit %d %v", n, err)
	}

	display := "Renamed"
	got, err = service.Patch(ctx, userID, Patch{DisplayName: &display})
	if err != nil || got.Status != "active" || got.ModerationReason != nil || got.ModerationNote != nil || *got.Bio != "New bio" {
		t.Fatalf("rename %#v %v", got, err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from profile_moderation_actions where subject_user_id = $1 and action = 'restored'`, userID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rename audit %d %v", n, err)
	}
}

func TestSuspendedRejectsOptionalFields(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	userID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into profiles (
			id, username, display_name, status, moderation_reason, created_at, updated_at
		) values ($1, 'keptname', 'Kept', 'suspended', 'spam', $2, $2)`, userID, now); err != nil {
		t.Fatal(err)
	}
	base := "https://abc.supabase.co"
	service := New(pool, ratelimit.NewUserLimiter(clock), nil, clock, base)
	value, gender, country := "x", "man", "DE"
	avatar := base + "/storage/v1/object/public/avatars/" + userID + "/a.webp"
	for _, patch := range []Patch{{Bio: &value}, {Gender: &gender}, {FromCity: &value}, {FromCountry: &country}, {AvatarURL: &avatar}} {
		_, err := service.Patch(ctx, userID, patch)
		var suspended *SuspendedError
		if !errors.As(err, &suspended) {
			t.Fatalf("suspended %#v %v", patch, err)
		}
	}
	var bio, storedGender *string
	if err := pool.QueryRow(ctx, `select bio, gender from profiles where id = $1`, userID).Scan(&bio, &storedGender); err != nil || bio != nil || storedGender != nil {
		t.Fatalf("row %v %v %v", bio, storedGender, err)
	}
}

func TestProfileStats(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	userID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	other := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	venueA := "11111111-1111-4111-8111-111111111111"
	venueB := "22222222-2222-4222-8222-222222222222"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1), ($2)`, userID, other); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into profiles (id, username, display_name) values ($1, 'counter', 'Counter'), ($2, 'nobody', 'Nobody')`, userID, other); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into venues (
			id, name, description, location, address, city, country_code, venue_type, verification_status
		) values
			($1, 'A', 'A metal bar', ST_MakePoint(10, 60)::geography, '1 Metal Street', 'Oslo', 'NO', 'bar', 'verified'),
			($2, 'B', 'A metal bar', ST_MakePoint(10, 60)::geography, '2 Metal Street', 'Oslo', 'NO', 'bar', 'verified')`, venueA, venueB); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into check_ins (venue_id, user_id, visited_at, recorded_at) values
			($1, $3, $4, $4), ($1, $3, $4 + interval '2 hours', $4), ($2, $3, $4, $4)`, venueA, venueB, userID, now); err != nil {
		t.Fatal(err)
	}
	service := New(pool, ratelimit.NewUserLimiter(clock), nil, clock, "")
	got, err := service.Get(ctx, userID)
	if err != nil || got.Stats != (Stats{CheckIns: 3, Venues: 2}) {
		t.Fatalf("stats %#v %v", got.Stats, err)
	}
	none, err := service.Get(ctx, other)
	if err != nil || none.Stats != (Stats{}) {
		t.Fatalf("empty stats %#v %v", none.Stats, err)
	}
	bio := "counted"
	patched, err := service.Patch(ctx, userID, Patch{Bio: &bio})
	if err != nil || patched.Stats != (Stats{CheckIns: 3, Venues: 2}) {
		t.Fatalf("patch stats %#v %v", patched.Stats, err)
	}
	unchanged, err := service.Patch(ctx, userID, Patch{Bio: &bio})
	if err != nil || unchanged.Stats != (Stats{CheckIns: 3, Venues: 2}) {
		t.Fatalf("no-op stats %#v %v", unchanged.Stats, err)
	}

	if _, err := pool.Exec(ctx, `delete from check_ins where venue_id = $1`, venueB); err != nil {
		t.Fatal(err)
	}
	if got, err = service.Get(ctx, userID); err != nil || got.Stats != (Stats{CheckIns: 2, Venues: 1}) {
		t.Fatalf("after delete %#v %v", got.Stats, err)
	}
	if _, err := pool.Exec(ctx, `delete from check_ins where user_id = $1 and visited_at > $2`, userID, now); err != nil {
		t.Fatal(err)
	}
	if got, err = service.Get(ctx, userID); err != nil || got.Stats != (Stats{CheckIns: 1, Venues: 1}) {
		t.Fatalf("after second delete %#v %v", got.Stats, err)
	}
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func drain(limiter *ratelimit.UserLimiter, userID string) int {
	n := 0
	for n < 60 {
		reservation, _, ok := limiter.Reserve(userID)
		if !ok {
			break
		}
		reservation.Commit()
		n++
	}
	return n
}
