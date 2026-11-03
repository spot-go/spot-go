package utf8

import "testing"

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"ascii", "Rock Classics", "Rock Classics"},
		{"latin accents", "Música Brasileira ção", "Música Brasileira ção"},
		{"control chars", "Line\nBreak\tTab\x00", "LineBreakTab"},
		{"unsupported script", "Japan 日本", "Japan "},
		{"single emoji", "Party 😀", "Party 😀"},
		{"skin tone at end", "Hi 👋🏻", "Hi 👋🏻"},
		{"skin tone in middle", "👋🏽 hello", "👋🏽 hello"},
		{"lone skin tone modifier", "🏿", "🏿"},
		{"couple with heart longest match", "👩🏾‍❤️‍👩🏼", "👩🏾‍❤️‍👩🏼"},
		{"longest emoji sequence", "\U0001F469‍❤️‍\U0001F48B‍\U0001F468", "\U0001F469‍❤️‍\U0001F48B‍\U0001F468"},
		{"longest skin tones sequence", "\U0001F469\U0001F3FB‍❤️‍\U0001F48B‍\U0001F468\U0001F3FC", "\U0001F469\U0001F3FB‍❤️‍\U0001F48B‍\U0001F468\U0001F3FC"},
		{"heart on fire", "❤️‍🔥 love", "❤️‍🔥 love"},
		{"keycap", "#️⃣ top", "#️⃣ top"},
		{"copyright", "© 2023", "© 2023"},
		{"country flag", "🇧🇷 Brasil", "🇧🇷 Brasil"},
		{"subdivision flag", "🏴󠁧󠁢󠁥󠁮󠁧󠁿", "🏴󠁧󠁢󠁥󠁮󠁧󠁿"},
		{"stray zero width joiner", "a‍b", "ab"},
		{"stray variation selector", "a️b", "ab"},
		{"unknown emoji sequence keeps known parts", "😀‍😀", "😀😀"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Normalize(tt.in); got != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRuneToHex(t *testing.T) {
	tests := map[rune]string{
		'#':     "0023",
		'©':     "00A9",
		0x200D:  "200D",
		0x1F600: "1F600",
		0xE0067: "E0067",
	}
	for in, want := range tests {
		if got := runeToHex(in); got != want {
			t.Errorf("runeToHex(%U) = %q, want %q", in, got, want)
		}
	}
}
