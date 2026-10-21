package venue

import (
	"errors"
	"fmt"
	"time"

	"github.com/metalhops/metalhops/backend/internal/profile"
)

type ModerationCommand struct {
	ActorID   string
	VenueID   string
	Action    string
	Reason    string
	ReasonSet bool
	Note      *string
	NoteSet   bool
	Key       string
}

type Moderated struct {
	Submission
	DelistingReason *string
	DelistingNote   *string
}

type ModerationResult struct {
	Created  bool
	ActionID *string
	Venue    Moderated
}

type PendingReport struct {
	ID        string
	Reason    string
	Note      *string
	CreatedAt time.Time
	Reporter  *profile.Profile
}

// ModerationAction is one audit row. ActorUserID is null once that User is deleted.
type ModerationAction struct {
	ID              string
	Action          string
	PriorStatus     string
	NewStatus       string
	RejectionReason *string
	RejectionNote   *string
	DelistingReason *string
	DelistingNote   *string
	ActorUserID     *string
	CreatedAt       time.Time
}

type ModerationVenue struct {
	Moderated
	Submitter *profile.Profile
	Reports   []PendingReport
	History   []ModerationAction
}

type OperatorCorrectCommand struct {
	ActorID string
	VenueID string
	Fields  RawSubmission
}

type ReportCommand struct {
	ReporterID string
	VenueID    string
	Reason     string
	Note       *string
	Key        string
}

type VenueReport struct {
	ID        string
	VenueID   string
	Reason    string
	Note      *string
	Status    string
	CreatedAt time.Time
}

type VenueSummary struct {
	ID                 string
	Name               string
	City               string
	CountryCode        string
	VerificationStatus string
}

type ReportItem struct {
	ID        string
	Reason    string
	Note      *string
	Status    string
	CreatedAt time.Time
	Reporter  *profile.Profile
	Venue     VenueSummary
	Submitter *profile.Profile
}

type ReportPage struct {
	Items      []ReportItem
	NextCursor *string
}

type InvalidTransitionError struct{ Action, Status string }

func (e *InvalidTransitionError) Error() string { return "venue: invalid transition" }

var (
	ErrSelfReport     = errors.New("venue: self report")
	ErrReportPending  = errors.New("venue: report pending")
	ErrReportNotFound = errors.New("venue: report not found")
	ErrReportResolved = errors.New("venue: report resolved")
	// ErrIncomplete refuses verify and reopen for a Venue with no Genre.
	ErrIncomplete = errors.New("venue: incomplete")
)

var rejectionReasons = set("duplicate", "not_eligible", "inaccurate_details", "insufficient_information", "cannot_verify", "other")
var delistingReasons = set("permanently_closed", "duplicate", "no_longer_eligible", "other")
var reportReasons = set("permanently_closed", "moved", "duplicate", "incorrect_details", "no_longer_eligible", "other")

func set(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, v := range values {
		out[v] = struct{}{}
	}
	return out
}

var transitions = map[string]struct{ from, to string }{
	"verify": {"pending", "verified"},
	"reject": {"pending", "rejected"},
	"delist": {"verified", "delisted"},
	"reopen": {"delisted", "verified"},
}

type moderationState struct {
	action, from, to string
	reason, note     *string
}

func normalizeModeration(cmd ModerationCommand) (moderationState, error) {
	if !CanonicalVenueID(cmd.VenueID) {
		return moderationState{}, &ValidationError{Fields: map[string]string{"venue_id": "must be a canonical uuid"}}
	}
	t, ok := transitions[cmd.Action]
	if !ok {
		return moderationState{}, &ValidationError{Fields: map[string]string{"action": "must be verify, reject, delist, or reopen"}}
	}
	state := moderationState{action: cmd.Action, from: t.from, to: t.to}
	switch cmd.Action {
	case "verify", "reopen":
		fields := map[string]string{}
		if cmd.ReasonSet {
			fields["reason"] = "must be omitted"
		}
		if cmd.NoteSet {
			fields["note"] = "must be omitted"
		}
		if len(fields) > 0 {
			return moderationState{}, &ValidationError{Fields: fields}
		}
	case "reject":
		reason, note, err := reasoned(cmd.Reason, cmd.ReasonSet, cmd.Note, rejectionReasons)
		if err != nil {
			return moderationState{}, err
		}
		state.reason, state.note = reason, note
	case "delist":
		reason, note, err := reasoned(cmd.Reason, cmd.ReasonSet, cmd.Note, delistingReasons)
		if err != nil {
			return moderationState{}, err
		}
		state.reason, state.note = reason, note
	}
	return state, nil
}

func reasoned(reason string, provided bool, rawNote *string, allowed map[string]struct{}) (*string, *string, error) {
	if !provided {
		return nil, nil, &ValidationError{Fields: map[string]string{"reason": "required"}}
	}
	if _, ok := allowed[reason]; !ok {
		return nil, nil, &ValidationError{Fields: map[string]string{"reason": "must be a known reason"}}
	}
	note, err := optionalNote(rawNote)
	if err != nil {
		return nil, nil, err
	}
	if reason == "other" && note == nil {
		return nil, nil, &ValidationError{Fields: map[string]string{"note": "required"}}
	}
	return &reason, note, nil
}

func normalizeVenueReport(reason string, rawNote *string) (string, *string, error) {
	if _, ok := reportReasons[reason]; !ok {
		return "", nil, &ValidationError{Fields: map[string]string{"reason": "must be a known reason"}}
	}
	note, err := optionalNote(rawNote)
	if err != nil {
		return "", nil, err
	}
	if (reason == "incorrect_details" || reason == "other") && note == nil {
		return "", nil, &ValidationError{Fields: map[string]string{"note": "required"}}
	}
	return reason, note, nil
}

func optionalNote(raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	note, err := normalizeNote(*raw)
	if err != nil {
		return nil, err
	}
	return &note, nil
}

// normalizeNote reuses the Profile note rules (trim, 1 to 1000 characters) and
// reports them as a venue ValidationError.
func normalizeNote(value string) (string, error) {
	note, err := profile.NormalizeNote(value)
	var invalid *profile.ValidationError
	if errors.As(err, &invalid) {
		return "", &ValidationError{Fields: invalid.Fields}
	}
	return note, err
}

func profileColumns(alias string) string {
	return fmt.Sprintf(`%[1]s.id::text, %[1]s.username, %[1]s.display_name, %[1]s.status,
		%[1]s.moderation_reason, %[1]s.moderation_note, %[1]s.suspended_until, %[1]s.created_at, %[1]s.updated_at`, alias)
}

// nullableProfile scans a LEFT JOINed profile; every column is null when the User has no Profile.
type nullableProfile struct {
	id, username, displayName, status, reason, note *string
	until, createdAt, updatedAt                     *time.Time
}

func (n *nullableProfile) dest() []any {
	return []any{&n.id, &n.username, &n.displayName, &n.status, &n.reason, &n.note, &n.until, &n.createdAt, &n.updatedAt}
}

func (n nullableProfile) profile() *profile.Profile {
	if n.id == nil {
		return nil
	}
	p := profile.Profile{
		ID: *n.id, Username: *n.username, DisplayName: *n.displayName, Status: *n.status,
		ModerationReason: n.reason, ModerationNote: n.note, SuspendedUntil: n.until,
		CreatedAt: n.createdAt.UTC(), UpdatedAt: n.updatedAt.UTC(),
	}
	if p.SuspendedUntil != nil {
		until := p.SuspendedUntil.UTC()
		p.SuspendedUntil = &until
	}
	return &p
}
