package cache

type NoopCache struct {
}

func NewNoopCache() Cache {
	return &NoopCache{}
}

// Get gets the file from local filesystem
func (c *NoopCache) Get(key string) ([]byte, error) {
	return nil, nil
}

// Put puts a file into the filesystem cache
func (c *NoopCache) Put(key string, data []byte) error {
	return nil
}

// Exists checks if a file exists in the filesystem cache
func (c *NoopCache) Exists(key string) (bool, error) {
	return false, nil
}
