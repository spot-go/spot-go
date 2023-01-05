package spotify

import (
	"context"
	"net/http"

	"github.com/zmb3/spotify/v2"
)

const (
	maxItemsPerPage = 50
)

type Client struct {
	*spotify.Client
}

const (
	cacheKeyAllCurrentUsersPlaylists = "AllCurrentUsersPlaylists"
)

func NewClient(httpClient *http.Client) *Client {
	return &Client{spotify.New(httpClient)}
}

func (c Client) AllCurrentUsersPlaylists(ctx context.Context) ([]spotify.SimplePlaylist, error) {
	return cacheMapGet(cacheKeyAllCurrentUsersPlaylists,
		func() ([]spotify.SimplePlaylist, error) {
			total := maxItemsPerPage
			offset := 0
			res := make([]spotify.SimplePlaylist, 0)
			for total == maxItemsPerPage {
				playlists, err := c.CurrentUsersPlaylists(
					ctx,
					spotify.Limit(maxItemsPerPage),
					spotify.Offset(offset),
				)
				if err != nil {
					return res, err
				}
				res = append(res, playlists.Playlists...)
				total = len(playlists.Playlists)
				offset = offset + total
			}

			return res, nil
		},
	)
}
