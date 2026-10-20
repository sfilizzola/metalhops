package venue

import (
	"errors"
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

type ModerationVenue struct {
	Moderated
	Submitter *profile.Profile
	Reports   []PendingReport
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
