package spotify

import (
	"context"
	"errors"
	"time"

	"github.com/allegro/bigcache/v3"
	"github.com/spot-go/spot-go/internal/encoding"
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

func cacheMapGet[E any](key string, fallbackFn func() ([]E, error)) ([]E, error) {
	cachedValue, err := cache.Get(key)
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
	err = cache.Set(key, b)
	if err != nil {
		return nil, err
	}

	return val, nil
}
