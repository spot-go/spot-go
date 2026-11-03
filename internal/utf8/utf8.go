package utf8

import (
	"fmt"
	"unicode"
)

func runeToHex(r rune) string {
	return fmt.Sprintf("%04X", r)
}

func longestSequence(runes []rune, pos int, table map[string]Emoji, maxSize int) int {
	longest := 0
	key := ""
	for size := 1; size <= maxSize && pos+size <= len(runes); size++ {
		if size > 1 {
			key += " "
		}
		key += runeToHex(runes[pos+size-1])
		if _, found := table[key]; found {
			longest = size
		}
	}
	return longest
}

func consumeRunes(runes []rune, pos int) int {
	size := longestSequence(runes, pos, emojis, emojiMaxCodeSize)
	if skinTonesSize := longestSequence(runes, pos, skinTonesEmojis, skinTonesMaxCodeSize); skinTonesSize > size {
		size = skinTonesSize
	}
	if size > 0 {
		return size
	}

	if r := runes[pos]; unicode.IsPrint(r) && isValidRange(r) {
		return 1
	}
	return 0
}

func Normalize(s string) string {
	runes := []rune(s)
	normalized := make([]rune, 0, len(runes))
	for pos := 0; pos < len(runes); {
		size := consumeRunes(runes, pos)
		if size == 0 {
			pos++
			continue
		}
		normalized = append(normalized, runes[pos:pos+size]...)
		pos += size
	}
	return string(normalized)
}
