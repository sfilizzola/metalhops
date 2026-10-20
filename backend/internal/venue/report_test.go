package venue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/metalhops/metalhops/backend/internal/profile"
)

func report(h *harness, user, venueID, key, reason string, note *string) (VenueReport, error) {
	return h.service.ReportVenue(context.Background(), ReportCommand{ReporterID: user, VenueID: venueID, Reason: reason, Note: note, Key: key})
}

func TestReportVenue(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(80), name: "Open", status: "verified", submitter: ptr(userA), lat: 70, lon: 10})
	seedVenue(t, h.pool, seed{id: vid(81), name: "Pending OK", status: "pending", lat: 71, lon: 10})
	seedVenue(t, h.pool, seed{id: vid(82), name: "Rejected", status: "rejected", submitter: ptr(userB), lat: 72, lon: 10})
	if _, err := h.pool.Exec(context.Background(), `
		insert into venues (id, name, location, address, city, country_code, venue_type, verification_status, delisting_reason)
		values ($1, 'Delisted', ST_MakePoint(10, 73)::geography, '1 Street', 'Oslo', 'NO', 'pub', 'delisted', 'duplicate')`, vid(83)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `
		insert into venues (id, name, location, address, city, country_code, venue_type, verification_status)
		values ($1, 'No genre', ST_MakePoint(10, 74)::geography, '1 Street', 'Oslo', 'NO', 'pub', 'pending')`, vid(84)); err != nil {
		t.Fatal(err)
	}

	got, err := report(h, userB, vid(80), "report-key-01", "moved", nil)
	if err != nil || got.Status != "pending" || got.VenueID != vid(80) {
		t.Fatalf("report %+v %v", got, err)
	}
	if _, err := report(h, userB, vid(81), "report-key-02", "other", ptr("closed down")); err != nil {
		t.Fatalf("pending venue: %v", err)
	}
	// Repeating the same key replays; a second pending report with a new key conflicts.
	if replay, err := report(h, userB, vid(80), "report-key-01", "moved", nil); err != nil || replay.ID != got.ID {
		t.Fatalf("replay %+v %v", replay, err)
	}
	if _, err := report(h, userB, vid(80), "report-key-03", "duplicate", nil); !errors.Is(err, ErrReportPending) {
		t.Fatalf("second report: %v", err)
	}
	if _, err := report(h, userA, vid(80), "report-key-04", "moved", nil); !errors.Is(err, ErrSelfReport) {
		t.Fatalf("self report: %v", err)
	}
	for _, id := range []string{vid(82), vid(83), vid(84), vid(85)} {
		if _, err := report(h, userA, id, "hidden-"+id[len(id)-2:]+"-key", "moved", nil); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s: %v", id, err)
		}
	}
	// Own rejected Venue: the catalogue check comes first.
	if _, err := report(h, userB, vid(82), "hidden-own-key1", "moved", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("own rejected: %v", err)
	}
	if _, err := report(h, userB, vid(81), "report-key-05", "incorrect_details", nil); err == nil {
		t.Fatal("missing note accepted")
	}
	var invalid *ValidationError
	if _, err := report(h, userA, vid(81), "report-key-07", "other", ptr(" \t\n ")); !errors.As(err, &invalid) || invalid.Fields["note"] == "" {
		t.Fatalf("whitespace note: %v", err)
	}
	if _, err := report(h, userB, "nope", "report-key-06", "moved", nil); err == nil {
		t.Fatal("bad venue id accepted")
	}
}

func TestReportQuotaIsShared(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(86), name: "Quota", status: "verified", submitter: ptr(userA), lat: 75, lon: 10})
	if _, err := h.pool.Exec(context.Background(), `
		insert into report_quota_events (user_id, created_at) select $1, $2 from generate_series(1, 20)`, userB, submitNow.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	var quota *profile.QuotaError
	if _, err := report(h, userB, vid(86), "quota-key-001", "moved", nil); !errors.As(err, &quota) {
		t.Fatalf("want quota error, got %v", err)
	}
}

// A report racing a delist waits on the Venue row lock and then sees the new status.
func TestReportWaitsForDelist(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userB, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(87), name: "Racing", status: "verified", lat: 79, lon: 10})
	ctx := context.Background()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		update venues set verification_status = 'delisted', delisting_reason = 'duplicate' where id = $1`, vid(87)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := report(h, userB, vid(87), "race-key-0001", "moved", nil)
		done <- err
	}()
	deadline := time.Now().Add(1500 * time.Millisecond)
	for {
		var waiting bool
		if err := h.pool.QueryRow(ctx, `
			select exists (select 1 from pg_stat_activity where wait_event_type = 'Lock' and query like '%for share of v%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("report did not wait for the Venue row lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrNotFound) {
		t.Fatalf("report after delist: %v", err)
	}
}

func TestListAndDismissReports(t *testing.T) {
	h := newHarness(t)
	insertOperator(t, h)
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(90), name: "Listed", status: "verified", submitter: ptr(userA), lat: 76, lon: 10})
	seedVenue(t, h.pool, seed{id: vid(91), name: "Orphan Venue", status: "verified", lat: 77, lon: 10})
	for _, id := range []string{vid(90), vid(91)} {
		if _, err := report(h, userB, id, "list-key-"+id[len(id)-2:]+"-x", "moved", nil); err != nil {
			t.Fatal(err)
		}
	}
	// Both reports share the fixed clock; order them by created_at rather than by random id.
	if _, err := h.pool.Exec(context.Background(), `
		update venue_reports set created_at = created_at + interval '1 minute' where venue_id = $1`, vid(91)); err != nil {
		t.Fatal(err)
	}
	page, err := h.service.ListVenueReports(context.Background(), 1, "")
	if err != nil || len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatalf("page 1 %+v %v", page, err)
	}
	if page.Items[0].Venue.ID != vid(90) || page.Items[0].Reporter == nil || page.Items[0].Submitter == nil || page.Items[0].Submitter.ID != userA {
		t.Fatalf("item %+v", page.Items[0])
	}
	second, err := h.service.ListVenueReports(context.Background(), 1, *page.NextCursor)
	if err != nil || len(second.Items) != 1 || second.NextCursor != nil || second.Items[0].Submitter != nil {
		t.Fatalf("orphan submitter %+v %v", second, err)
	}
	if _, err := h.service.ListVenueReports(context.Background(), 0, ""); err == nil {
		t.Fatal("limit 0 accepted")
	}
	if _, err := h.service.ListVenueReports(context.Background(), 5, "garbage"); err == nil {
		t.Fatal("bad cursor accepted")
	}

	item, err := h.service.DismissVenueReport(context.Background(), operatorID, page.Items[0].ID)
	if err != nil || item.Status != "dismissed" {
		t.Fatalf("dismiss %+v %v", item, err)
	}
	if again, err := h.service.DismissVenueReport(context.Background(), operatorID, page.Items[0].ID); err != nil || again.Status != "dismissed" {
		t.Fatalf("dismiss twice %+v %v", again, err)
	}
	if _, err := h.service.DismissVenueReport(context.Background(), operatorID, "nope"); err == nil {
		t.Fatal("bad id accepted")
	}
	if _, err := h.service.DismissVenueReport(context.Background(), operatorID, vid(1)); !errors.Is(err, ErrReportNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := act(h, vid(91), "delist", "list-delist-1", "duplicate", nil); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := h.pool.QueryRow(context.Background(), `select id::text from venue_reports where venue_id = $1`, vid(91)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.DismissVenueReport(context.Background(), operatorID, id); !errors.Is(err, ErrReportResolved) {
		t.Fatalf("dismiss actioned: %v", err)
	}
	// After the delist the Venue is out of the catalogue, so a new report is 404.
	if _, err := report(h, userA, vid(91), "list-after-delist", "moved", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("report delisted: %v", err)
	}
}

func TestListReportsDeletedSubmitter(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(92), name: "Left", status: "verified", submitter: ptr(userA), lat: 78, lon: 10})
	if _, err := report(h, userB, vid(92), "deleted-key-01", "moved", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `delete from auth.users where id = $1`, userA); err != nil {
		t.Fatal(err)
	}
	page, err := h.service.ListVenueReports(context.Background(), 5, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].Submitter != nil || page.Items[0].Venue.Name != "Left" {
		t.Fatalf("%+v %v", page, err)
	}
	// A reporter whose Profile is gone still lists, with a null reporter.
	if _, err := h.pool.Exec(context.Background(), `delete from profiles where id = $1`, userB); err != nil {
		t.Fatal(err)
	}
	page, err = h.service.ListVenueReports(context.Background(), 5, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].Reporter != nil {
		t.Fatalf("%+v %v", page, err)
	}
}
