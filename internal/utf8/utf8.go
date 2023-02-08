package utf8

import (
	"fmt"
	"strings"
	"unicode"
)

func runeToHex(r rune) string {
	return strings.ToUpper(fmt.Sprintf("%x", r))
}

func consumeRunes(runes []rune, pos int) (*rune, int) {
	r := runes[pos]
	if !unicode.IsPrint(r) {
		return nil, 0
	}
	if isValidRange(r) {
		return &r, 0
	}
	return unicode.IsPrint(r) &&
		(isValidRange(r) ||
			isValidEmoji(r) ||
			isSkinTonesEmoji(runes, pos))
}

func Normalize(s string) string {
	normalized := make([]rune, 0)
	runes := []rune(s)
	for pos, r := range runes {
		if consumed := consumeRunes(runes, pos); consumed > 0 {
			normalized = append(normalized, r)
		}
	}
	return string(normalized)
}
