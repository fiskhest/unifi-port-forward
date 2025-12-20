package testutils

import (
	"sync"
	"time"
)

// MockClock provides deterministic time control for testing
type MockClock struct {
	mu     sync.RWMutex
	now    time.Time
	timers []*MockTimer
}

// NewMockClock creates a new mock clock starting at the specified time
func NewMockClock(start time.Time) *MockClock {
	return &MockClock{
		now:    start,
		timers: make([]*MockTimer, 0),
	}
}

// Now returns the current mock time
func (c *MockClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

// Advance advances the mock clock by the specified duration
func (c *MockClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)

	// Trigger any expired timers
	for _, timer := range c.timers {
		if !timer.fired && c.now.After(timer.target) {
			timer.fired = true
			if timer.callback != nil {
				go timer.callback()
			}
		}
	}
}

// SetTime sets the mock clock to the specified time
func (c *MockClock) SetTime(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t

	// Trigger any expired timers
	for _, timer := range c.timers {
		if !timer.fired && c.now.After(timer.target) {
			timer.fired = true
			if timer.callback != nil {
				go timer.callback()
			}
		}
	}
}

// MockTimer represents a mock timer
type MockTimer struct {
	target   time.Time
	callback func()
	fired    bool
}

// NewTimer creates a new mock timer
func (c *MockClock) NewTimer(d time.Duration, callback func()) *MockTimer {
	c.mu.Lock()
	defer c.mu.Unlock()

	timer := &MockTimer{
		target:   c.now.Add(d),
		callback: callback,
		fired:    false,
	}

	c.timers = append(c.timers, timer)
	return timer
}

// Sleep waits for the specified duration
func (c *MockClock) Sleep(d time.Duration) {
	c.Advance(d)
}

// TimeProvider interface for injecting mock time
type TimeProvider interface {
	Now() time.Time
	Sleep(d time.Duration)
}

// RealTimeProvider provides real system time
type RealTimeProvider struct{}

func (r *RealTimeProvider) Now() time.Time {
	return time.Now()
}

func (r *RealTimeProvider) Sleep(d time.Duration) {
	time.Sleep(d)
}
