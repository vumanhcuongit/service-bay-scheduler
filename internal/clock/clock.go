// Package clock provides the model.Clock implementations: the wall clock and a fixed one for tests.
package clock

import (
	"sync"
	"time"
)

// System reads the wall clock. It is the only caller of time.Now outside latency timing (D12).
type System struct{}

func (System) Now() time.Time { return time.Now() }

// Fixed returns a set instant until Set moves it.
type Fixed struct {
	mu sync.Mutex
	t  time.Time
}

func NewFixed(t time.Time) *Fixed { return &Fixed{t: t} }

func (f *Fixed) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *Fixed) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = t
}
