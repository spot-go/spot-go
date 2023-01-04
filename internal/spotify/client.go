package spotify

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/spot-go/spot-go/internal/encoding"

	"github.com/allegro/bigcache/v3"
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

var (
	cache *bigcache.BigCache
)

func init() {
	var err error
	cache, err = bigcache.New(context.Background(), bigcache.DefaultConfig(10*time.Minute))
	if err != nil {
		panic(err)
	}
}

func NewClient(httpClient *http.Client) *Client {
	return &Client{spotify.New(httpClient)}
}

func (c Client) AllCurrentUsersPlaylists(ctx context.Context) ([]spotify.SimplePlaylist, error) {
	cachedValue, err := cache.Get(cacheKeyAllCurrentUsersPlaylists)
	if err != nil && !errors.Is(err, bigcache.ErrEntryNotFound) {
		return nil, err
	}
	if cachedValue != nil {
		return encoding.BytesToMap[spotify.SimplePlaylist](cachedValue)
	}

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

	b, err := encoding.MapToBytes(res)
	if err != nil {
		return nil, err
	}
	err = cache.Set(cacheKeyAllCurrentUsersPlaylists, b)
	if err != nil {
		return nil, err
	}

	return res, nil
}
