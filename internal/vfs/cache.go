package vfs

import (
	"container/list"
	"fmt"
)

type cacheKey struct {
	fileID       string
	segmentIndex int
}

type cacheEntry struct {
	key  cacheKey
	data []byte
}

// SegmentCache is an LRU cache for decoded segments.
type SegmentCache struct {
	maxSize  int64
	curSize  int64
	items    map[string]*list.Element
	eviction *list.List
}

// NewSegmentCache creates a segment cache with the given max size in bytes.
func NewSegmentCache(maxSize int64) *SegmentCache {
	return &SegmentCache{
		maxSize:  maxSize,
		items:    make(map[string]*list.Element),
		eviction: list.New(),
	}
}

func keyStr(fileID string, segmentIndex int) string {
	return fmt.Sprintf("%s:%d", fileID, segmentIndex)
}

// Put stores a segment in the cache.
func (c *SegmentCache) Put(fileID string, segmentIndex int, data []byte) {
	k := keyStr(fileID, segmentIndex)

	// If already present, remove first
	if elem, ok := c.items[k]; ok {
		c.removeElement(elem)
	}

	// Evict until there's room
	for c.curSize+int64(len(data)) > c.maxSize && c.eviction.Len() > 0 {
		c.removeElement(c.eviction.Back())
	}

	entry := &cacheEntry{
		key:  cacheKey{fileID: fileID, segmentIndex: segmentIndex},
		data: append([]byte(nil), data...),
	}
	elem := c.eviction.PushFront(entry)
	c.items[k] = elem
	c.curSize += int64(len(data))
}

// Get retrieves a segment from the cache. Returns data and true if found.
func (c *SegmentCache) Get(fileID string, segmentIndex int) ([]byte, bool) {
	k := keyStr(fileID, segmentIndex)
	elem, ok := c.items[k]
	if !ok {
		return nil, false
	}
	c.eviction.MoveToFront(elem)
	return elem.Value.(*cacheEntry).data, true
}

// Size returns the current cache size in bytes.
func (c *SegmentCache) Size() int64 {
	return c.curSize
}

// Clear empties the cache.
func (c *SegmentCache) Clear() {
	c.items = make(map[string]*list.Element)
	c.eviction.Init()
	c.curSize = 0
}

func (c *SegmentCache) removeElement(elem *list.Element) {
	entry := c.eviction.Remove(elem).(*cacheEntry)
	k := keyStr(entry.key.fileID, entry.key.segmentIndex)
	delete(c.items, k)
	c.curSize -= int64(len(entry.data))
}
