package styles

import (
	"charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"
)

// Help is how the hints of keys show, wherever spot-go shows them: each key
// brighter than what it does, and both easy to read on the background of
// the terminal, which the dim grays of bubbles' help aren't on a dark one.
// The separators between hints stay dim, as they only space them out.
func Help() help.Styles {
	lightDark := lipgloss.LightDark(HasDarkBackground)
	key := lipgloss.NewStyle().Foreground(lightDark(lipgloss.Color("#303030"), lipgloss.Color("#D0D0D0")))
	desc := lipgloss.NewStyle().Foreground(lightDark(lipgloss.Color("#6C6C6C"), lipgloss.Color("#9E9E9E")))
	sep := lipgloss.NewStyle().Foreground(lightDark(lipgloss.Color("#B2B2B2"), lipgloss.Color("#626262")))
	return help.Styles{
		ShortKey:       key,
		ShortDesc:      desc,
		ShortSeparator: sep,
		Ellipsis:       sep,
		FullKey:        key,
		FullDesc:       desc,
		FullSeparator:  sep,
	}
}
