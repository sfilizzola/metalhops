package profile

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,30}$`)

var reservedBases = []string{
	"admin", "administrator", "mod", "moderator", "metalhops", "support", "staff", "official",
}

func normalizeUsername(value string) (string, error) {
	if !usernamePattern.MatchString(value) {
		return "", &ValidationError{Fields: map[string]string{
			"username": "must be 3 to 30 letters, digits, or underscores",
		}}
	}
	if reservedUsername(value) {
		return "", ErrUsernameReserved
	}
	return value, nil
}

func normalizeDisplayName(value string) (string, error) {
	if !utf8.ValidString(value) || lineOrControl(value) {
		return "", &ValidationError{Fields: map[string]string{"display_name": "must be valid text"}}
	}
	value = norm.NFC.String(strings.TrimSpace(value))
	if value == "" || utf8.RuneCountInString(value) > 80 || controlText(value) {
		return "", &ValidationError{Fields: map[string]string{
			"display_name": "must be 1 to 80 characters on one line",
		}}
	}
	return value, nil
}

func lineOrControl(value string) bool {
	for _, r := range value {
		if r == '\n' || r == '\r' || r == 0 || (unicode.IsControl(r) && !unicode.IsSpace(r)) {
			return true
		}
	}
	return false
}

func controlText(value string) bool {
	return lineOrControl(value)
}

func reservedUsername(value string) bool {
	var chars []rune
	for _, r := range strings.ToLower(value) {
		if r == '_' {
			continue
		}
		chars = append(chars, r)
	}
	for _, word := range reservedBases {
		if skeletonEquals(chars, word) {
			return true
		}
	}
	return skeletonContains(chars, "metalhops")
}

func skeletonEquals(chars []rune, word string) bool {
	states := map[int]bool{0: true}
	for _, ch := range chars {
		states = stepSkeleton(states, word, ch, false)
	}
	return states[len(word)]
}

func skeletonContains(chars []rune, word string) bool {
	states := map[int]bool{0: true}
	for _, ch := range chars {
		states = stepSkeleton(states, word, ch, true)
		if states[len(word)] {
			return true
		}
	}
	return false
}

func stepSkeleton(states map[int]bool, word string, ch rune, restart bool) map[int]bool {
	next := map[int]bool{}
	if restart {
		next[0] = true
	}
	for _, option := range mappedRunes(ch) {
		for n := range states {
			if n < len(word) && rune(word[n]) == option {
				next[n+1] = true
			}
		}
		if restart && word != "" && rune(word[0]) == option {
			next[1] = true
		}
	}
	return next
}

func mappedRunes(ch rune) []rune {
	switch ch {
	case '0':
		return []rune{'o'}
	case '1':
		return []rune{'i', 'l'}
	case '3':
		return []rune{'e'}
	case '4':
		return []rune{'a'}
	case '5':
		return []rune{'s'}
	case '7':
		return []rune{'t'}
	default:
		return []rune{ch}
	}
}
