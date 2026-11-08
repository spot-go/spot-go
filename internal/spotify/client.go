package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zmb3/spotify/v2"
)

const (
	maxItemsPerPage = 50
	apiBaseURL      = "https://api.spotify.com/v1/"
	maxRetries      = 3
	maxRetryAfter   = 10 * time.Second

	playableFields     = "type,name,uri,duration_ms,is_playable,artists(name),show(name)"
	playlistItemFields = "total,items(is_local,item(" + playableFields + "),track(" + playableFields + "))"
)

type (
	Client struct {
		*spotify.Client
		httpClient *http.Client
		baseURL    string
	}

	PlaylistItem struct {
		Position int
		Name     string
		Artists  []string
		Duration time.Duration
		URI      spotify.URI
		IsLocal  bool
		Playable bool
	}

	PlaylistItemsPage struct {
		Items []PlaylistItem
		Next  int
		Total int
		Last  bool
	}

	PlayedPlaylist struct {
		URI spotify.URI
		At  time.Time
	}

	playlistItemsResponse struct {
		Total int `json:"total"`
		Items []struct {
			IsLocal bool      `json:"is_local"`
			Item    *playable `json:"item"`
			Track   *playable `json:"track"`
		} `json:"items"`
	}

	playable struct {
		Type       string      `json:"type"`
		Name       string      `json:"name"`
		URI        spotify.URI `json:"uri"`
		DurationMs int         `json:"duration_ms"`
		IsPlayable *bool       `json:"is_playable"`
		Artists    []named     `json:"artists"`
		Show       *named      `json:"show"`
	}

	named struct {
		Name string `json:"name"`
	}

	recentlyPlayedResponse struct {
		Items []struct {
			PlayedAt time.Time        `json:"played_at"`
			Context  *playbackContext `json:"context"`
		} `json:"items"`
	}

	NowPlaying struct {
		Context        spotify.URI
		Item           spotify.URI
		Name           string
		Artist         string
		Progress       time.Duration
		Duration       time.Duration
		PausedItem     spotify.URI
		PausedProgress time.Duration
	}

	Playback struct {
		Playing  bool
		Device   spotify.ID
		Item     spotify.URI
		Name     string
		Artist   string
		Progress time.Duration
		Duration time.Duration
	}

	playerResponse struct {
		IsPlaying  bool             `json:"is_playing"`
		ProgressMs int              `json:"progress_ms"`
		Context    *playbackContext `json:"context"`
		Device     *struct {
			ID spotify.ID `json:"id"`
		} `json:"device"`
		Item *playerItem `json:"item"`
	}

	playerItem struct {
		URI        spotify.URI `json:"uri"`
		Name       string      `json:"name"`
		DurationMs int         `json:"duration_ms"`
		Artists    []named     `json:"artists"`
		Show       *struct {
			URI  spotify.URI `json:"uri"`
			Name string      `json:"name"`
		} `json:"show"`
	}

	playbackContext struct {
		Type string      `json:"type"`
		URI  spotify.URI `json:"uri"`
	}
)

const (
	cacheKeyAllCurrentUsersPlaylists = "AllCurrentUsersPlaylists"
)

func NewClient(httpClient *http.Client) *Client {
	return &Client{
		Client:     spotify.New(httpClient),
		httpClient: httpClient,
		baseURL:    apiBaseURL,
	}
}

func (c Client) AllCurrentUsersPlaylists(ctx context.Context) ([]spotify.SimplePlaylist, error) {
	return cacheMapGet(cacheKeyAllCurrentUsersPlaylists,
		func() ([]spotify.SimplePlaylist, error) {
			res := make([]spotify.SimplePlaylist, 0)
			seen := make(map[spotify.ID]bool)
			for offset := 0; ; offset += maxItemsPerPage {
				page, err := c.CurrentUsersPlaylists(
					ctx,
					spotify.Limit(maxItemsPerPage),
					spotify.Offset(offset),
				)
				if err != nil {
					return res, err
				}
				for _, pl := range page.Playlists {
					if !seen[pl.ID] {
						seen[pl.ID] = true
						res = append(res, pl)
					}
				}
				if page.Next == "" || offset+maxItemsPerPage >= int(page.Total) {
					return res, nil
				}
			}
		},
	)
}

func (c Client) PlaylistItemsPage(ctx context.Context, playlistID spotify.ID, offset int) (PlaylistItemsPage, error) {
	query := url.Values{
		"limit":            {strconv.Itoa(maxItemsPerPage)},
		"offset":           {strconv.Itoa(offset)},
		"market":           {"from_token"},
		"additional_types": {"track,episode"},
		"fields":           {playlistItemFields},
	}
	endpoint := fmt.Sprintf("%splaylists/%s/items?%s", c.baseURL, url.PathEscape(string(playlistID)), query.Encode())

	var resp playlistItemsResponse
	if err := c.getJSON(ctx, endpoint, &resp); err != nil {
		return PlaylistItemsPage{}, err
	}

	page := PlaylistItemsPage{
		Items: make([]PlaylistItem, 0, len(resp.Items)),
		Next:  offset + len(resp.Items),
		Total: resp.Total,
	}
	page.Last = len(resp.Items) == 0 || page.Next >= resp.Total
	for i, entry := range resp.Items {
		p := entry.Item
		if p == nil {
			p = entry.Track
		}
		if p != nil {
			page.Items = append(page.Items, p.playlistItem(offset+i, entry.IsLocal))
		}
	}
	return page, nil
}

func (c Client) RecentlyPlayedPlaylists(ctx context.Context) ([]PlayedPlaylist, error) {
	query := url.Values{"limit": {strconv.Itoa(maxItemsPerPage)}}
	var resp recentlyPlayedResponse
	if err := c.getJSON(ctx, c.baseURL+"me/player/recently-played?"+query.Encode(), &resp); err != nil {
		return nil, err
	}

	var played []PlayedPlaylist
	for _, item := range resp.Items {
		if uri, ok := item.Context.playlist(); ok {
			played = append(played, PlayedPlaylist{URI: uri, At: item.PlayedAt})
		}
	}
	return played, nil
}

func (c Client) NowPlaying(ctx context.Context) (NowPlaying, error) {
	resp, err := c.player(ctx)
	if err != nil {
		return NowPlaying{}, err
	}
	if !resp.IsPlaying {
		var paused NowPlaying
		if resp.Item != nil {
			paused.PausedItem = resp.Item.URI
			paused.PausedProgress = time.Duration(resp.ProgressMs) * time.Millisecond
		}
		return paused, nil
	}

	var playing NowPlaying
	if uri, ok := resp.Context.uriOf("playlist", "show"); ok {
		playing.Context = uri
	}
	if item := resp.Item; item != nil {
		playing.Item, playing.Name, playing.Artist = item.URI, item.Name, item.artist()
		playing.Progress = time.Duration(resp.ProgressMs) * time.Millisecond
		playing.Duration = time.Duration(item.DurationMs) * time.Millisecond
		if playing.Context == "" && item.Show != nil {
			playing.Context = item.Show.URI
		}
	}
	return playing, nil
}

func (c Client) Playback(ctx context.Context) (Playback, error) {
	resp, err := c.player(ctx)
	if err != nil {
		return Playback{}, err
	}

	playback := Playback{Playing: resp.IsPlaying, Progress: time.Duration(resp.ProgressMs) * time.Millisecond}
	if resp.Device != nil {
		playback.Device = resp.Device.ID
	}
	if resp.Item != nil {
		playback.Item, playback.Name, playback.Artist = resp.Item.URI, resp.Item.Name, resp.Item.artist()
		playback.Duration = time.Duration(resp.Item.DurationMs) * time.Millisecond
	}
	return playback, nil
}

func (i playerItem) artist() string {
	names := make([]string, 0, len(i.Artists))
	for _, a := range i.Artists {
		names = append(names, a.Name)
	}
	if len(names) == 0 && i.Show != nil {
		return i.Show.Name
	}
	return strings.Join(names, ", ")
}

func (c Client) ResumePlayback(ctx context.Context, deviceID spotify.ID) error {
	opt := &spotify.PlayOptions{}
	if deviceID != "" {
		opt.DeviceID = &deviceID
	}
	return c.PlayOpt(ctx, opt)
}

func (c Client) player(ctx context.Context) (playerResponse, error) {
	query := url.Values{"additional_types": {"episode"}}
	var resp playerResponse
	err := c.getJSON(ctx, c.baseURL+"me/player?"+query.Encode(), &resp)
	return resp, err
}

func (c Client) PlayPlaylist(ctx context.Context, deviceID spotify.ID, playlist spotify.URI, position int) error {
	opt := &spotify.PlayOptions{
		PlaybackContext: &playlist,
		PlaybackOffset:  &spotify.PlaybackOffset{Position: &position},
	}
	if deviceID != "" {
		opt.DeviceID = &deviceID
	}
	return c.PlayOpt(ctx, opt)
}

func (c Client) getJSON(ctx context.Context, endpoint string, result any) error {
	for attempt := 0; ; attempt++ {
		resp, err := c.get(ctx, endpoint)
		if err != nil {
			return err
		}

		wait, retry := retryAfter(resp)
		if retry && attempt < maxRetries {
			resp.Body.Close()
			if err := sleep(ctx, wait); err != nil {
				return err
			}
			continue
		}

		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNoContent {
			return nil
		}
		if resp.StatusCode != http.StatusOK {
			return decodeError(resp)
		}
		return json.NewDecoder(resp.Body).Decode(result)
	}
}

func (c Client) get(ctx context.Context, endpoint string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	return c.httpClient.Do(req)
}

func retryAfter(resp *http.Response) (time.Duration, bool) {
	switch resp.StatusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
	default:
		return 0, false
	}
	seconds, err := strconv.Atoi(resp.Header.Get("Retry-After"))
	if err != nil || seconds < 0 {
		return time.Second, true
	}
	wait := time.Duration(seconds) * time.Second
	return wait, wait <= maxRetryAfter
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func decodeError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var e struct {
		E spotify.Error `json:"error"`
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&e); err != nil || e.E.Message == "" {
		e.E.Message = fmt.Sprintf("spotify: unexpected HTTP %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	e.E.Status = resp.StatusCode
	return e.E
}

func (c *playbackContext) playlist() (spotify.URI, bool) {
	return c.uriOf("playlist")
}

func (c *playbackContext) uriOf(types ...string) (spotify.URI, bool) {
	if c == nil || c.URI == "" || !slices.Contains(types, c.Type) {
		return "", false
	}
	return c.URI, true
}

func (p playable) playlistItem(position int, isLocal bool) PlaylistItem {
	artists := make([]string, 0, len(p.Artists))
	for _, a := range p.Artists {
		artists = append(artists, a.Name)
	}
	if len(artists) == 0 && p.Show != nil {
		artists = append(artists, p.Show.Name)
	}

	return PlaylistItem{
		Position: position,
		Name:     p.Name,
		Artists:  artists,
		Duration: time.Duration(p.DurationMs) * time.Millisecond,
		URI:      p.URI,
		IsLocal:  isLocal,
		Playable: !isLocal && (p.IsPlayable == nil || *p.IsPlayable),
	}
}
