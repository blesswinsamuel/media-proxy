package cache

import (
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
)

type FsCache struct {
	cachePath string
}

func NewFsCache(cachePath string) Cache {
	cache := &FsCache{cachePath: cachePath}
	if err := prometheus.Register(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name:        "media_proxy_cache_fs_size_bytes",
		ConstLabels: prometheus.Labels{"cache_path": cachePath},
	}, func() float64 {
		size, _, _ := cache.GetCacheSize()
		return float64(size)
	})); err != nil {
		log.Warn().Err(err).Str("cache_path", cachePath).Msg("failed to register metric media_proxy_cache_fs_size_bytes")
	}
	if err := prometheus.Register(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name:        "media_proxy_cache_fs_files_count",
		ConstLabels: prometheus.Labels{"cache_path": cachePath},
	}, func() float64 {
		_, count, _ := cache.GetCacheSize()
		return float64(count)
	})); err != nil {
		log.Warn().Err(err).Str("cache_path", cachePath).Msg("failed to register metric media_proxy_cache_fs_files_count")
	}
	return cache
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

func (c *FsCache) GetCacheSize() (int64, int64, error) {
	var size int64
	var count int64
	if err := filepath.Walk(c.cachePath, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			size += info.Size()
			count++
		}
		return err
	}); err != nil {
		return 0, 0, err
	}
	return size, count, nil
}
