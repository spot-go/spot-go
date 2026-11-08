package spotify

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/zmb3/spotify/v2"
)

type (
	Artist struct {
		ID   spotify.ID
		URI  spotify.URI
		Name string
	}

	followedArtistsResponse struct {
		Artists struct {
			Next    string `json:"next"`
			Cursors struct {
				After string `json:"after"`
			} `json:"cursors"`
			Items []*struct {
				ID   spotify.ID  `json:"id"`
				URI  spotify.URI `json:"uri"`
				Name string      `json:"name"`
			} `json:"items"`
		} `json:"artists"`
	}

	artistAlbumsResponse struct {
		Total int            `json:"total"`
		Items []*albumObject `json:"items"`
	}
)

const (
	artistAlbumsPerPage = 10
	artistAlbumGroups   = "album,single,compilation"
)

func (c Client) AllFollowedArtists(ctx context.Context) ([]Artist, error) {
	artists := make([]Artist, 0)
	seen := make(map[spotify.ID]bool)
	after := ""
	for {
		query := url.Values{
			"type":  {"artist"},
			"limit": {strconv.Itoa(maxItemsPerPage)},
		}
		if after != "" {
			query.Set("after", after)
		}
		var resp followedArtistsResponse
		if err := c.getJSON(ctx, c.baseURL+"me/following?"+query.Encode(), &resp); err != nil {
			return nil, err
		}
		page := resp.Artists
		for _, artist := range page.Items {
			if artist == nil || artist.ID == "" || seen[artist.ID] {
				continue
			}
			seen[artist.ID] = true
			artists = append(artists, Artist{ID: artist.ID, URI: artist.URI, Name: artist.Name})
		}
		next := page.Cursors.After
		if len(page.Items) == 0 || page.Next == "" || next == "" || next == after {
			return artists, nil
		}
		after = next
	}
}

func (c Client) ArtistAlbumsPage(ctx context.Context, artistID spotify.ID, offset int) (AlbumsPage, error) {
	query := url.Values{
		"include_groups": {artistAlbumGroups},
		"limit":          {strconv.Itoa(artistAlbumsPerPage)},
		"offset":         {strconv.Itoa(offset)},
		"market":         {"from_token"},
	}
	endpoint := fmt.Sprintf("%sartists/%s/albums?%s", c.baseURL, url.PathEscape(string(artistID)), query.Encode())

	var resp artistAlbumsResponse
	if err := c.getJSON(ctx, endpoint, &resp); err != nil {
		return AlbumsPage{}, err
	}

	page := AlbumsPage{
		Items: make([]Album, 0, len(resp.Items)),
		Next:  offset + len(resp.Items),
		Total: resp.Total,
	}
	page.Last = len(resp.Items) == 0 || page.Next >= resp.Total
	for _, a := range resp.Items {
		if a != nil {
			page.Items = append(page.Items, a.album())
		}
	}
	return page, nil
}
