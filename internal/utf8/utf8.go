package utf8

import (
	"unicode"
)

func isValid(r rune) bool {
	return unicode.IsPrint(r) &&
		(isValidRange(r) ||
			isValidEmoji(r) ||
			isValidSkinTones(r) ||
			isValidSmiley(r))
}

func Normalize(s string) string {
	normalized := make([]rune, 0)
	for _, r := range s {
		if isValid(r) {
			normalized = append(normalized, r)
		}
	}
	return string(normalized)
}
