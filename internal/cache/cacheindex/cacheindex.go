package cacheindex

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"

	"github.com/puzpuzpuz/xsync/v3"
	"github.com/rs/zerolog/log"
	"github.com/syndtr/goleveldb/leveldb"
)

type CacheIndexItem struct {
	Size         int
	AccessCount  int64
	LatestAccess int64
}

type CacheIndex interface {
	Get(key string) (CacheIndexItem, bool, error)
	Put(key string, data CacheIndexItem) error
	Delete(key string) error
	GetAll() (map[string]CacheIndexItem, error)
}

type CacheIndexMemory struct {
	cacheIndex xsync.MapOf[string, CacheIndexItem]
}

var _ CacheIndex = &CacheIndexMemory{}

func (c *CacheIndexMemory) GetAll() (map[string]CacheIndexItem, error) {
	cacheIndex := make(map[string]CacheIndexItem)
	c.cacheIndex.Range(func(key string, value CacheIndexItem) bool {
		cacheIndex[key] = value
		return true
	})
	return cacheIndex, nil
}

func (c *CacheIndexMemory) Get(key string) (CacheIndexItem, bool, error) {
	if item, ok := c.cacheIndex.Load(key); ok {
		return item, ok, nil
	}
	return CacheIndexItem{}, false, nil
}

func (c *CacheIndexMemory) Put(key string, data CacheIndexItem) error {
	c.cacheIndex.Store(key, data)
	return nil
}

func (c *CacheIndexMemory) Delete(key string) error {
	c.cacheIndex.Delete(key)
	return nil
}

type CacheIndexLevelDB struct {
	db *leveldb.DB
}

func NewCacheIndexLevelDB(cachePath string) *CacheIndexLevelDB {
	db, err := leveldb.OpenFile(cachePath, nil)
	if err != nil {
		panic(err)
	}
	// defer db.Close()
	return &CacheIndexLevelDB{db: db}
}

var _ CacheIndex = &CacheIndexLevelDB{}

func (c *CacheIndexLevelDB) GetAll() (map[string]CacheIndexItem, error) {
	cacheIndex := make(map[string]CacheIndexItem)
	iter := c.db.NewIterator(nil, nil)
	for iter.Next() {
		key := string(iter.Key())
		item := CacheIndexItem{}
		err := json.Unmarshal(iter.Value(), &item)
		if err != nil {
			return nil, err
		}
		cacheIndex[key] = item
	}
	return cacheIndex, nil
}

func (c *CacheIndexLevelDB) Get(key string) (CacheIndexItem, bool, error) {
	item, err := c.db.Get([]byte(key), nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			return CacheIndexItem{}, false, nil
		}
		return CacheIndexItem{}, false, err
	}
	cacheIndexItem := CacheIndexItem{}
	err = json.Unmarshal(item, &cacheIndexItem)
	if err != nil {
		return CacheIndexItem{}, false, err
	}
	return cacheIndexItem, true, nil
}

func (c *CacheIndexLevelDB) Put(key string, data CacheIndexItem) error {
	item, err := json.Marshal(data)
	if err != nil {
		return err
	}
	err = c.db.Put([]byte(key), item, nil)
	if err != nil {
		return err
	}
	return nil
}

func (c *CacheIndexLevelDB) Delete(key string) error {
	c.db.Delete([]byte(key), nil)
	return nil
}

type CacheIndexJsonFile struct {
	indexPath string
}

var _ CacheIndex = &CacheIndexJsonFile{}

func (c *CacheIndexJsonFile) GetAll() (map[string]CacheIndexItem, error) {
	file, err := os.Open(c.indexPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	jsonDecoder := json.NewDecoder(file)
	cacheIndex := make(map[string]CacheIndexItem)
	if err := jsonDecoder.Decode(&cacheIndex); err != nil {
		return nil, err
	}
	return cacheIndex, nil
}

func (c *CacheIndexJsonFile) putAll(index map[string]CacheIndexItem) error {
	file, err := os.Create(c.indexPath)
	if err != nil {
		return err
	}
	defer file.Close()
	jsonEncoder := json.NewEncoder(file)
	if err := jsonEncoder.Encode(index); err != nil {
		return err
	}
	return nil
}

func (c *CacheIndexJsonFile) Get(key string) (CacheIndexItem, bool, error) {
	cacheIndex, err := c.GetAll()
	if err != nil {
		return CacheIndexItem{}, false, err
	}
	cacheIndexItem, ok := cacheIndex[key]
	return cacheIndexItem, ok, nil
}

func (c *CacheIndexJsonFile) Put(key string, data CacheIndexItem) error {
	cacheIndex, err := c.GetAll()
	if err != nil {
		return err
	}
	cacheIndex[key] = data
	if err := c.putAll(cacheIndex); err != nil {
		return err
	}
	return nil
}

func (c *CacheIndexJsonFile) Delete(key string) error {
	cacheIndex, err := c.GetAll()
	if err != nil {
		return err
	}
	delete(cacheIndex, key)
	if err := c.putAll(cacheIndex); err != nil {
		return err
	}
	return nil
}

type CacheIndexFs struct {
	cacheIndexPath string
}

var _ CacheIndex = &CacheIndexFs{}

func (c *CacheIndexFs) GetAll() (map[string]CacheIndexItem, error) {
	cacheIndex := make(map[string]CacheIndexItem)
	fs.WalkDir(os.DirFS(c.cacheIndexPath), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		key := strings.TrimPrefix(path, c.cacheIndexPath)
		value, _, err := c.Get(key)
		if err != nil {
			return err
		}
		cacheIndex[key] = value
		return nil
	})
	return cacheIndex, nil
}

func (c *CacheIndexFs) Get(key string) (CacheIndexItem, bool, error) {
	file, err := os.Open(path.Join(c.cacheIndexPath, key))
	if err != nil {
		if os.IsNotExist(err) {
			return CacheIndexItem{}, false, nil
		}
		return CacheIndexItem{}, false, err
	}
	defer file.Close()
	cacheIndexItem := CacheIndexItem{}
	jsonDecoder := json.NewDecoder(file)
	if err := jsonDecoder.Decode(&cacheIndexItem); err != nil {
		return CacheIndexItem{}, false, err
	}
	return cacheIndexItem, true, nil
}

func (c *CacheIndexFs) Put(key string, data CacheIndexItem) error {
	if err := os.MkdirAll(c.cacheIndexPath, 0755); err != nil {
		return err
	}
	file, err := os.Create(path.Join(c.cacheIndexPath, key))
	if err != nil {
		return err
	}
	defer file.Close()
	jsonEncoder := json.NewEncoder(file)
	if err := jsonEncoder.Encode(data); err != nil {
		return err
	}
	return nil
}

func (c *CacheIndexFs) Delete(key string) error {
	filePath := path.Join(c.cacheIndexPath, key)
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			log.Warn().Err(err).Str("filePath", filePath).Msg("failed to remove missing file")
		} else {
			return err
		}
	}
	return nil
}

func PruneCache(cacheIndex CacheIndex, cachePath string, ttl time.Duration) error {
	toDeleteKeys := make([]string, 0)
	deleteBeforeTime := time.Now().Add(-ttl).UnixNano()
	index, err := cacheIndex.GetAll()
	if err != nil {
		return err
	}
	for key, value := range index {
		if value.LatestAccess < deleteBeforeTime {
			toDeleteKeys = append(toDeleteKeys, key)
		}
	}
	for _, key := range toDeleteKeys {
		filePath := path.Join(cachePath, key)
		if err := os.Remove(filePath); err != nil {
			if os.IsNotExist(err) {
				log.Warn().Err(err).Str("filePath", filePath).Msg("failed to remove missing file")
			} else {
				return err
			}
		}
		cacheIndex.Delete(key)
	}
	// TODO: compare existing files with index, delete missing files from index and delete actual cache file if missing from index
	return nil
}

func UpdateIndex(cacheIndex CacheIndex, key string, data []byte) error {
	cacheIndexItem, ok, err := cacheIndex.Get(key)
	if err != nil {
		return err
	}
	if !ok {
		cacheIndexItem = CacheIndexItem{Size: len(data)}
	}
	cacheIndexItem.AccessCount++
	cacheIndexItem.LatestAccess = time.Now().UnixNano()
	cacheIndex.Put(key, cacheIndexItem)
	return nil
}
