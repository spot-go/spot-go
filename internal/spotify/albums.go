package spotify

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/zmb3/spotify/v2"
)

type (
	Album struct {
		ID       spotify.ID
		URI      spotify.URI
		Name     string
		Artists  []string
		Type     string
		Released time.Time
	}

	SavedAlbum struct {
		Album
		AddedAt time.Time
	}

	AlbumsPage struct {
		Items []Album
		Next  int
		Total int
		Last  bool
	}

	albumObject struct {
		ID                   spotify.ID  `json:"id"`
		URI                  spotify.URI `json:"uri"`
		Name                 string      `json:"name"`
		AlbumType            string      `json:"album_type"`
		ReleaseDate          string      `json:"release_date"`
		ReleaseDatePrecision string      `json:"release_date_precision"`
		Artists              []named     `json:"artists"`
	}

	savedAlbumsResponse struct {
		Total int    `json:"total"`
		Next  string `json:"next"`
		Items []struct {
			AddedAt time.Time    `json:"added_at"`
			Album   *albumObject `json:"album"`
		} `json:"items"`
	}

	albumTracksResponse struct {
		Total int         `json:"total"`
		Items []*playable `json:"items"`
	}
)

func (c Client) AllSavedAlbums(ctx context.Context) ([]SavedAlbum, error) {
	albums := make([]SavedAlbum, 0)
	seen := make(map[spotify.ID]bool)
	for offset := 0; ; offset += maxItemsPerPage {
		query := url.Values{
			"limit":  {strconv.Itoa(maxItemsPerPage)},
			"offset": {strconv.Itoa(offset)},
			"market": {"from_token"},
		}
		var resp savedAlbumsResponse
		if err := c.getJSON(ctx, c.baseURL+"me/albums?"+query.Encode(), &resp); err != nil {
			return nil, err
		}
		for _, item := range resp.Items {
			album := item.Album
			if album == nil || album.ID == "" || seen[album.ID] {
				continue
			}
			seen[album.ID] = true
			albums = append(albums, SavedAlbum{Album: album.album(), AddedAt: item.AddedAt})
		}
		if len(resp.Items) == 0 || resp.Next == "" || offset+maxItemsPerPage >= resp.Total {
			return albums, nil
		}
	}
}

func (c Client) AlbumTracksPage(ctx context.Context, albumID spotify.ID, offset int) (PlaylistItemsPage, error) {
	query := url.Values{
		"limit":  {strconv.Itoa(maxItemsPerPage)},
		"offset": {strconv.Itoa(offset)},
		"market": {"from_token"},
	}
	endpoint := fmt.Sprintf("%salbums/%s/tracks?%s", c.baseURL, url.PathEscape(string(albumID)), query.Encode())

	var resp albumTracksResponse
	if err := c.getJSON(ctx, endpoint, &resp); err != nil {
		return PlaylistItemsPage{}, err
	}

	page := PlaylistItemsPage{
		Items: make([]PlaylistItem, 0, len(resp.Items)),
		Next:  offset + len(resp.Items),
		Total: resp.Total,
	}
	page.Last = len(resp.Items) == 0 || page.Next >= resp.Total
	for i, track := range resp.Items {
		if track != nil {
			page.Items = append(page.Items, track.playlistItem(offset+i, false))
		}
	}
	return page, nil
}

func (a albumObject) album() Album {
	released, _ := parseRelease(a.ReleaseDate, a.ReleaseDatePrecision)
	artists := make([]string, 0, len(a.Artists))
	for _, artist := range a.Artists {
		artists = append(artists, artist.Name)
	}
	return Album{
		ID:       a.ID,
		URI:      a.URI,
		Name:     a.Name,
		Artists:  artists,
		Type:     a.AlbumType,
		Released: released,
	}
}
