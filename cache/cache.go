package cache

import (
	"fmt"
	"io"
	"os"
	"path"

	"github.com/rs/zerolog/log"
)

type Cache interface {
	Get(key string) ([]byte, error)
	Put(key string, data []byte) error
	Exists(key string) (bool, error)
}

type FsCache struct {
	cachePath string
}

func NewFsCache(cachePath string) Cache {
	return &FsCache{cachePath: cachePath}
}

// Get gets the file from local filesystem
func (c *FsCache) Get(key string) ([]byte, error) {
	filePath := path.Join(c.cachePath, key)
	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

// Put puts a file into the filesystem cache
func (c *FsCache) Put(key string, data []byte) error {
	if err := os.MkdirAll(c.cachePath, 0755); err != nil {
		return err
	}
	filePath := path.Join(c.cachePath, key)
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	file.Write(data)
	return nil
}

// Exists checks if a file exists in the filesystem cache
func (c *FsCache) Exists(key string) (bool, error) {
	filePath := path.Join(c.cachePath, key)
	if _, err := os.Stat(filePath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
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
