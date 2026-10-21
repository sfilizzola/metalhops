package venue

import (
	"strings"
	"testing"
)

func TestNormalizeModeration(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000001"
	long := strings.Repeat("x", 1001)
	cases := []struct {
		name   string
		cmd    ModerationCommand
		field  string
		action string
		to     string
	}{
		{"verify", ModerationCommand{VenueID: id, Action: "verify"}, "", "verify", "verified"},
		{"reopen", ModerationCommand{VenueID: id, Action: "reopen"}, "", "reopen", "verified"},
		{"reject", ModerationCommand{VenueID: id, Action: "reject", Reason: "duplicate", ReasonSet: true}, "", "reject", "rejected"},
		{"delist", ModerationCommand{VenueID: id, Action: "delist", Reason: "permanently_closed", ReasonSet: true}, "", "delist", "delisted"},
		{"unknown action", ModerationCommand{VenueID: id, Action: "ban"}, "action", "", ""},
		{"bad id", ModerationCommand{VenueID: "nope", Action: "verify"}, "venue_id", "", ""},
		{"verify with reason", ModerationCommand{VenueID: id, Action: "verify", Reason: "duplicate", ReasonSet: true}, "reason", "", ""},
		{"reopen with note", ModerationCommand{VenueID: id, Action: "reopen", Note: ptr("x"), NoteSet: true}, "note", "", ""},
		{"reject without reason", ModerationCommand{VenueID: id, Action: "reject"}, "reason", "", ""},
		{"reject with delisting reason", ModerationCommand{VenueID: id, Action: "reject", Reason: "moved", ReasonSet: true}, "reason", "", ""},
		{"delist with rejection reason", ModerationCommand{VenueID: id, Action: "delist", Reason: "cannot_verify", ReasonSet: true}, "reason", "", ""},
		{"reject other needs note", ModerationCommand{VenueID: id, Action: "reject", Reason: "other", ReasonSet: true}, "note", "", ""},
		{"note too long", ModerationCommand{VenueID: id, Action: "delist", Reason: "duplicate", ReasonSet: true, Note: &long, NoteSet: true}, "note", "", ""},
		{"whitespace note", ModerationCommand{VenueID: id, Action: "reject", Reason: "duplicate", ReasonSet: true, Note: ptr("   "), NoteSet: true}, "note", "", ""},
	}
	for _, c := range cases {
		state, err := normalizeModeration(c.cmd)
		if c.field != "" {
			if _, ok := fieldsOf(t, err)[c.field]; !ok {
				t.Fatalf("%s: want field %s, got %v", c.name, c.field, err)
			}
			continue
		}
		if err != nil || state.action != c.action || state.to != c.to {
			t.Fatalf("%s: %+v %v", c.name, state, err)
		}
	}
	state, err := normalizeModeration(ModerationCommand{VenueID: id, Action: "reject", Reason: "other", ReasonSet: true, Note: ptr("  Not a venue  "), NoteSet: true})
	if err != nil || state.note == nil || *state.note != "Not a venue" {
		t.Fatalf("note trimming %+v %v", state, err)
	}
}

func TestNormalizeVenueReport(t *testing.T) {
	for _, c := range []struct {
		reason string
		note   *string
		field  string
	}{
		{"moved", nil, ""},
		{"duplicate", ptr("same bar"), ""},
		{"incorrect_details", nil, "note"},
		{"other", nil, "note"},
		{"other", ptr("   "), "note"},
		{"spam", nil, "reason"},
		{"", nil, "reason"},
	} {
		_, _, err := normalizeVenueReport(c.reason, c.note)
		if c.field == "" && err != nil {
			t.Fatalf("%s: %v", c.reason, err)
		}
		if c.field != "" {
			if _, ok := fieldsOf(t, err)[c.field]; !ok {
				t.Fatalf("%s: want field %s, got %v", c.reason, c.field, err)
			}
		}
	}
	_, note, err := normalizeVenueReport("moved", ptr("  now at 5th st "))
	if err != nil || note == nil || *note != "now at 5th st" {
		t.Fatalf("trim %v %v", note, err)
	}
}
