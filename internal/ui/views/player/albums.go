package player

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spot-go/spot-go/internal/spotify"
	libspotify "github.com/zmb3/spotify/v2"
)

type (
	albumModel struct {
		playerContext
		paneSize
		after func(time.Duration, tea.Msg) tea.Cmd
	}

	albumItem struct {
		title   string
		artist  string
		id      libspotify.ID
		uri     libspotify.URI
		addedAt time.Time
		index   int
	}

	albumsLoaded struct {
		albums []spotify.SavedAlbum
		recent []spotify.PlayedContext
	}

	albumsLoadFailed struct {
		err error
	}

	albumPlaybackStarted struct {
		seq  int
		name string
		uri  libspotify.URI
		item libspotify.URI
	}

	albumPlaybackFailed struct {
		seq int
		err error
	}
)

func (a albumItem) Title() string       { return a.title }
func (a albumItem) Description() string { return "" }
func (a albumItem) FilterValue() string { return a.title }

func newAlbumModel(ctx playerContext, historyErr error) albumModel {
	return albumModel{playerContext: ctx, after: after}
}

func (a albumModel) Init() tea.Cmd {
	return nil
}

func (a albumModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if cd, ok := msg.(contentDimension); ok {
		a.paneSize = paneSize(cd)
	}
	return a, nil
}

func (a albumModel) View() tea.View {
	return tea.NewView("")
}
