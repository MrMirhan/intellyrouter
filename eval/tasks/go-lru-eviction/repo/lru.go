// Package lru implements a fixed-capacity least-recently-used cache.
package lru

import "container/list"

type entry[K comparable, V any] struct {
	key   K
	value V
}

// Cache is a fixed-capacity LRU cache. It is not safe for concurrent use.
type Cache[K comparable, V any] struct {
	capacity int
	ll       *list.List
	items    map[K]*list.Element
	onEvict  func(K, V)
}

// New returns a cache that holds at most capacity entries. onEvict may be nil.
func New[K comparable, V any](capacity int, onEvict func(K, V)) *Cache[K, V] {
	if capacity <= 0 {
		panic("lru: capacity must be positive")
	}
	return &Cache[K, V]{
		capacity: capacity,
		ll:       list.New(),
		items:    make(map[K]*list.Element),
		onEvict:  onEvict,
	}
}

// Get returns the value for key and marks the key as recently used.
func (c *Cache[K, V]) Get(key K) (V, bool) {
	if el, ok := c.items[key]; ok {
		return el.Value.(*entry[K, V]).value, true
	}
	var zero V
	return zero, false
}

// Peek returns the value for key without marking it as recently used.
func (c *Cache[K, V]) Peek(key K) (V, bool) {
	if el, ok := c.items[key]; ok {
		return el.Value.(*entry[K, V]).value, true
	}
	var zero V
	return zero, false
}

// Put inserts or updates key and marks it as recently used.
func (c *Cache[K, V]) Put(key K, value V) {
	if el, ok := c.items[key]; ok {
		el.Value.(*entry[K, V]).value = value
		return
	}
	c.items[key] = c.ll.PushFront(&entry[K, V]{key: key, value: value})
	if c.ll.Len() > c.capacity {
		c.removeElement(c.ll.Back())
	}
}

// Remove deletes key. It does not call the eviction callback.
func (c *Cache[K, V]) Remove(key K) bool {
	el, ok := c.items[key]
	if !ok {
		return false
	}
	c.ll.Remove(el)
	delete(c.items, key)
	return true
}

// Len returns the number of entries in the cache.
func (c *Cache[K, V]) Len() int {
	return c.ll.Len()
}

// Keys returns the keys from most to least recently used.
func (c *Cache[K, V]) Keys() []K {
	keys := make([]K, 0, c.ll.Len())
	for el := c.ll.Front(); el != nil; el = el.Next() {
		keys = append(keys, el.Value.(*entry[K, V]).key)
	}
	return keys
}

func (c *Cache[K, V]) removeElement(el *list.Element) {
	c.ll.Remove(el)
	e := el.Value.(*entry[K, V])
	delete(c.items, e.key)
	if c.onEvict != nil {
		c.onEvict(e.key, e.value)
	}
}
