package player

import (
	"cmp"
	"slices"
	"time"

	"charm.land/bubbles/v2/list"
	"github.com/spot-go/spot-go/internal/library"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

var (
	playlistOrders = []library.Order{
		library.OrderRecents,
		library.OrderAlphabetical,
		library.OrderCreator,
		library.OrderCustom,
	}
	podcastOrders = []library.Order{
		library.OrderRecents,
		library.OrderRecentlyAdded,
		library.OrderAlphabetical,
	}
	orderLabels = map[library.Order]string{
		library.OrderRecents:       "Recents",
		library.OrderRecentlyAdded: "Recently added",
		library.OrderAlphabetical:  "Alphabetical",
		library.OrderCreator:       "Creator",
		library.OrderCustom:        "Custom order",
	}
)

func validOrder(orders []library.Order, o library.Order) library.Order {
	if slices.Contains(orders, o) {
		return o
	}
	return orders[0]
}

func nextOrder(orders []library.Order, o library.Order) library.Order {
	i := slices.Index(orders, validOrder(orders, o))
	return orders[(i+1)%len(orders)]
}

func sortItems(items []list.Item, order library.Order, history library.History) []list.Item {
	sorted := slices.Clone(items)
	collator := collate.New(language.Und, collate.IgnoreCase)
	byIndex := func(a, b item) int { return cmp.Compare(a.index, b.index) }
	byTitle := func(a, b item) int { return collator.CompareString(a.title, b.title) }

	var compare func(a, b item) int
	switch validOrder(playlistOrders, order) {
	case library.OrderRecents:
		compare = func(a, b item) int {
			return history.Recent(b.uri).Compare(history.Recent(a.uri))
		}
	case library.OrderAlphabetical:
		compare = byTitle
	case library.OrderCreator:
		compare = func(a, b item) int {
			return cmp.Or(collator.CompareString(a.owner, b.owner), byTitle(a, b))
		}
	default:
		compare = byIndex
	}

	slices.SortStableFunc(sorted, func(a, b list.Item) int {
		x, y := a.(item), b.(item)
		return cmp.Or(compare(x, y), byIndex(x, y))
	})
	return sorted
}

func sortShows(items []list.Item, order library.Order, history library.History) []list.Item {
	sorted := slices.Clone(items)
	collator := collate.New(language.Und, collate.IgnoreCase)
	byIndex := func(a, b showItem) int { return cmp.Compare(a.index, b.index) }
	recent := func(s showItem) time.Time {
		if played := history.PodcastLastPlayed(s.uri); played.After(s.addedAt) {
			return played
		}
		return s.addedAt
	}

	var compare func(a, b showItem) int
	switch validOrder(podcastOrders, order) {
	case library.OrderRecents:
		compare = func(a, b showItem) int { return recent(b).Compare(recent(a)) }
	case library.OrderRecentlyAdded:
		compare = func(a, b showItem) int { return b.addedAt.Compare(a.addedAt) }
	default:
		compare = func(a, b showItem) int { return collator.CompareString(a.title, b.title) }
	}

	slices.SortStableFunc(sorted, func(a, b list.Item) int {
		x, y := a.(showItem), b.(showItem)
		return cmp.Or(compare(x, y), byIndex(x, y))
	})
	return sorted
}
