package checkin

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func (h *createHarness) listOwn(userID string, limit int, cursor string) (Page, error) {
	return h.service.ListOwn(context.Background(), ListQuery{UserID: userID, Limit: limit, Cursor: cursor})
}

func TestHistoryPagination(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	insertVenue(t, h.pool, pendingVenue, "pending")
	insertVenue(t, h.pool, delistedVenue, "delisted")
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	insertUser(t, h.pool, userC, "", nil)

	created, err := h.create(userA, verifiedVenue, "history-key-1", visitedAtJSON(t0.Add(-time.Hour+123456789*time.Nanosecond)))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	tiedAt := t0.Add(-2 * time.Hour)
	tied := []string{
		insertCheckIn(t, h.pool, pendingVenue, userA, tiedAt),
		insertCheckIn(t, h.pool, delistedVenue, userA, tiedAt),
	}
	if tied[0] < tied[1] {
		tied[0], tied[1] = tied[1], tied[0]
	}
	other := insertCheckIn(t, h.pool, verifiedVenue, userB, t0.Add(-90*time.Minute))

	first, err := h.listOwn(userA, 1, "")
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.CheckIns) != 1 || first.CheckIns[0].ID != created.ID || first.NextCursor == nil {
		t.Fatalf("first page %+v", first)
	}
	if want := t0.Add(-time.Hour + 123456000*time.Nanosecond); !first.CheckIns[0].VisitedAt.Equal(want) {
		t.Fatalf("visited_at %s want %s", first.CheckIns[0].VisitedAt, want)
	}

	second, err := h.listOwn(userA, 2, *first.NextCursor)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.CheckIns) != 2 || second.CheckIns[0].ID != tied[0] || second.CheckIns[1].ID != tied[1] || second.NextCursor != nil {
		t.Fatalf("second page %+v want %v", second, tied)
	}
	for _, page := range []Page{first, second} {
		for _, c := range page.CheckIns {
			if c.ID == other {
				t.Fatalf("userB row leaked")
			}
		}
	}

	empty, err := h.listOwn(userC, 20, "")
	if err != nil || empty.CheckIns == nil || len(empty.CheckIns) != 0 || empty.NextCursor != nil {
		t.Fatalf("no profile %+v %v", empty, err)
	}
}

func TestHistorySummaries(t *testing.T) {
	h := newCreateHarness(t)
	statuses := map[string]string{
		verifiedVenue: "verified", pendingVenue: "pending", rejectedVenue: "rejected", delistedVenue: "delisted",
	}
	insertUser(t, h.pool, userA, "active", nil)
	ids := map[string]string{}
	i := 0
	for venueID, status := range statuses {
		insertVenue(t, h.pool, venueID, status)
		ids[venueID] = insertCheckIn(t, h.pool, venueID, userA, t0.Add(-time.Duration(i+1)*time.Hour))
		i++
	}

	page, err := h.listOwn(userA, 20, "")
	if err != nil || len(page.CheckIns) != 4 {
		t.Fatalf("list %+v %v", page, err)
	}
	for _, c := range page.CheckIns {
		want := statuses[c.VenueID]
		if c.ID != ids[c.VenueID] || c.Venue.VerificationStatus != want {
			t.Fatalf("row %+v", c)
		}
		hidden := want == "rejected" || want == "delisted"
		if hidden != (c.Venue.VenueType == nil) {
			t.Fatalf("%s venue type %v", want, c.Venue.VenueType)
		}
		if !hidden && *c.Venue.VenueType != "bar" {
			t.Fatalf("%s venue type %s", want, *c.Venue.VenueType)
		}
	}

	if _, err := h.pool.Exec(context.Background(), `
		update venues set verification_status = 'verified', delisting_reason = null, delisting_note = null where id = $1`, delistedVenue); err != nil {
		t.Fatal(err)
	}
	page, err = h.listOwn(userA, 20, "")
	if err != nil {
		t.Fatalf("relist: %v", err)
	}
	for _, c := range page.CheckIns {
		if c.ID == ids[delistedVenue] && (c.Venue.VenueType == nil || *c.Venue.VenueType != "bar" || c.Venue.VerificationStatus != "verified") {
			t.Fatalf("restored row %+v", c)
		}
	}
}

func TestHistoryReadableWhenRestricted(t *testing.T) {
	h := newCreateHarness(t)
	insertVenue(t, h.pool, verifiedVenue, "verified")
	future := t0.Add(time.Hour)
	insertUser(t, h.pool, userA, "suspended", &future)
	insertUser(t, h.pool, userB, "rename_required", nil)
	for _, userID := range []string{userA, userB} {
		id := insertCheckIn(t, h.pool, verifiedVenue, userID, t0.Add(-time.Hour))
		page, err := h.listOwn(userID, 20, "")
		if err != nil || len(page.CheckIns) != 1 || page.CheckIns[0].ID != id {
			t.Fatalf("%s list %+v %v", userID, page, err)
		}
		if left := tokensLeft(h.limiter, userID); left != 60 {
			t.Fatalf("%s tokens %d", userID, left)
		}
	}
}

func TestHistoryPlan(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	insertVenue(t, pool, verifiedVenue, "verified")
	if _, err := pool.Exec(ctx, `insert into auth.users (id) select gen_random_uuid() from generate_series(1, 100)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		with users as (select id, row_number() over (order by id) - 1 as n from auth.users)
		insert into check_ins (venue_id, user_id, visited_at, recorded_at)
		select $1::uuid, users.id, now() - (i || ' minutes')::interval, now() - (i || ' minutes')::interval
		from generate_series(0, 4999) as i
		join users on users.n = i % 100`, verifiedVenue); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "vacuum analyze check_ins"); err != nil {
		t.Fatal(err)
	}
	var userID string
	if err := pool.QueryRow(ctx, `select user_id::text from check_ins limit 1`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if plan := explain(t, pool, historySQL, userID, nil, nil, 21); !strings.Contains(plan, "check_ins_user_history_idx") {
		t.Fatalf("history plan does not use check_ins_user_history_idx:\n%s", plan)
	}
}

func explain(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	rows, err := pool.Query(context.Background(), "explain "+sql, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return plan.String()
}
