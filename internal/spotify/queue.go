package spotify

import (
	"context"
)

type (
	// Queue is what Spotify plays now and next, on any device.
	Queue struct {
		// Playing is the song or episode playing now, or nil when Spotify
		// plays nothing.
		Playing *PlaylistItem
		// Items are the songs and episodes that play next, in order. Each
		// Position is its place in the queue, counting the entries the Web
		// API returns empty, which Items leaves out, so the item at Position
		// plays after Position+1 skips.
		Items []PlaylistItem
	}

	queueResponse struct {
		CurrentlyPlaying *playable   `json:"currently_playing"`
		Queue            []*playable `json:"queue"`
	}
)

// Queue reads the user's queue. The Web API returns only the first part of
// the queue, usually 20 items, and doesn't say which items were added to the
// queue and which come from the context.
func (c Client) Queue(ctx context.Context) (Queue, error) {
	var resp queueResponse
	if err := c.getJSON(ctx, c.baseURL+"me/player/queue", &resp); err != nil {
		return Queue{}, err
	}

	var queue Queue
	if p := resp.CurrentlyPlaying; p != nil && p.URI != "" {
		playing := p.playlistItem(0, false)
		queue.Playing = &playing
	}
	for i, p := range resp.Queue {
		if p != nil && p.URI != "" {
			queue.Items = append(queue.Items, p.playlistItem(i, false))
		}
	}
	return queue, nil
}
