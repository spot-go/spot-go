package utf8

type utfrange struct {
	min, max rune
}

func (u utfrange) isIn(r rune) bool {
	return r >= u.min && r <= u.max
}

func isValidRange(r rune) bool {
	for _, u := range ranges {
		if u.isIn(r) {
			return true
		}
	}
	return false
}

var (
	// https://www.w3schools.com/charsets/ref_utf_basic_latin.asp
	latinBasic = utfrange{0, 127}
	// https://www.w3schools.com/charsets/ref_utf_latin1_supplement.asp
	latin1Supplement = utfrange{128, 255}
	// https://www.w3schools.com/charsets/ref_utf_latin_extended_a.asp
	latinExtendedA = utfrange{256, 383}
	// https://www.w3schools.com/charsets/ref_utf_latin_extended_b.asp
	latinExtendedB = utfrange{384, 591}
	// https://www.w3schools.com/charsets/ref_utf_modifiers.asp
	modifiers = utfrange{688, 767}
	// https://www.w3schools.com/charsets/ref_utf_ref_utf_diacritical.asp
	ref_utf_diacritical = utfrange{768, 879}
	// https://www.w3schools.com/charsets/ref_utf_greek.asp
	greek = utfrange{880, 1023}
	// https://www.w3schools.com/charsets/ref_utf_cyrillic.asp
	cyrillic = utfrange{1024, 1279}
	// https://www.w3schools.com/charsets/ref_utf_cyrillic_supplement.asp
	cyrillicSupplement = utfrange{1280, 1327}
	// https://www.w3schools.com/charsets/ref_utf_punctuation.asp
	generalPunctuation = utfrange{8192, 8303}
	// https://www.w3schools.com/charsets/ref_utf_currency.asp
	currency = utfrange{8352, 8399}
	// https://www.w3schools.com/charsets/ref_utf_letterlike.asp
	letterlike = utfrange{8448, 8527}
	// https://www.w3schools.com/charsets/ref_utf_arrows.asp
	arrows = utfrange{8592, 8703}
	// https://www.w3schools.com/charsets/ref_utf_math.asp
	math = utfrange{8704, 8959}
	// https://www.w3schools.com/charsets/ref_utf_box.asp
	box = utfrange{9472, 9599}
	// https://www.w3schools.com/charsets/ref_utf_block.asp
	block = utfrange{9600, 9631}
	// https://www.w3schools.com/charsets/ref_utf_geometric.asp
	geometric = utfrange{9632, 9727}
	// https://www.w3schools.com/charsets/ref_utf_symbols.asp
	symbols = utfrange{9728, 9983}
	// https://www.w3schools.com/charsets/ref_utf_dingbats.asp
	dingbats = utfrange{9984, 10175}

	ranges = []utfrange{
		latinBasic,
		latin1Supplement,
		latinExtendedA,
		latinExtendedB,
		modifiers,
		ref_utf_diacritical,
		greek,
		cyrillic,
		cyrillicSupplement,
		generalPunctuation,
		currency,
		letterlike,
		arrows,
		math,
		box,
		block,
		geometric,
		symbols,
		dingbats,
	}
)
