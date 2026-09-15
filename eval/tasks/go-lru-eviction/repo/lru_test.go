package lru

import (
	"slices"
	"testing"
)

type eviction struct {
	key   string
	value int
}

func newRecorded(capacity int) (*Cache[string, int], *[]eviction) {
	var evicted []eviction
	c := New(capacity, func(k string, v int) {
		evicted = append(evicted, eviction{k, v})
	})
	return c, &evicted
}

func TestPutEvictsOldestWhenFull(t *testing.T) {
	c, evicted := newRecorded(2)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)

	if _, ok := c.Get("a"); ok {
		t.Fatalf("a should have been evicted")
	}
	if want := []eviction{{"a", 1}}; !slices.Equal(*evicted, want) {
		t.Fatalf("evicted = %v, want %v", *evicted, want)
	}
	if c.Len() != 2 {
		t.Fatalf("Len = %d, want 2", c.Len())
	}
}

func TestGetMarksKeyAsRecentlyUsed(t *testing.T) {
	c, evicted := newRecorded(2)
	c.Put("a", 1)
	c.Put("b", 2)
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("Get(a) = %d, %v", v, ok)
	}
	c.Put("c", 3)

	if _, ok := c.Peek("a"); !ok {
		t.Fatalf("a was read before c was added, it must stay in the cache")
	}
	if _, ok := c.Peek("b"); ok {
		t.Fatalf("b is the least recently used key and should have been evicted")
	}
	if want := []eviction{{"b", 2}}; !slices.Equal(*evicted, want) {
		t.Fatalf("evicted = %v, want %v", *evicted, want)
	}
}

func TestPutExistingKeyUpdatesAndMarksRecentlyUsed(t *testing.T) {
	c, evicted := newRecorded(2)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("a", 10)
	c.Put("c", 3)

	if v, ok := c.Peek("a"); !ok || v != 10 {
		t.Fatalf("Peek(a) = %d, %v, want 10, true", v, ok)
	}
	if want := []eviction{{"b", 2}}; !slices.Equal(*evicted, want) {
		t.Fatalf("evicted = %v, want %v", *evicted, want)
	}
	if c.Len() != 2 {
		t.Fatalf("Len = %d, want 2", c.Len())
	}
}

func TestPeekDoesNotChangeOrder(t *testing.T) {
	c, evicted := newRecorded(2)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Peek("a")
	c.Put("c", 3)

	if want := []eviction{{"a", 1}}; !slices.Equal(*evicted, want) {
		t.Fatalf("evicted = %v, want %v", *evicted, want)
	}
}

func TestKeysOrder(t *testing.T) {
	c, _ := newRecorded(3)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)
	c.Get("a")
	c.Put("b", 20)

	if got, want := c.Keys(), []string{"b", "a", "c"}; !slices.Equal(got, want) {
		t.Fatalf("Keys = %v, want %v", got, want)
	}
}

func TestRemove(t *testing.T) {
	c, evicted := newRecorded(2)
	c.Put("a", 1)
	if !c.Remove("a") {
		t.Fatalf("Remove(a) = false")
	}
	if c.Remove("a") {
		t.Fatalf("second Remove(a) = true")
	}
	if c.Len() != 0 || len(*evicted) != 0 {
		t.Fatalf("Len = %d, evicted = %v", c.Len(), *evicted)
	}
}
