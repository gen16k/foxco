package dlp

import "testing"

func TestFingerprintStable(t *testing.T) {
	a := Fingerprint("hello")
	b := Fingerprint("hello")
	if a != b {
		t.Fatalf("fingerprint not stable: %s != %s", a, b)
	}
	if a == Fingerprint("world") {
		t.Fatalf("distinct inputs collided")
	}
}

func TestCacheLRUEviction(t *testing.T) {
	c := NewCache(2)
	c.Put("a", Result{Decision: Allow})
	c.Put("b", Result{Decision: Block})
	if _, ok := c.Get("a"); !ok { // touch a so b is LRU
		t.Fatal("a should be present")
	}
	c.Put("c", Result{Decision: Allow}) // evicts b
	if _, ok := c.Get("b"); ok {
		t.Fatal("b should have been evicted")
	}
	if _, ok := c.Get("a"); !ok {
		t.Fatal("a should still be present")
	}
	if _, ok := c.Get("c"); !ok {
		t.Fatal("c should be present")
	}
}

func TestCacheDisabled(t *testing.T) {
	c := NewCache(0)
	c.Put("a", Result{Decision: Block})
	if _, ok := c.Get("a"); ok {
		t.Fatal("cache with max<=0 should not store")
	}
}
