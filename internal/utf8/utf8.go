package utf8

import (
	"fmt"
	"log"
	"strings"
	"unicode"
)

func runeToHex(r rune) string {
	return strings.ToUpper(fmt.Sprintf("%x", r))
}

func isValid(runes []rune, pos int) bool {
	r := runes[pos]
	log.Printf("r %+v\n", r)
	return unicode.IsPrint(r) &&
		(isValidRange(r) ||
			isValidEmoji(r) ||
			isSkinTonesEmoji(runes, pos))
}

func Normalize(s string) string {
	normalized := make([]rune, 0)
	runes := []rune(s)
	for pos, r := range runes {
		if isValid(runes, pos) {
			normalized = append(normalized, r)
		}
	}
	return string(normalized)
}
