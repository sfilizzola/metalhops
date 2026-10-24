package review

import (
	"errors"

	"github.com/metalhops/metalhops/backend/internal/profile"
)

var reportReasons = map[string]struct{}{
	"spam":                   {},
	"harassment":             {},
	"hate_or_discrimination": {},
	"personal_information":   {},
	"illegal_content":        {},
	"off_topic":              {},
	"other":                  {},
}

var moderationActions = map[string]struct{}{
	"remove_commentary":           {},
	"delete_review":               {},
	"lift_commentary_restriction": {},
}

func NormalizeReport(reason string, note *string) (string, *string, error) {
	if _, ok := reportReasons[reason]; !ok {
		return "", nil, &ValidationError{Fields: map[string]string{"reason": "must be a known reason"}}
	}
	normalized, err := optionalNote(note)
	if err != nil {
		return "", nil, err
	}
	if reason == "other" && normalized == nil {
		return "", nil, &ValidationError{Fields: map[string]string{"note": "required"}}
	}
	return reason, normalized, nil
}

func NormalizeAction(action, reason string, note *string) (string, string, *string, error) {
	if _, ok := moderationActions[action]; !ok {
		return "", "", nil, &ValidationError{Fields: map[string]string{
			"action": "must be remove_commentary, delete_review, or lift_commentary_restriction",
		}}
	}
	if action == "lift_commentary_restriction" {
		if reason != "" {
			return "", "", nil, &ValidationError{Fields: map[string]string{"reason": "must be omitted"}}
		}
		if note != nil {
			return "", "", nil, &ValidationError{Fields: map[string]string{"note": "must be omitted"}}
		}
		return action, "", nil, nil
	}
	reason, note, err := NormalizeReport(reason, note)
	if err != nil {
		return "", "", nil, err
	}
	return action, reason, note, nil
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

func normalizeNote(value string) (string, error) {
	note, err := profile.NormalizeNote(value)
	var invalid *profile.ValidationError
	if errors.As(err, &invalid) {
		return "", &ValidationError{Fields: invalid.Fields}
	}
	return note, err
}
