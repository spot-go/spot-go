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
	SavedShow struct {
		ID      spotify.ID
		URI     spotify.URI
		Name    string
		AddedAt time.Time
	}

	Episode struct {
		ID          spotify.ID
		URI         spotify.URI
		Name        string
		Duration    time.Duration
		Released    time.Time
		Precision   string
		Playable    bool
		FullyPlayed bool
		Resume      time.Duration
	}

	EpisodesPage struct {
		Items []Episode
		Next  int
		Total int
		Last  bool
	}

	savedShowsResponse struct {
		Total int    `json:"total"`
		Next  string `json:"next"`
		Items []struct {
			AddedAt time.Time `json:"added_at"`
			Show    *struct {
				ID   spotify.ID  `json:"id"`
				URI  spotify.URI `json:"uri"`
				Name string      `json:"name"`
			} `json:"show"`
		} `json:"items"`
	}

	showEpisodesResponse struct {
		Total int              `json:"total"`
		Items []*episodeObject `json:"items"`
	}

	episodeObject struct {
		ID                   spotify.ID  `json:"id"`
		URI                  spotify.URI `json:"uri"`
		Name                 string      `json:"name"`
		DurationMs           int         `json:"duration_ms"`
		ReleaseDate          string      `json:"release_date"`
		ReleaseDatePrecision string      `json:"release_date_precision"`
		IsPlayable           *bool       `json:"is_playable"`
		ResumePoint          *struct {
			FullyPlayed      bool `json:"fully_played"`
			ResumePositionMs int  `json:"resume_position_ms"`
		} `json:"resume_point"`
	}
)

var releaseLayouts = map[string]string{
	"day":   "2006-01-02",
	"month": "2006-01",
	"year":  "2006",
}

func (e Episode) InProgress() bool {
	return !e.FullyPlayed && e.Resume > 0 && e.Resume < e.Duration
}

func (c Client) AllSavedShows(ctx context.Context) ([]SavedShow, error) {
	shows := make([]SavedShow, 0)
	seen := make(map[spotify.ID]bool)
	for offset := 0; ; offset += maxItemsPerPage {
		query := url.Values{
			"limit":  {strconv.Itoa(maxItemsPerPage)},
			"offset": {strconv.Itoa(offset)},
		}
		var resp savedShowsResponse
		if err := c.getJSON(ctx, c.baseURL+"me/shows?"+query.Encode(), &resp); err != nil {
			return nil, err
		}
		for _, item := range resp.Items {
			show := item.Show
			if show == nil || show.ID == "" || seen[show.ID] {
				continue
			}
			seen[show.ID] = true
			shows = append(shows, SavedShow{ID: show.ID, URI: show.URI, Name: show.Name, AddedAt: item.AddedAt})
		}
		if len(resp.Items) == 0 || resp.Next == "" || offset+maxItemsPerPage >= resp.Total {
			return shows, nil
		}
	}
}

func (c Client) ShowEpisodesPage(ctx context.Context, showID spotify.ID, offset int) (EpisodesPage, error) {
	query := url.Values{
		"limit":  {strconv.Itoa(maxItemsPerPage)},
		"offset": {strconv.Itoa(offset)},
		"market": {"from_token"},
	}
	endpoint := fmt.Sprintf("%sshows/%s/episodes?%s", c.baseURL, url.PathEscape(string(showID)), query.Encode())

	var resp showEpisodesResponse
	if err := c.getJSON(ctx, endpoint, &resp); err != nil {
		return EpisodesPage{}, err
	}

	page := EpisodesPage{
		Items: make([]Episode, 0, len(resp.Items)),
		Next:  offset + len(resp.Items),
		Total: resp.Total,
	}
	page.Last = len(resp.Items) == 0 || page.Next >= resp.Total
	for _, e := range resp.Items {
		if e != nil {
			page.Items = append(page.Items, e.episode())
		}
	}
	return page, nil
}

func (c Client) Episode(ctx context.Context, id spotify.ID) (Episode, error) {
	query := url.Values{"market": {"from_token"}}
	endpoint := fmt.Sprintf("%sepisodes/%s?%s", c.baseURL, url.PathEscape(string(id)), query.Encode())

	var resp episodeObject
	if err := c.getJSON(ctx, endpoint, &resp); err != nil {
		return Episode{}, err
	}
	return resp.episode(), nil
}

func (c Client) PlayEpisode(ctx context.Context, deviceID spotify.ID, show, episode spotify.URI, from time.Duration) error {
	opt := &spotify.PlayOptions{
		PlaybackContext: &show,
		PlaybackOffset:  &spotify.PlaybackOffset{URI: episode},
		PositionMs:      spotify.Numeric(max(from.Milliseconds(), 0)),
	}
	if deviceID != "" {
		opt.DeviceID = &deviceID
	}
	return c.PlayOpt(ctx, opt)
}

func (e episodeObject) episode() Episode {
	released, precision := parseRelease(e.ReleaseDate, e.ReleaseDatePrecision)
	ep := Episode{
		ID:        e.ID,
		URI:       e.URI,
		Name:      e.Name,
		Duration:  time.Duration(e.DurationMs) * time.Millisecond,
		Released:  released,
		Precision: precision,
		Playable:  e.IsPlayable == nil || *e.IsPlayable,
	}
	if e.ResumePoint != nil {
		ep.FullyPlayed = e.ResumePoint.FullyPlayed
		ep.Resume = time.Duration(e.ResumePoint.ResumePositionMs) * time.Millisecond
	}
	return ep
}

func parseRelease(date, precision string) (time.Time, string) {
	if precision == "" {
		for p, layout := range releaseLayouts {
			if len(layout) == len(date) {
				precision = p
			}
		}
	}
	layout, ok := releaseLayouts[precision]
	if !ok {
		return time.Time{}, ""
	}
	released, err := time.Parse(layout, date)
	if err != nil {
		return time.Time{}, ""
	}
	return released, precision
}
