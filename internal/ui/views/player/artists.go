package player

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spot-go/spot-go/internal/spotify"
	libspotify "github.com/zmb3/spotify/v2"
)

type (
	artistModel struct {
		playerContext
		paneSize
		after func(time.Duration, tea.Msg) tea.Cmd
	}

	artistItem struct {
		title string
		id    libspotify.ID
		uri   libspotify.URI
		index int
	}

	artistsLoaded struct {
		artists []spotify.Artist
		recent  []spotify.PlayedContext
	}

	artistsLoadFailed struct {
		err error
	}

	artistPlaybackStarted struct {
		seq    int
		name   string
		artist libspotify.URI
		uri    libspotify.URI
		item   libspotify.URI
	}

	artistPlaybackFailed struct {
		seq int
		err error
	}
)

func (a artistItem) Title() string       { return a.title }
func (a artistItem) Description() string { return "" }
func (a artistItem) FilterValue() string { return a.title }

func newArtistModel(ctx playerContext, historyErr error) artistModel {
	return artistModel{playerContext: ctx, after: after}
}

func (a artistModel) Init() tea.Cmd {
	return nil
}

func (a artistModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if cd, ok := msg.(contentDimension); ok {
		a.paneSize = paneSize(cd)
	}
	return a, nil
}

func (a artistModel) View() tea.View {
	return tea.NewView("")
}
