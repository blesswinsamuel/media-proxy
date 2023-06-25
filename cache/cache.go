package cache

import (
	"crypto/sha256"
	"fmt"

	"github.com/rs/zerolog/log"
)

type Cache interface {
	Get(key string) ([]byte, error)
	Put(key string, data []byte) error
	Exists(key string) (bool, error)
}

func GetCachedOrFetch(cache Cache, key string, fetch func() ([]byte, error)) ([]byte, error) {
	if cachedImage, err := cache.Get(key); err != nil {
		return nil, fmt.Errorf("failed to fetch from cache: %w", err)
	} else if cachedImage != nil {
		log.Debug().Msgf("Cache hit for %s", key)
		return cachedImage, nil
	}
	log.Debug().Msgf("Cache miss for %s", key)
	img, err := fetch()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from upstream: %w", err)
	}
	if err := cache.Put(key, img); err != nil {
		return nil, fmt.Errorf("failed to put to cache: %w", err)
	}
	return img, nil
}

func Sha256Hash(data string) string {
	h := sha256.New()
	h.Write([]byte(data))
	bs := h.Sum(nil)
	return fmt.Sprintf("%x", bs)
}
