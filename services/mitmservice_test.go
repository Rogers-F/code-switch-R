package services

import (
	"crypto/tls"
	"testing"
)

func TestMITMCertLRUCacheEviction(t *testing.T) {
	cache := newMITMCertLRUCache(2)

	cache.Put("a.example", &tls.Certificate{})
	cache.Put("b.example", &tls.Certificate{})
	if cache.Len() != 2 {
		t.Fatalf("expected len=2, got %d", cache.Len())
	}

	// Adding a third entry should evict the least-recently-added (a.example).
	cache.Put("c.example", &tls.Certificate{})
	if cache.Len() != 2 {
		t.Fatalf("expected len=2 after eviction, got %d", cache.Len())
	}
	if _, ok := cache.Get("a.example"); ok {
		t.Fatalf("expected a.example to be evicted")
	}
	if _, ok := cache.Get("b.example"); !ok {
		t.Fatalf("expected b.example to remain")
	}
	if _, ok := cache.Get("c.example"); !ok {
		t.Fatalf("expected c.example to remain")
	}

	// Touch b.example, then insert d.example, which should evict c.example.
	if _, ok := cache.Get("b.example"); !ok {
		t.Fatalf("expected b.example to exist")
	}
	cache.Put("d.example", &tls.Certificate{})
	if _, ok := cache.Get("c.example"); ok {
		t.Fatalf("expected c.example to be evicted after touching b.example")
	}
	if _, ok := cache.Get("b.example"); !ok {
		t.Fatalf("expected b.example to remain after touching")
	}
	if _, ok := cache.Get("d.example"); !ok {
		t.Fatalf("expected d.example to remain")
	}
}
