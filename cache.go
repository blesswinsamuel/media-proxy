package main

import (
	"io"
	"os"
	"path"
)

type FsCache struct {
	cachePath string
}

// Get gets the file from local filesystem
func (c *FsCache) Get(key string) ([]byte, error) {
	filePath := path.Join(c.cachePath, key)
	file, err := os.Open(filePath)
	if err != nil {
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
