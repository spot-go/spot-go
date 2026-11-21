package spotify

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/allegro/bigcache/v3"
	"github.com/spot-go/spot-go/internal/encoding"
)

var (
	// cache is made on first use, so spot-go in the background, which never
	// uses it, doesn't hold its memory or clean it every second.
	cache = sync.OnceValue(func() *bigcache.BigCache {
		c, err := bigcache.New(context.Background(), bigcache.DefaultConfig(10*time.Minute))
		if err != nil {
			panic(err)
		}
		return c
	})
)

func cacheMapGet[E any](key string, fallbackFn func() ([]E, error)) ([]E, error) {
	cachedValue, err := cache().Get(key)
	if err != nil && !errors.Is(err, bigcache.ErrEntryNotFound) {
		return nil, err
	}
	if cachedValue != nil {
		return encoding.BytesToMap[E](cachedValue)
	}

	val, err := fallbackFn()
	if err != nil {
		return nil, err
	}

	b, err := encoding.MapToBytes(val)
	if err != nil {
		return nil, err
	}
	err = cache().Set(key, b)
	if err != nil {
		return nil, err
	}

	return val, nil
}
