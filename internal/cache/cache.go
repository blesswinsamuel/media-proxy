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
	keyHashed := Sha256Hash(key)
	if cachedImage, err := cache.Get(keyHashed); err != nil {
		return nil, fmt.Errorf("failed to fetch from cache: %w", err)
	} else if cachedImage != nil {
		log.Debug().Str("key", key).Str("keyHashed", keyHashed).Int("size", len(cachedImage)).Msgf("Cache hit")
		return cachedImage, nil
	}
	log.Debug().Str("key", key).Str("keyHashed", keyHashed).Msgf("Cache miss")
	img, err := fetch()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from upstream: %w", err)
	}
	if err := cache.Put(keyHashed, img); err != nil {
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
