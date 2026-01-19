package wapp

import (
	"testing"
)

func TestDecompressionCache(t *testing.T) {
	t.Run("NilCache", func(t *testing.T) {
		var c *decompressionCache
		_, ok := c.Get("key")
		if ok {
			t.Error("Get on nil cache should return false")
		}
		c.Add("key", []byte("data")) // should not panic
	})

	t.Run("BasicOperations", func(t *testing.T) {
		c := newDecompressionCache(1024)
		if c == nil {
			t.Fatal("newDecompressionCache returned nil")
		}

		c.Add("key1", []byte("value1"))
		data, ok := c.Get("key1")
		if !ok {
			t.Error("Get should return true for existing key")
		}
		if string(data) != "value1" {
			t.Errorf("Get returned %q, want %q", data, "value1")
		}

		_, ok = c.Get("nonexistent")
		if ok {
			t.Error("Get should return false for non-existent key")
		}
	})

	t.Run("UpdateExisting", func(t *testing.T) {
		c := newDecompressionCache(1024)
		c.Add("key", []byte("value1"))
		c.Add("key", []byte("value2"))

		data, ok := c.Get("key")
		if !ok {
			t.Error("Get should return true")
		}
		if string(data) != "value2" {
			t.Errorf("Get returned %q, want %q", data, "value2")
		}
	})

	t.Run("Eviction", func(t *testing.T) {
		c := newDecompressionCache(20)
		c.Add("key1", []byte("12345678")) // 8 bytes
		c.Add("key2", []byte("12345678")) // 8 bytes
		c.Add("key3", []byte("12345678")) // 8 bytes, should evict key1

		_, ok := c.Get("key1")
		if ok {
			t.Error("key1 should have been evicted")
		}

		_, ok = c.Get("key2")
		if !ok {
			t.Error("key2 should still exist")
		}

		_, ok = c.Get("key3")
		if !ok {
			t.Error("key3 should exist")
		}
	})

	t.Run("LRUOrder", func(t *testing.T) {
		c := newDecompressionCache(15)
		c.Add("key1", []byte("1234")) // 4 bytes, curBytes=4
		c.Add("key2", []byte("1234")) // 4 bytes, curBytes=8
		c.Add("key3", []byte("1234")) // 4 bytes, curBytes=12

		// Access key1 to make it recently used (moves to front)
		c.Get("key1")

		// Add entry to trigger eviction (curBytes would be 20 > 15)
		c.Add("key4", []byte("1234")) // 4 bytes

		// key1 should still exist (recently used)
		_, ok := c.Get("key1")
		if !ok {
			t.Error("key1 should still exist (recently used)")
		}

		// key2 should be evicted (least recently used after key1 access)
		_, ok = c.Get("key2")
		if ok {
			t.Error("key2 should have been evicted")
		}
	})

	t.Run("ZeroMaxBytes", func(t *testing.T) {
		c := newDecompressionCache(0)
		if c != nil {
			t.Error("newDecompressionCache(0) should return nil")
		}
	})

	t.Run("NegativeMaxBytes", func(t *testing.T) {
		c := newDecompressionCache(-1)
		if c != nil {
			t.Error("newDecompressionCache(-1) should return nil")
		}
	})

	t.Run("OversizedEntry", func(t *testing.T) {
		c := newDecompressionCache(10)
		c.Add("big", []byte("this is way too big for the cache"))

		_, ok := c.Get("big")
		if ok {
			t.Error("Oversized entry should not be cached")
		}
	})

	t.Run("EmptyEntry", func(t *testing.T) {
		c := newDecompressionCache(1024)
		c.Add("empty", []byte{})

		_, ok := c.Get("empty")
		if ok {
			t.Error("Empty entry should not be cached")
		}
	})
}
