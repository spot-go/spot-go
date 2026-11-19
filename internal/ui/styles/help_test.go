package styles

import (
	"image/color"
	"math"
	"testing"

	"charm.land/lipgloss/v2"
)

// luminance is the relative luminance of c, as WCAG defines it.
func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	linear := func(v uint32) float64 {
		s := float64(v) / 0xffff
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b)
}

// contrast is the contrast ratio of a and b, as WCAG defines it: from 1, the
// same color, to 21, black on white.
func contrast(a, b color.Color) float64 {
	la, lb := luminance(a), luminance(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

func TestHelpIsEasyToRead(t *testing.T) {
	defer func(dark bool) { HasDarkBackground = dark }(HasDarkBackground)

	for _, tc := range []struct {
		name        string
		dark        bool
		backgrounds []string
	}{
		{"dark", true, []string{"#000000", "#1E1E1E", "#292C33"}},
		{"light", false, []string{"#FFFFFF", "#FAFAFA", "#EEEEEE"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			HasDarkBackground = tc.dark
			h := Help()
			for _, bg := range tc.backgrounds {
				background := lipgloss.Color(bg)
				key := contrast(h.ShortKey.GetForeground(), background)
				desc := contrast(h.ShortDesc.GetForeground(), background)
				sep := contrast(h.ShortSeparator.GetForeground(), background)
				// What a key does reads as body text does (WCAG AA), and
				// the key stands out from it.
				if desc < 4.5 {
					t.Errorf("on %s, what a key does has a contrast of %.2f, want at least 4.5", bg, desc)
				}
				if key < 1.5*desc {
					t.Errorf("on %s, a key has a contrast of %.2f and what it does %.2f, want the key to stand out", bg, key, desc)
				}
				if sep >= desc {
					t.Errorf("on %s, the separator has a contrast of %.2f and what a key does %.2f, want the separator dimmer", bg, sep, desc)
				}
			}
			if h.FullKey.GetForeground() != h.ShortKey.GetForeground() || h.FullDesc.GetForeground() != h.ShortDesc.GetForeground() {
				t.Error("the list of keys doesn't show keys as the hints do")
			}
		})
	}
}
