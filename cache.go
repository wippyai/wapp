package wapp

import (
	"container/list"
	"sync"
)

type decompressionCache struct {
	maxBytes int64
	curBytes int64
	entries  map[string]*list.Element
	lru      *list.List
	mu       sync.Mutex
}

type cacheEntry struct {
	path string
	data []byte
	size int64
}

func newDecompressionCache(maxBytes int64) *decompressionCache {
	if maxBytes <= 0 {
		return nil
	}
	return &decompressionCache{
		maxBytes: maxBytes,
		entries:  make(map[string]*list.Element),
		lru:      list.New(),
	}
}

func (c *decompressionCache) Get(path string) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.entries[path]
	if !ok {
		return nil, false
	}

	c.lru.MoveToFront(elem)
	entry := elem.Value.(*cacheEntry)
	return entry.data, true
}

func (c *decompressionCache) Add(path string, data []byte) {
	if c == nil {
		return
	}
	size := int64(len(data))
	if size <= 0 || size > c.maxBytes {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.entries[path]; ok {
		entry := elem.Value.(*cacheEntry)
		c.curBytes -= entry.size
		entry.data = data
		entry.size = size
		c.curBytes += entry.size
		c.lru.MoveToFront(elem)
	} else {
		elem := c.lru.PushFront(&cacheEntry{
			path: path,
			data: data,
			size: size,
		})
		c.entries[path] = elem
		c.curBytes += size
	}

	for c.curBytes > c.maxBytes {
		back := c.lru.Back()
		if back == nil {
			break
		}
		entry := back.Value.(*cacheEntry)
		delete(c.entries, entry.path)
		c.curBytes -= entry.size
		c.lru.Remove(back)
	}
}
