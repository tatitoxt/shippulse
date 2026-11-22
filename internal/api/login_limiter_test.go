package api

import (
	"testing"
	"time"
)

func TestLoginLimiterBlocksAfterMaxFailures(t *testing.T) {
	l := &loginLimiter{fails: make(map[string][]time.Time)}
	key := "127.0.0.1:1234|a@b.c"
	for i := 0; i < loginMaxFailures-1; i++ {
		if l.blocked(key) {
			t.Fatalf("blocked early at failure %d", i)
		}
		l.record(key, false)
	}
	if l.blocked(key) {
		t.Fatal("blocked before reaching the cap")
	}
	l.record(key, false)
	if !l.blocked(key) {
		t.Fatal("not blocked at the cap")
	}
	// A different key is unaffected.
	if l.blocked("other|a@b.c") {
		t.Fatal("unrelated key blocked")
	}
}

func TestLoginLimiterResetsOnSuccess(t *testing.T) {
	l := &loginLimiter{fails: make(map[string][]time.Time)}
	key := "k|e"
	for i := 0; i < loginMaxFailures; i++ {
		l.record(key, false)
	}
	if !l.blocked(key) {
		t.Fatal("expected blocked")
	}
	l.record(key, true)
	if l.blocked(key) {
		t.Fatal("success did not clear the counter")
	}
}

func TestLoginLimiterExpiresWindow(t *testing.T) {
	l := &loginLimiter{fails: make(map[string][]time.Time)}
	key := "k|e"
	for i := 0; i < loginMaxFailures; i++ {
		l.fails[key] = append(l.fails[key], time.Now().Add(-loginWindow-time.Second))
	}
	if l.blocked(key) {
		t.Fatal("expired failures still counted")
	}
}
