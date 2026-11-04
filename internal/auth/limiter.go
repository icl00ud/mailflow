package auth

import (
	"sync"
	"time"
)

// Limiter is a fixed-window counter of failed attempts keyed by caller
// (usually "ip|email"). In-memory: it resets on restart and is per process,
// which is fine for one panel replica and the 15 minute window.
type Limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, hits: map[string][]time.Time{}}
}

// Allow reports whether key may attempt again at now. It does not count the
// attempt; call Fail for that, so a success can clear the history.
func (l *Limiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.prune(key, now)
	return len(kept) < l.max
}

// Fail records one failed attempt.
func (l *Limiter) Fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hits[key] = append(l.prune(key, now), now)
}

// Reset forgets the history of key (a successful login).
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}

func (l *Limiter) prune(key string, now time.Time) []time.Time {
	old := l.hits[key]
	cut := now.Add(-l.window)
	keep := old[:0]
	for _, t := range old {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	if len(keep) == 0 {
		delete(l.hits, key)
		return nil
	}
	l.hits[key] = keep
	return keep
}
