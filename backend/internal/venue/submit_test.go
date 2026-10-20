package venue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

// Earlier than any container clock, so trigger-set updated_at is always later.
var submitNow = time.Date(2020, 1, 15, 12, 0, 0, 0, time.UTC)

const (
	userA = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	userB = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

type harness struct {
	pool    *pgxpool.Pool
	service *Service
	limiter *ratelimit.UserLimiter
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := testPool(t)
	clock := func() time.Time { return submitNow }
	limiter := ratelimit.NewUserLimiter(clock)
	return &harness{
		pool:    pool,
		service: New(pool, profile.New(pool, limiter, nil, clock), limiter, clock),
		limiter: limiter,
	}
}

func insertUser(t *testing.T, pool *pgxpool.Pool, id, status string, until *time.Time) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, id); err != nil {
		t.Fatal(err)
	}
	if status == "" {
		return
	}
	var reason *string
	if status != "active" {
		value := "spam"
		reason = &value
	}
	if _, err := pool.Exec(ctx, `
		insert into profiles (
			id, username, display_name, status, moderation_reason, suspended_until, created_at, updated_at
		) values ($1, $2, 'Tester', $3, $4, $5, $6, $6)`,
		id, "u"+id[len(id)-12:], status, reason, until, submitNow); err != nil {
		t.Fatal(err)
	}
}

type seed struct {
	id        string
	name      string
	status    string
	submitter *string
	lat, lon  float64
	created   time.Time
	updated   time.Time
	genre     int
}

func seedVenue(t *testing.T, pool *pgxpool.Pool, s seed) {
	t.Helper()
	ctx := context.Background()
	if s.status == "" {
		s.status = "verified"
	}
	if s.created.IsZero() {
		s.created = submitNow.Add(-48 * time.Hour)
	}
	if s.updated.IsZero() {
		s.updated = s.created
	}
	var rejectionReason, rejectionNote *string
	if s.status == "rejected" {
		rejectionReason, rejectionNote = ptr("other"), ptr("seed")
	}
	if _, err := pool.Exec(ctx, `
		insert into venues (
			id, name, location, address, city, country_code, venue_type,
			verification_status, submitted_by, created_at, updated_at, rejection_reason, rejection_note
		) values (
			$1, $2, ST_SetSRID(ST_MakePoint($3::float8, $4::float8), 4326)::geography,
			'1 Metal Street', 'Oslo', 'NO', 'pub', $5, $6::uuid, $7, $8, $9, $10
		)`, s.id, s.name, s.lon, s.lat, s.status, s.submitter, s.created, s.updated, rejectionReason, rejectionNote); err != nil {
		t.Fatal(err)
	}
	if s.genre == 0 {
		s.genre = genreID(t, pool, "Punk")
	}
	if _, err := pool.Exec(ctx, `insert into venue_genres (venue_id, genre_id) values ($1, $2)`, s.id, s.genre); err != nil {
		t.Fatal(err)
	}
}

func rawVenue(name string, lat, lon float64, genres ...int) RawSubmission {
	address, city, country, venueType := "1 Metal Street", "Oslo", "NO", "pub"
	ids := make([]int64, len(genres))
	for i, genre := range genres {
		ids[i] = int64(genre)
	}
	return RawSubmission{
		Name: &name, Latitude: &lat, Longitude: &lon, Address: &address, City: &city,
		CountryCode: &country, VenueType: &venueType, GenreIDs: ids,
	}
}

func tokensLeft(limiter *ratelimit.UserLimiter, userID string) int {
	held := make([]*ratelimit.Reservation, 0, 60)
	for len(held) < 60 {
		reservation, _, ok := limiter.Reserve(userID)
		if !ok {
			break
		}
		held = append(held, reservation)
	}
	for _, reservation := range held {
		reservation.Cancel()
	}
	return len(held)
}

func ptr(s string) *string { return &s }

func submit(h *harness, user, key string, raw RawSubmission) (Submission, error) {
	return h.service.Submit(context.Background(), SubmitCommand{UserID: user, Key: key, Fields: raw})
}

func countRows(t *testing.T, h *harness, query string, args ...any) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSubmitCreatesPublicPendingVenue(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	punk := genreID(t, h.pool, "Punk")
	got, err := submit(h, userA, "create-key1", rawVenue("Iron Maiden Pub", 59.91, 10.75, punk, punk))
	if err != nil {
		t.Fatal(err)
	}
	if got.VerificationStatus != "pending" || got.Name != "Iron Maiden Pub" || len(got.Genres) != 1 ||
		!got.CreatedAt.Equal(submitNow) || !got.UpdatedAt.Equal(submitNow) || got.Latitude != 59.91 || got.Longitude != 10.75 {
		t.Fatalf("submission %+v", got)
	}
	public, err := h.service.GetVenue(ctx, got.ID)
	if err != nil || public.VerificationStatus != "pending" {
		t.Fatalf("public %+v %v", public, err)
	}
	if tokensLeft(h.limiter, userA) != 59 {
		t.Fatal("create did not take one token")
	}
	if countRows(t, h, `select count(*) from idempotency_records where user_id = $1 and operation = 'venue_submission'`, userA) != 1 {
		t.Fatal("idempotency row")
	}
	var submitter string
	if err := h.pool.QueryRow(ctx, `select submitted_by::text from venues where id = $1`, got.ID).Scan(&submitter); err != nil || submitter != userA {
		t.Fatalf("submitter %s %v", submitter, err)
	}
}

func TestSubmitDuplicateRules(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	at := func(n int) float64 { return float64(n) }
	seedVenue(t, h.pool, seed{id: vid(1), name: "Iron Maiden", status: "pending", submitter: ptr(userB), lat: at(1), lon: 10})
	seedVenue(t, h.pool, seed{id: vid(2), name: "Black Hole", status: "pending", submitter: ptr(userB), lat: at(2), lon: 10})
	seedVenue(t, h.pool, seed{id: vid(3), name: "Black Hole", status: "verified", lat: at(2) + 0.00045, lon: 10})
	seedVenue(t, h.pool, seed{id: vid(4), name: "Own Place", status: "pending", submitter: ptr(userA), lat: at(3), lon: 10})
	seedVenue(t, h.pool, seed{id: vid(6), name: "Twin", status: "verified", lat: at(4), lon: 10})
	seedVenue(t, h.pool, seed{id: vid(5), name: "Twin", status: "verified", lat: at(4), lon: 10})
	seedVenue(t, h.pool, seed{id: vid(7), name: "Gone", status: "rejected", lat: at(5), lon: 10})
	seedVenue(t, h.pool, seed{id: vid(8), name: "Iron Maiden", status: "verified", lat: at(6), lon: 10})
	seedVenue(t, h.pool, seed{id: vid(9), name: "!!!", status: "verified", lat: at(7), lon: 10})
	seedVenue(t, h.pool, seed{id: vid(10), name: "Orphan", status: "pending", lat: at(8), lon: 10})
	punk := genreID(t, h.pool, "Punk")

	cases := []struct {
		name  string
		raw   RawSubmission
		match *DuplicateMatch
		dup   bool
	}{
		{"private pending", rawVenue("iron maiden!", at(1), 10, punk), nil, true},
		{"verified beats closer private", rawVenue("Black  Hole", at(2), 10, punk), &DuplicateMatch{vid(3), "verified"}, true},
		{"own pending", rawVenue("Own Place", at(3), 10, punk), &DuplicateMatch{vid(4), "pending"}, true},
		{"tie smallest id", rawVenue("Twin", at(4), 10, punk), &DuplicateMatch{vid(5), "verified"}, true},
		{"rejected ignored", rawVenue("Gone", at(5), 10, punk), nil, false},
		{"space counts", rawVenue("IronMaiden", at(6), 10, punk), nil, false},
		{"punctuation only", rawVenue("???", at(7), 10, punk), &DuplicateMatch{vid(9), "verified"}, true},
		{"deleted submitter stays private", rawVenue("Orphan", at(8), 10, punk), nil, true},
		{"far away", rawVenue("Iron Maiden", at(20), 10, punk), nil, false},
	}
	for i, c := range cases {
		before := tokensLeft(h.limiter, userA)
		_, err := submit(h, userA, "dup-key-"+string(rune('a'+i)), c.raw)
		var duplicate *DuplicateError
		switch {
		case c.dup && !errors.As(err, &duplicate):
			t.Fatalf("%s: err %v", c.name, err)
		case !c.dup && err != nil:
			t.Fatalf("%s: err %v", c.name, err)
		}
		if c.dup {
			if (c.match == nil) != (duplicate.Match == nil) || (c.match != nil && *c.match != *duplicate.Match) {
				t.Fatalf("%s: match %+v", c.name, duplicate.Match)
			}
			if tokensLeft(h.limiter, userA) != before {
				t.Fatalf("%s: duplicate spent a token", c.name)
			}
		}
	}
}

func TestSubmitDistanceBoundary(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	punk := genreID(t, h.pool, "Punk")
	for i, metres := range []float64{99, 100, 101} {
		name := "Edge " + string(rune('A'+i))
		lat := float64(10 + i)
		seedVenue(t, h.pool, seed{id: vid(100 + i), name: name, lat: lat, lon: 20})
		var probeLat, probeLon, distance float64
		if err := h.pool.QueryRow(ctx, `
			with origin as (select ST_SetSRID(ST_MakePoint(20, $1::float8), 4326)::geography as o),
			     probe as (select ST_Project(o, $2::float8, radians(90)) as p, o from origin)
			select ST_Y(p::geometry), ST_X(p::geometry), ST_Distance(p, o) from probe`, lat, metres).Scan(&probeLat, &probeLon, &distance); err != nil {
			t.Fatal(err)
		}
		_, err := submit(h, userA, "edge-key-"+string(rune('a'+i)), rawVenue(name, probeLat, probeLon, punk))
		var duplicate *DuplicateError
		if got, want := errors.As(err, &duplicate), distance <= 100; got != want {
			t.Fatalf("%.0f m (measured %.9f): duplicate %v err %v", metres, distance, got, err)
		}
	}
}

func TestSubmitConcurrentSameName(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	punk := genreID(t, h.pool, "Punk")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, user := range []string{userA, userB} {
		wg.Add(1)
		go func(i int, user string) {
			defer wg.Done()
			_, errs[i] = submit(h, user, "race-key-1", rawVenue("Race Bar", 30, 30, punk))
		}(i, user)
	}
	wg.Wait()
	var duplicate *DuplicateError
	wins := 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case !errors.As(err, &duplicate):
			t.Fatalf("err %v", err)
		}
	}
	if wins != 1 || countRows(t, h, `select count(*) from venues where name = 'Race Bar'`) != 1 {
		t.Fatalf("wins %d errs %v", wins, errs)
	}
}

func TestSubmitQuota(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	punk := genreID(t, h.pool, "Punk")
	statuses := []string{"pending", "verified", "rejected"}
	for i := 0; i < 9; i++ {
		seedVenue(t, h.pool, seed{
			id: vid(200 + i), name: "Quota " + string(rune('A'+i)), status: statuses[i%3], submitter: ptr(userA),
			lat: float64(i), lon: 40, created: submitNow.Add(-time.Duration(23-i) * time.Hour),
		})
	}
	seedVenue(t, h.pool, seed{id: vid(220), name: "Expired Slot", status: "pending", submitter: ptr(userA), lat: 50, lon: 40, created: submitNow.Add(-24 * time.Hour)})

	tenth, err := submit(h, userA, "quota-key-10", rawVenue("Tenth", 60, 40, punk))
	if err != nil {
		t.Fatalf("tenth %v", err)
	}
	if tokensLeft(h.limiter, userA) != 59 {
		t.Fatal("tenth token")
	}
	_, err = submit(h, userA, "quota-key-11", rawVenue("Eleventh", 61, 40, punk))
	var quota *QuotaError
	if !errors.As(err, &quota) || quota.RetryAfter != 3600 {
		t.Fatalf("eleventh %v", err)
	}
	if tokensLeft(h.limiter, userA) != 59 || countRows(t, h, `select count(*) from venues where name = 'Eleventh'`) != 0 {
		t.Fatal("quota failure spent a token or wrote a row")
	}
	if countRows(t, h, `select count(*) from idempotency_records where key = 'quota-key-11'`) != 0 {
		t.Fatal("quota failure recorded idempotency")
	}
	replay, err := submit(h, userA, "quota-key-10", rawVenue("Tenth", 60, 40, punk))
	if err != nil || replay.ID != tenth.ID || tokensLeft(h.limiter, userA) != 59 {
		t.Fatalf("replay %+v %v", replay, err)
	}
}

func TestSubmitIdempotency(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	punk := genreID(t, h.pool, "Punk")
	first, err := submit(h, userA, "same-key-1", rawVenue("Echo Bar", 70, 10, punk))
	if err != nil {
		t.Fatal(err)
	}
	replay, err := submit(h, userA, "same-key-1", rawVenue("Echo Bar", 70, 10, punk))
	if err != nil || replay.ID != first.ID || !replay.UpdatedAt.Equal(first.UpdatedAt) || !replay.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("replay %+v %v", replay, err)
	}
	if _, err := submit(h, userA, "same-key-1", rawVenue("Other Bar", 71, 10, punk)); !errors.Is(err, idempotency.ErrReused) {
		t.Fatalf("reused %v", err)
	}
	if _, err := submit(h, userA, "short", rawVenue("Key Bar", 72, 10, punk)); !errors.Is(err, idempotency.ErrInvalid) {
		t.Fatalf("invalid key %v", err)
	}
	bad := rawVenue("", 73, 10, punk)
	var validation *ValidationError
	if _, err := submit(h, userA, "bad-key-01", bad); !errors.As(err, &validation) {
		t.Fatalf("validation %v", err)
	}
	if _, err := submit(h, userA, "bad-key-02", rawVenue("Ghost Genre", 74, 10, 999999)); !errors.As(err, &validation) || validation.Fields["genre_ids"] != "must be an existing genre" {
		t.Fatalf("genre %v", err)
	}
	if countRows(t, h, `select count(*) from idempotency_records where key like 'bad-key-%'`) != 0 || countRows(t, h, `select count(*) from venues where submitted_by = $1`, userA) != 1 {
		t.Fatal("failed request left rows")
	}
	if tokensLeft(h.limiter, userA) != 59 {
		t.Fatal("replay or failure spent a token")
	}
}

func TestSubmitContributionRules(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	punk := genreID(t, h.pool, "Punk")
	noProfile := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	renamed := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	suspended := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	expired := "ffffffff-ffff-4fff-8fff-ffffffffffff"
	until := submitNow.Add(time.Hour)
	past := submitNow.Add(-time.Hour)
	insertUser(t, h.pool, noProfile, "", nil)
	insertUser(t, h.pool, renamed, "rename_required", nil)
	insertUser(t, h.pool, suspended, "suspended", &until)
	insertUser(t, h.pool, expired, "suspended", &past)

	if _, err := submit(h, noProfile, "role-key-1", rawVenue("A", 80, 1, punk)); !errors.Is(err, profile.ErrProfileRequired) {
		t.Fatalf("no profile %v", err)
	}
	if _, err := submit(h, renamed, "role-key-2", rawVenue("B", 80, 2, punk)); !errors.Is(err, profile.ErrRenameRequired) {
		t.Fatalf("renamed %v", err)
	}
	var blocked *profile.SuspendedError
	if _, err := submit(h, suspended, "role-key-3", rawVenue("C", 80, 3, punk)); !errors.As(err, &blocked) || blocked.Until == nil || !blocked.Until.Equal(until) {
		t.Fatalf("suspended %v", err)
	}
	if _, err := submit(h, expired, "role-key-4", rawVenue("D", 80, 4, punk)); err != nil {
		t.Fatalf("expired suspension %v", err)
	}
	var status string
	if err := h.pool.QueryRow(ctx, `select status from profiles where id = $1`, expired).Scan(&status); err != nil || status != "active" {
		t.Fatalf("restored %s %v", status, err)
	}
	if countRows(t, h, `select count(*) from venues`) != 1 {
		t.Fatal("blocked caller wrote a venue")
	}
	catalogueOnly := New(h.pool, nil, nil, time.Now)
	if _, err := catalogueOnly.Submit(ctx, SubmitCommand{UserID: expired, Key: "role-key-5", Fields: rawVenue("E", 80, 5, punk)}); !errors.Is(err, errWritesUnavailable) {
		t.Fatalf("catalogue-only service %v", err)
	}
}

func correct(h *harness, user, venueID string, raw RawSubmission) (Submission, error) {
	return h.service.Correct(context.Background(), CorrectCommand{UserID: user, VenueID: venueID, Fields: raw})
}

func TestCorrectPendingSubmission(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	punk := genreID(t, h.pool, "Punk")
	classic := genreID(t, h.pool, "Classic rock")
	created, err := submit(h, userA, "fix-key-01", rawVenue("Rust Bar", 15, 15, punk))
	if err != nil {
		t.Fatal(err)
	}

	renamed, err := correct(h, userA, created.ID, rawVenue("Rust Pub", 15, 15, classic))
	if err != nil || renamed.Name != "Rust Pub" || renamed.VerificationStatus != "pending" || len(renamed.Genres) != 1 || renamed.Genres[0].ID != classic ||
		!renamed.CreatedAt.Equal(submitNow) || !renamed.UpdatedAt.After(submitNow) {
		t.Fatalf("renamed %+v %v", renamed, err)
	}
	var searchName string
	if err := h.pool.QueryRow(ctx, `select search_name from venues where id = $1`, created.ID).Scan(&searchName); err != nil || searchName != "rustpub" {
		t.Fatalf("search key %s %v", searchName, err)
	}
	if public, err := h.service.GetVenue(ctx, created.ID); err != nil || public.Name != "Rust Pub" {
		t.Fatalf("public %+v %v", public, err)
	}

	genresOnly, err := correct(h, userA, created.ID, rawVenue("Rust Pub", 15, 15, classic, punk))
	if err != nil || len(genresOnly.Genres) != 2 || !genresOnly.UpdatedAt.After(renamed.UpdatedAt) {
		t.Fatalf("genre-only %+v %v", genresOnly, err)
	}
	tokens := tokensLeft(h.limiter, userA)
	same, err := correct(h, userA, created.ID, rawVenue("Rust Pub", genresOnly.Latitude, genresOnly.Longitude, punk, classic))
	if err != nil || !same.UpdatedAt.Equal(genresOnly.UpdatedAt) || tokensLeft(h.limiter, userA) != tokens {
		t.Fatalf("unchanged %+v %v", same, err)
	}
	if tokens != 57 {
		t.Fatalf("tokens %d", tokens)
	}

	seedVenue(t, h.pool, seed{id: vid(30), name: "Taken", lat: 15.0002, lon: 15})
	var duplicate *DuplicateError
	if _, err := correct(h, userA, created.ID, rawVenue("Taken", 15, 15, punk)); !errors.As(err, &duplicate) || duplicate.Match == nil || duplicate.Match.VenueID != vid(30) {
		t.Fatalf("duplicate %v", err)
	}
	var validation *ValidationError
	if _, err := correct(h, userA, created.ID, rawVenue("Rust Pub", 15, 15, 999999)); !errors.As(err, &validation) || validation.Fields["genre_ids"] != "must be an existing genre" {
		t.Fatalf("unknown genre %v", err)
	}

	seedVenue(t, h.pool, seed{id: vid(31), name: "Verified Own", status: "verified", submitter: ptr(userA), lat: 16, lon: 15})
	seedVenue(t, h.pool, seed{id: vid(32), name: "Rejected Own", status: "rejected", submitter: ptr(userA), lat: 17, lon: 15})
	seedVenue(t, h.pool, seed{id: vid(33), name: "Orphan", status: "pending", lat: 18, lon: 15})
	for _, c := range []struct{ id, status string }{{vid(31), "verified"}, {vid(32), "rejected"}} {
		var notPending *NotPendingError
		if _, err := correct(h, userA, c.id, rawVenue("Changed", 16, 15, 999999)); !errors.As(err, &notPending) || notPending.Status != c.status {
			t.Fatalf("%s err %v", c.id, err)
		}
	}
	for _, c := range []struct{ user, id string }{{userB, created.ID}, {userA, vid(33)}, {userA, vid(99)}} {
		if _, err := correct(h, c.user, c.id, rawVenue("Changed", 19, 15, punk)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%+v err %v", c, err)
		}
	}
	if _, err := correct(h, userA, "not-a-uuid", rawVenue("Changed", 19, 15, punk)); !errors.As(err, &validation) || validation.Fields["venue_id"] != "must be a canonical uuid" {
		t.Fatalf("id %v", err)
	}
}

func TestCorrectThenReplay(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	punk := genreID(t, h.pool, "Punk")
	first, err := submit(h, userA, "keep-key-1", rawVenue("Original", 25, 25, punk))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := correct(h, userA, first.ID, rawVenue("Corrected", 25, 25, punk)); err != nil {
		t.Fatal(err)
	}
	replay, err := submit(h, userA, "keep-key-1", rawVenue("Original", 25, 25, punk))
	if err != nil || replay.Name != "Original" || !replay.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatalf("replay %+v %v", replay, err)
	}
}

func TestCorrectDoesNotTakeQuota(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	punk := genreID(t, h.pool, "Punk")
	for i := 0; i < 10; i++ {
		seedVenue(t, h.pool, seed{
			id: vid(300 + i), name: "Slot " + string(rune('A'+i)), status: "pending", submitter: ptr(userA),
			lat: float64(i), lon: 45, created: submitNow.Add(-time.Hour),
		})
	}
	if _, err := submit(h, userA, "slot-key-11", rawVenue("Over", 50, 45, punk)); err == nil {
		t.Fatal("quota not full")
	}
	var created time.Time
	if err := h.pool.QueryRow(ctx, `select created_at from venues where id = $1`, vid(300)).Scan(&created); err != nil {
		t.Fatal(err)
	}
	if _, err := correct(h, userA, vid(300), rawVenue("Slot Fixed", 0, 45, punk)); err != nil {
		t.Fatalf("correction under full quota %v", err)
	}
	var after time.Time
	if err := h.pool.QueryRow(ctx, `select created_at from venues where id = $1`, vid(300)).Scan(&after); err != nil || !after.Equal(created) {
		t.Fatalf("correction moved created_at %s %v", after, err)
	}
}

func TestCorrectContributionRules(t *testing.T) {
	h := newHarness(t)
	renamed := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	insertUser(t, h.pool, renamed, "rename_required", nil)
	seedVenue(t, h.pool, seed{id: vid(40), name: "Mine", status: "pending", submitter: ptr(renamed), lat: 5, lon: 5})
	if _, err := correct(h, renamed, vid(40), rawVenue("Mine Too", 5, 5, genreID(t, h.pool, "Punk"))); !errors.Is(err, profile.ErrRenameRequired) {
		t.Fatalf("renamed %v", err)
	}
	if got, err := h.service.GetOwn(context.Background(), renamed, vid(40)); err != nil || got.Name != "Mine" {
		t.Fatalf("read while renamed %+v %v", got, err)
	}
}
