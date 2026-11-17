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

	PlayedContext struct {
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
		IsLocal    bool        `json:"is_local"`
		Artists    []named     `json:"artists"`
		Show       *named      `json:"show"`
	}

	savedTracksResponse struct {
		Total int `json:"total"`
		Items []struct {
			Track *playable `json:"track"`
		} `json:"items"`
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
		Playing        bool
		Context        spotify.URI
		Item           spotify.URI
		Name           string
		Artist         string
		Progress       time.Duration
		Duration       time.Duration
		PausedItem     spotify.URI
		PausedProgress time.Duration
		Device         spotify.ID
		DeviceName     string
		Volume         int
		SupportsVolume bool
		Shuffle        bool
		Repeat         Repeat
	}

	Playback struct {
		Playing        bool
		Device         spotify.ID
		DeviceName     string
		Item           spotify.URI
		Name           string
		Artist         string
		Progress       time.Duration
		Duration       time.Duration
		Volume         int
		SupportsVolume bool
		Shuffle        bool
		Repeat         Repeat
	}

	// Repeat is the repeat mode of the playback: RepeatOff, RepeatContext
	// or RepeatTrack. It is empty when Spotify doesn't say.
	Repeat string

	playerResponse struct {
		IsPlaying    bool             `json:"is_playing"`
		ProgressMs   int              `json:"progress_ms"`
		ShuffleState bool             `json:"shuffle_state"`
		RepeatState  Repeat           `json:"repeat_state"`
		Context      *playbackContext `json:"context"`
		Device       *struct {
			ID             spotify.ID `json:"id"`
			Name           string     `json:"name"`
			VolumePercent  *int       `json:"volume_percent"`
			SupportsVolume *bool      `json:"supports_volume"`
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

const (
	// RepeatOff plays the context once.
	RepeatOff Repeat = "off"
	// RepeatContext plays the context, such as a playlist or an album,
	// again from the start when it ends.
	RepeatContext Repeat = "context"
	// RepeatTrack plays the same song or episode again.
	RepeatTrack Repeat = "track"
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

// LikedSongsPage returns the songs you liked, from offset, newest first, as
// Liked Songs lists them in the Spotify app.
func (c Client) LikedSongsPage(ctx context.Context, offset int) (PlaylistItemsPage, error) {
	query := url.Values{
		"limit":  {strconv.Itoa(maxItemsPerPage)},
		"offset": {strconv.Itoa(offset)},
		"market": {"from_token"},
	}

	var resp savedTracksResponse
	if err := c.getJSON(ctx, c.baseURL+"me/tracks?"+query.Encode(), &resp); err != nil {
		return PlaylistItemsPage{}, err
	}

	page := PlaylistItemsPage{
		Items: make([]PlaylistItem, 0, len(resp.Items)),
		Next:  offset + len(resp.Items),
		Total: resp.Total,
	}
	page.Last = len(resp.Items) == 0 || page.Next >= resp.Total
	for i, entry := range resp.Items {
		if t := entry.Track; t != nil {
			page.Items = append(page.Items, t.playlistItem(offset+i, t.IsLocal))
		}
	}
	return page, nil
}

func (c Client) RecentlyPlayedPlaylists(ctx context.Context) ([]PlayedContext, error) {
	return c.recentlyPlayed(ctx, "playlist")
}

func (c Client) RecentlyPlayedAlbums(ctx context.Context) ([]PlayedContext, error) {
	return c.recentlyPlayed(ctx, "album")
}

func (c Client) RecentlyPlayedArtists(ctx context.Context) ([]PlayedContext, error) {
	return c.recentlyPlayed(ctx, "artist")
}

func (c Client) recentlyPlayed(ctx context.Context, contextType string) ([]PlayedContext, error) {
	query := url.Values{"limit": {strconv.Itoa(maxItemsPerPage)}}
	var resp recentlyPlayedResponse
	if err := c.getJSON(ctx, c.baseURL+"me/player/recently-played?"+query.Encode(), &resp); err != nil {
		return nil, err
	}

	var played []PlayedContext
	for _, item := range resp.Items {
		if uri, ok := item.Context.uriOf(contextType); ok {
			played = append(played, PlayedContext{URI: uri, At: item.PlayedAt})
		}
	}
	return played, nil
}

func (c Client) NowPlaying(ctx context.Context) (NowPlaying, error) {
	resp, err := c.player(ctx)
	if err != nil {
		return NowPlaying{}, err
	}
	device, volume, supportsVolume := resp.deviceVolume()
	state := NowPlaying{
		Device:         device,
		DeviceName:     resp.deviceName(),
		Volume:         volume,
		SupportsVolume: supportsVolume,
		Shuffle:        resp.ShuffleState,
		Repeat:         resp.RepeatState,
	}
	if !resp.IsPlaying {
		paused := state
		if resp.Item != nil {
			paused.PausedItem = resp.Item.URI
			paused.PausedProgress = time.Duration(resp.ProgressMs) * time.Millisecond
		}
		return paused, nil
	}

	playing := state
	playing.Playing = true
	if uri, ok := resp.Context.uriOf("playlist", "show", "album", "artist"); ok {
		playing.Context = uri
	} else if uri, ok := resp.Context.likedSongs(); ok {
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

	playback := Playback{
		Playing:    resp.IsPlaying,
		Progress:   time.Duration(resp.ProgressMs) * time.Millisecond,
		DeviceName: resp.deviceName(),
		Shuffle:    resp.ShuffleState,
		Repeat:     resp.RepeatState,
	}
	playback.Device, playback.Volume, playback.SupportsVolume = resp.deviceVolume()
	if resp.Item != nil {
		playback.Item, playback.Name, playback.Artist = resp.Item.URI, resp.Item.Name, resp.Item.artist()
		playback.Duration = time.Duration(resp.Item.DurationMs) * time.Millisecond
	}
	return playback, nil
}

func (r playerResponse) deviceVolume() (spotify.ID, int, bool) {
	d := r.Device
	if d == nil {
		return "", 0, false
	}
	if d.VolumePercent == nil || d.SupportsVolume != nil && !*d.SupportsVolume {
		return d.ID, 0, false
	}
	return d.ID, *d.VolumePercent, true
}

func (r playerResponse) deviceName() string {
	if r.Device == nil {
		return ""
	}
	return r.Device.Name
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

func (c Client) SetVolume(ctx context.Context, deviceID spotify.ID, percent int) error {
	opt := &spotify.PlayOptions{}
	if deviceID != "" {
		opt.DeviceID = &deviceID
	}
	return c.VolumeOpt(ctx, percent, opt)
}

// SetShuffle turns shuffle on or off on the device Spotify plays on.
func (c Client) SetShuffle(ctx context.Context, shuffle bool) error {
	return c.Shuffle(ctx, shuffle)
}

// SetRepeat sets the repeat mode on the device Spotify plays on.
func (c Client) SetRepeat(ctx context.Context, repeat Repeat) error {
	return c.Repeat(ctx, string(repeat))
}

// SeekTo moves the playback on the device Spotify plays on to position.
func (c Client) SeekTo(ctx context.Context, position time.Duration) error {
	return c.Seek(ctx, int(position.Milliseconds()))
}

func (c Client) player(ctx context.Context) (playerResponse, error) {
	query := url.Values{"additional_types": {"episode"}}
	var resp playerResponse
	err := c.getJSON(ctx, c.baseURL+"me/player?"+query.Encode(), &resp)
	return resp, err
}

func (c Client) PlayContext(ctx context.Context, deviceID spotify.ID, playbackContext spotify.URI, position int) error {
	return c.play(ctx, deviceID, playbackContext, &spotify.PlaybackOffset{Position: &position})
}

// PlayContextFrom plays playbackContext from item, one of its songs, or from
// the start when item is empty. Unlike a position, item keeps pointing at the
// same song when songs are added before it.
func (c Client) PlayContextFrom(ctx context.Context, deviceID spotify.ID, playbackContext, item spotify.URI) error {
	var offset *spotify.PlaybackOffset
	if item != "" {
		offset = &spotify.PlaybackOffset{URI: item}
	}
	return c.play(ctx, deviceID, playbackContext, offset)
}

// PlaySongs plays songs, in order, without a context. It says to start from
// the first one: given only the songs, Spotify may start from another one.
func (c Client) PlaySongs(ctx context.Context, deviceID spotify.ID, songs []spotify.URI) error {
	first := 0
	opt := &spotify.PlayOptions{URIs: songs, PlaybackOffset: &spotify.PlaybackOffset{Position: &first}}
	if deviceID != "" {
		opt.DeviceID = &deviceID
	}
	return c.PlayOpt(ctx, opt)
}

func (c Client) PlayArtist(ctx context.Context, deviceID spotify.ID, artist spotify.URI) error {
	return c.play(ctx, deviceID, artist, nil)
}

func (c Client) play(ctx context.Context, deviceID spotify.ID, playbackContext spotify.URI, offset *spotify.PlaybackOffset) error {
	opt := &spotify.PlayOptions{
		PlaybackContext: &playbackContext,
		PlaybackOffset:  offset,
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

func (c *playbackContext) uriOf(types ...string) (spotify.URI, bool) {
	if c == nil || c.URI == "" || !slices.Contains(types, c.Type) {
		return "", false
	}
	return c.URI, true
}

// likedSongs is the URI of Liked Songs, spotify:user:<id>:collection, when it
// is the context. Your Episodes is a collection too, but its URI goes on,
// with :your-episodes, and its episodes count as played from their show.
func (c *playbackContext) likedSongs() (spotify.URI, bool) {
	if c == nil || c.Type != "collection" || !strings.HasSuffix(string(c.URI), ":collection") {
		return "", false
	}
	return c.URI, true
}

func (i PlaylistItem) Artist() string {
	return strings.Join(i.Artists, ", ")
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
