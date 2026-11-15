package spotify

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zmb3/spotify/v2"
)

type (
	SearchType string

	SearchPlaylist struct {
		ID    spotify.ID
		URI   spotify.URI
		Name  string
		Owner string
	}

	SearchShow struct {
		ID        spotify.ID
		URI       spotify.URI
		Name      string
		Publisher string
	}

	SearchPage struct {
		Next  int
		Total int
		Last  bool
	}

	SearchResults struct {
		Songs     []PlaylistItem
		Artists   []Artist
		Albums    []Album
		Playlists []SearchPlaylist
		Shows     []SearchShow
		Episodes  []Episode
		Pages     map[SearchType]SearchPage
	}

	searchList[T any] struct {
		Total int  `json:"total"`
		Items []*T `json:"items"`
	}

	searchResponse struct {
		Tracks  *searchList[playable] `json:"tracks"`
		Artists *searchList[struct {
			ID   spotify.ID  `json:"id"`
			URI  spotify.URI `json:"uri"`
			Name string      `json:"name"`
		}] `json:"artists"`
		Albums    *searchList[albumObject] `json:"albums"`
		Playlists *searchList[struct {
			ID    spotify.ID  `json:"id"`
			URI   spotify.URI `json:"uri"`
			Name  string      `json:"name"`
			Owner *struct {
				DisplayName string `json:"display_name"`
			} `json:"owner"`
		}] `json:"playlists"`
		Shows *searchList[struct {
			ID        spotify.ID  `json:"id"`
			URI       spotify.URI `json:"uri"`
			Name      string      `json:"name"`
			Publisher string      `json:"publisher"`
		}] `json:"shows"`
		Episodes *searchList[episodeObject] `json:"episodes"`
	}
)

const (
	SearchSongs     SearchType = "track"
	SearchArtists   SearchType = "artist"
	SearchAlbums    SearchType = "album"
	SearchPlaylists SearchType = "playlist"
	SearchShows     SearchType = "show"
	SearchEpisodes  SearchType = "episode"

	// The Web API returns at most 10 results of each type per request, and
	// no results past offset 1000.
	SearchPerPage   = 10
	maxSearchOffset = 1000
)

var SearchTypes = []SearchType{SearchSongs, SearchArtists, SearchAlbums, SearchPlaylists, SearchShows, SearchEpisodes}

// Search searches the Spotify catalog for query, returning a page of
// results of each of the types, starting at offset.
func (c Client) Search(ctx context.Context, query string, offset int, types ...SearchType) (SearchResults, error) {
	if len(types) == 0 {
		types = SearchTypes
	}
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = string(t)
	}
	values := url.Values{
		"q":      {query},
		"type":   {strings.Join(names, ",")},
		"limit":  {strconv.Itoa(SearchPerPage)},
		"offset": {strconv.Itoa(offset)},
		"market": {"from_token"},
	}

	var resp searchResponse
	if err := c.getJSON(ctx, c.baseURL+"search?"+values.Encode(), &resp); err != nil {
		return SearchResults{}, err
	}

	results := SearchResults{Pages: make(map[SearchType]SearchPage, len(types))}
	if l := resp.Tracks; l != nil {
		results.Pages[SearchSongs] = searchPage(offset, l.Total, len(l.Items))
		for i, t := range l.Items {
			if t != nil && t.URI != "" {
				results.Songs = append(results.Songs, t.playlistItem(offset+i, false))
			}
		}
	}
	if l := resp.Artists; l != nil {
		results.Pages[SearchArtists] = searchPage(offset, l.Total, len(l.Items))
		for _, a := range l.Items {
			if a != nil && a.URI != "" {
				results.Artists = append(results.Artists, Artist{ID: a.ID, URI: a.URI, Name: a.Name})
			}
		}
	}
	if l := resp.Albums; l != nil {
		results.Pages[SearchAlbums] = searchPage(offset, l.Total, len(l.Items))
		for _, a := range l.Items {
			if a != nil && a.URI != "" {
				results.Albums = append(results.Albums, a.album())
			}
		}
	}
	if l := resp.Playlists; l != nil {
		results.Pages[SearchPlaylists] = searchPage(offset, l.Total, len(l.Items))
		for _, p := range l.Items {
			if p == nil || p.URI == "" {
				continue
			}
			pl := SearchPlaylist{ID: p.ID, URI: p.URI, Name: p.Name}
			if p.Owner != nil {
				pl.Owner = p.Owner.DisplayName
			}
			results.Playlists = append(results.Playlists, pl)
		}
	}
	if l := resp.Shows; l != nil {
		results.Pages[SearchShows] = searchPage(offset, l.Total, len(l.Items))
		for _, s := range l.Items {
			if s != nil && s.URI != "" {
				results.Shows = append(results.Shows, SearchShow{ID: s.ID, URI: s.URI, Name: s.Name, Publisher: s.Publisher})
			}
		}
	}
	if l := resp.Episodes; l != nil {
		results.Pages[SearchEpisodes] = searchPage(offset, l.Total, len(l.Items))
		for _, e := range l.Items {
			if e != nil && e.URI != "" {
				results.Episodes = append(results.Episodes, e.episode())
			}
		}
	}
	return results, nil
}

func searchPage(offset, total, items int) SearchPage {
	page := SearchPage{Next: offset + items, Total: total}
	page.Last = items == 0 || page.Next >= min(total, maxSearchOffset)
	return page
}

// PlayItem plays a single song or episode, without a context, from the
// given position.
func (c Client) PlayItem(ctx context.Context, deviceID spotify.ID, item spotify.URI, from time.Duration) error {
	opt := &spotify.PlayOptions{URIs: []spotify.URI{item}}
	if from > 0 {
		opt.PositionMs = spotify.Numeric(from.Milliseconds())
	}
	if deviceID != "" {
		opt.DeviceID = &deviceID
	}
	return c.PlayOpt(ctx, opt)
}
