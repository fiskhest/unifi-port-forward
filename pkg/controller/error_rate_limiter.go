package controller

import (
	"crypto/md5"
	"fmt"
	"strings"
	"sync"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"unifi-port-forward/pkg/routers"
	"unifi-port-forward/testutils"
)

// ErrorRateLimiter provides exponential backoff for error logging to reduce log spam
type ErrorRateLimiter struct {
	mutex           sync.RWMutex
	errorEntries    map[string]*ErrorEntry
	backoffSchedule []time.Duration
	cleanupTicker   *time.Ticker
	timeProvider    testutils.TimeProvider
}

// ErrorEntry tracks error information for a specific service
type ErrorEntry struct {
	lastError           error
	lastErrorTime       time.Time
	lastLogTime         time.Time
	errorCount          int
	currentBackoffIndex int    // Current backoff level (1-4: 1min, 5min, 15min, 60min)
	nextBackoffIndex    int    // Next backoff index to use for rate limiting
	errorHash           string // Hash of error message to detect new error types
}

// NewErrorRateLimiter creates a new error rate limiter with exponential backoff
func NewErrorRateLimiter() *ErrorRateLimiter {
	erl := &ErrorRateLimiter{
		errorEntries: make(map[string]*ErrorEntry),
		backoffSchedule: []time.Duration{
			0,                // Index 0: 1st error - immediate
			1 * time.Minute,  // Index 1: 2nd error - 1 minute
			5 * time.Minute,  // Index 2: 3rd error - 5 minutes
			15 * time.Minute, // Index 3: 4th error - 15 minutes
			60 * time.Minute, // Index 4: 5th+ error - 60 minutes
		},
		timeProvider: &testutils.RealTimeProvider{},
	}

	// Start cleanup goroutine to remove old error entries
	erl.cleanupTicker = time.NewTicker(24 * time.Hour)
	go func() {
		for range erl.cleanupTicker.C {
			erl.cleanup()
		}
	}()

	return erl
}

// Stop stops error rate limiter's cleanup ticker
func (e *ErrorRateLimiter) Stop() {
	if e.cleanupTicker != nil {
		e.cleanupTicker.Stop()
	}
}

// ShouldLogError determines whether an error should be logged based on rate limiting
func (e *ErrorRateLimiter) ShouldLogError(serviceKey string, err error) (bool, string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	now := e.timeProvider.Now()
	errorHash := hashError(err)

	entry, exists := e.errorEntries[serviceKey]
	if !exists {
		// First error for this service - always log
		e.errorEntries[serviceKey] = &ErrorEntry{
			lastError:           err,
			lastErrorTime:       now,
			lastLogTime:         now,
			errorCount:          1,
			currentBackoffIndex: 1, // Set to 1 after first error (next error will wait 1 minute)
			nextBackoffIndex:    2, // Next error will use index 2 (5 minutes)
			errorHash:           errorHash,
		}
		return true, ""
	}

	// Check if error type changed - if so, reset and log immediately
	if entry.errorHash != errorHash {
		// Reset backoff for new error type
		entry.lastError = err
		entry.lastErrorTime = now
		entry.lastLogTime = now
		entry.errorCount = 1 // Reset count for new error type
		entry.currentBackoffIndex = 1
		entry.nextBackoffIndex = 2
		entry.errorHash = errorHash
		return true, "new error type"
	}

	// Always increment error count for each ShouldLogError call (only for same error type)
	entry.errorCount++

	// Calculate time since last log
	timeSinceLastLog := now.Sub(entry.lastLogTime)
	backoffDuration := e.getBackoffDuration(entry.currentBackoffIndex)

	if timeSinceLastLog >= backoffDuration {
		// Enough time passed, log and advance backoff
		entry.lastError = err
		entry.lastErrorTime = now
		entry.lastLogTime = now

		// Advance backoff indices for next time (but don't exceed max)
		if entry.currentBackoffIndex < len(e.backoffSchedule)-1 {
			entry.currentBackoffIndex++
		}
		if entry.nextBackoffIndex < len(e.backoffSchedule)-1 {
			entry.nextBackoffIndex++
		}

		return true, ""
	}

	// Not enough time passed - suppress with remaining time
	reason := fmt.Sprintf("rate limited (next log in %v)", backoffDuration-timeSinceLastLog)
	return false, reason
}

// ResetService clears error tracking for a service (call when service is fixed)
func (e *ErrorRateLimiter) ResetService(serviceKey string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	delete(e.errorEntries, serviceKey)
}

// ResetServiceOnErrorChange clears error tracking for a service if error type changed
func (e *ErrorRateLimiter) ResetServiceOnErrorChange(serviceKey string, newError error) bool {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	entry, exists := e.errorEntries[serviceKey]
	if !exists {
		return false
	}

	newErrorHash := hashError(newError)
	if entry.errorHash != newErrorHash {
		// Update entry with new error type but reset timing to allow immediate logging
		entry.lastError = newError
		entry.lastErrorTime = e.timeProvider.Now()
		entry.lastLogTime = time.Time{} // Reset to zero time to force immediate log
		entry.errorCount = 1            // Reset count for new error type
		entry.currentBackoffIndex = 0   // Reset backoff to start
		entry.nextBackoffIndex = 1      // Next error will use index 1
		entry.errorHash = newErrorHash
		return true
	}

	return false
}

// getBackoffDuration returns backoff duration for given index
func (e *ErrorRateLimiter) getBackoffDuration(index int) time.Duration {
	if index < 0 {
		return 0
	}
	if index >= len(e.backoffSchedule) {
		return e.backoffSchedule[len(e.backoffSchedule)-1]
	}
	return e.backoffSchedule[index]
}

// GetBackoffIndex returns current backoff index for a service (for testing/metrics)
func (e *ErrorRateLimiter) GetBackoffIndex(serviceKey string) int {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	if entry, exists := e.errorEntries[serviceKey]; exists {
		return entry.currentBackoffIndex
	}
	return 0
}

// GetNextBackoffIndex returns next backoff index for a service (for testing/metrics)
func (e *ErrorRateLimiter) GetNextBackoffIndex(serviceKey string) int {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	if entry, exists := e.errorEntries[serviceKey]; exists {
		return entry.nextBackoffIndex
	}
	return 0
}

// GetErrorCount returns error count for a service (for testing)
func (e *ErrorRateLimiter) GetErrorCount(serviceKey string) int {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	if entry, exists := e.errorEntries[serviceKey]; exists {
		return entry.errorCount
	}
	return 0
}

// NewErrorRateLimiterWithTime creates a new error rate limiter with custom time provider (for testing)
func NewErrorRateLimiterWithTime(timeProvider testutils.TimeProvider) *ErrorRateLimiter {
	erl := &ErrorRateLimiter{
		errorEntries: make(map[string]*ErrorEntry),
		backoffSchedule: []time.Duration{
			0,                // Index 0: 1st error - immediate
			1 * time.Minute,  // Index 1: 2nd error - 1 minute
			5 * time.Minute,  // Index 2: 3rd error - 5 minutes
			15 * time.Minute, // Index 3: 4th error - 15 minutes
			60 * time.Minute, // Index 4: 5th+ error - 60 minutes
		},
		timeProvider: timeProvider,
	}

	// Only start cleanup goroutine for real time provider (not for tests)
	if _, isRealTime := timeProvider.(*testutils.RealTimeProvider); isRealTime {
		erl.cleanupTicker = time.NewTicker(24 * time.Hour)
		go func() {
			for range erl.cleanupTicker.C {
				erl.cleanup()
			}
		}()
	}

	return erl
}

// FilterErrorForReconcile determines whether an error should be returned to controller-runtime
// This prevents controller-runtime from logging every reconciliation attempt
// Returns (shouldReturnError, result, suppressReason)
func (e *ErrorRateLimiter) FilterErrorForReconcile(serviceKey string, err error) (bool, ctrl.Result, string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	now := e.timeProvider.Now()
	errorHash := hashError(err)

	entry, exists := e.errorEntries[serviceKey]
	if !exists {
		// First error for this service - return it to controller-runtime and set up backoff
		e.errorEntries[serviceKey] = &ErrorEntry{
			lastError:           err,
			lastErrorTime:       now,
			lastLogTime:         now,
			errorCount:          1,
			currentBackoffIndex: 1, // We're now at backoff level 1 (1 minute wait)
			nextBackoffIndex:    2, // Next error will use index 2 (5 minutes)
			errorHash:           errorHash,
		}
		return true, ctrl.Result{}, ""
	}

	// Check if error type changed - if so, reset and return error immediately
	if entry.errorHash != errorHash {
		// Reset for new error type
		entry.lastError = err
		entry.lastErrorTime = now
		entry.lastLogTime = now
		entry.errorCount = 1 // Reset count for new error type
		entry.currentBackoffIndex = 1
		entry.nextBackoffIndex = 2
		entry.errorHash = errorHash
		return true, ctrl.Result{}, "new error type"
	}

	// Always increment error count for each FilterErrorForReconcile call
	entry.errorCount++

	// Calculate time since last log
	timeSinceLastLog := now.Sub(entry.lastLogTime)
	backoffDuration := e.getBackoffDuration(entry.nextBackoffIndex)

	if timeSinceLastLog >= backoffDuration {
		// Enough time passed, return error to controller-runtime
		entry.lastError = err
		entry.lastErrorTime = now
		entry.lastLogTime = now

		// Advance current backoff index for next time
		if entry.currentBackoffIndex < len(e.backoffSchedule)-1 {
			entry.currentBackoffIndex++
		}
		// nextBackoffIndex stays ahead of current by 1 (unless at max)
		if entry.nextBackoffIndex < len(e.backoffSchedule)-1 {
			entry.nextBackoffIndex++
		}

		return true, ctrl.Result{}, ""
	}

	// Not enough time passed - suppress error and return requeue result
	// For FilterErrorForReconcile, advance backoff for each suppressed error to avoid spam
	if entry.currentBackoffIndex < len(e.backoffSchedule)-1 {
		entry.currentBackoffIndex++
	}
	if entry.nextBackoffIndex < len(e.backoffSchedule)-1 {
		entry.nextBackoffIndex++
	}

	reason := fmt.Sprintf("rate limited (next log in %v)", backoffDuration-timeSinceLastLog)
	result := ctrl.Result{RequeueAfter: backoffDuration}
	return false, result, reason
}

// extractServiceKeyFromConfig extracts the service key from a PortConfig name
// Expected format: namespace/service:port or namespace/service:protocol
func extractServiceKeyFromConfig(config routers.PortConfig) string {
	// Config name format: namespace/service:port or namespace/service:protocol
	parts := strings.SplitN(config.Name, ":", 2)
	if len(parts) == 0 {
		return ""
	}

	// If no colon, this is invalid format for our purposes
	if len(parts) == 1 {
		// Must have namespace/service format even without colon
		serviceParts := strings.SplitN(parts[0], "/", 2)
		if len(serviceParts) != 2 || serviceParts[0] == "" || serviceParts[1] == "" {
			return ""
		}
		return parts[0]
	}

	// For colon case, validate that the first part contains a slash (namespace/service)
	servicePart := parts[0]
	serviceParts := strings.SplitN(servicePart, "/", 2)

	// Must have both namespace and service, and neither should be empty
	if len(serviceParts) != 2 || serviceParts[0] == "" || serviceParts[1] == "" {
		return ""
	}

	return parts[0]
}

// hashError creates a hash of error message for comparison
func hashError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%x", md5.Sum([]byte(err.Error())))
}

// cleanup removes old error entries to prevent memory leaks
func (e *ErrorRateLimiter) cleanup() {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	cutoff := e.timeProvider.Now().Add(-24 * time.Hour)
	for key, entry := range e.errorEntries {
		if entry.lastErrorTime.Before(cutoff) {
			delete(e.errorEntries, key)
		}
	}
}

// getNextBackoffDuration gets the next backoff duration for rate-limited errors
func (e *ErrorRateLimiter) getNextBackoffDuration(serviceKey string) time.Duration {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	if entry, exists := e.errorEntries[serviceKey]; exists {
		return e.getBackoffDuration(entry.nextBackoffIndex)
	}

	// Default to 1 minute if no entry exists
	return 1 * time.Minute
}
