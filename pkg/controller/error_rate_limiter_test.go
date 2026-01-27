package controller

import (
	"errors"
	"testing"
	"time"

	"unifi-port-forward/pkg/routers"
	"unifi-port-forward/testutils"
)

func TestErrorRateLimiter_ShouldLogError(t *testing.T) {
	erl := NewErrorRateLimiter()
	defer erl.Stop()

	serviceKey := "test-namespace/test-service"
	err := errors.New("test error")

	// First error should always be logged
	shouldLog, reason := erl.ShouldLogError(serviceKey, err)
	if !shouldLog {
		t.Errorf("Expected first error to be logged, got suppressed with reason: %s", reason)
	}
	if reason != "" {
		t.Errorf("Expected empty reason for first error, got: %s", reason)
	}

	// Second error immediately after should be suppressed
	shouldLog, reason = erl.ShouldLogError(serviceKey, err)
	if shouldLog {
		t.Errorf("Expected second error to be suppressed, but it was logged")
	}
	if reason == "" {
		t.Errorf("Expected suppression reason, got empty string")
	}

	// Verify error count increased
	if count := erl.GetErrorCount(serviceKey); count != 2 {
		t.Errorf("Expected error count to be 2, got %d", count)
	}
}

func TestErrorRateLimiter_ExponentialBackoff(t *testing.T) {
	startTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	mockClock := testutils.NewMockClock(startTime)
	erl := NewErrorRateLimiterWithTime(mockClock)
	defer erl.Stop()

	serviceKey := "test-namespace/test-service"
	err := errors.New("test error")

	// First error - should log
	shouldLog, _ := erl.ShouldLogError(serviceKey, err)
	if !shouldLog {
		t.Error("Expected first error to be logged")
	}

	// Test backoff progression
	expectedBackoffs := []time.Duration{0, 1 * time.Minute, 5 * time.Minute, 15 * time.Minute, 60 * time.Minute}

	for i := 1; i < len(expectedBackoffs); i++ {
		shouldLog, _ := erl.ShouldLogError(serviceKey, err)
		if shouldLog {
			t.Errorf("Expected error %d to be suppressed", i+1)
		}

		// Fast forward time using mock clock
		mockClock.Advance(10 * time.Millisecond)
	}
}

func TestErrorRateLimiter_DifferentErrorTypes(t *testing.T) {
	erl := NewErrorRateLimiter()
	defer erl.Stop()

	serviceKey := "test-namespace/test-service"
	err1 := errors.New("first error type")
	err2 := errors.New("different error type")

	// First error of type 1
	shouldLog, _ := erl.ShouldLogError(serviceKey, err1)
	if !shouldLog {
		t.Error("Expected first error type to be logged")
	}

	// Second error of same type - should be suppressed
	shouldLog, _ = erl.ShouldLogError(serviceKey, err1)
	if shouldLog {
		t.Error("Expected same error type to be suppressed")
	}

	// Different error type - should be logged immediately
	shouldLog, reason := erl.ShouldLogError(serviceKey, err2)
	if !shouldLog {
		t.Errorf("Expected different error type to be logged, suppressed with reason: %s", reason)
	}
	if reason != "new error type" {
		t.Errorf("Expected reason to be 'new error type', got: %s", reason)
	}

	// Verify error count reset to 1 for new error type
	if count := erl.GetErrorCount(serviceKey); count != 1 {
		t.Errorf("Expected error count to reset to 1, got %d", count)
	}
}

func TestErrorRateLimiter_ResetService(t *testing.T) {
	erl := NewErrorRateLimiter()
	defer erl.Stop()

	serviceKey := "test-namespace/test-service"
	err := errors.New("test error")

	// Add some errors
	erl.ShouldLogError(serviceKey, err)
	erl.ShouldLogError(serviceKey, err)

	// Reset service
	erl.ResetService(serviceKey)

	// First error after reset should be logged
	shouldLog, _ := erl.ShouldLogError(serviceKey, err)
	if !shouldLog {
		t.Error("Expected error after reset to be logged")
	}

	// Verify error count reset
	if count := erl.GetErrorCount(serviceKey); count != 1 {
		t.Errorf("Expected error count to be 1 after reset, got %d", count)
	}
}

func TestErrorRateLimiter_ResetServiceOnErrorChange(t *testing.T) {
	erl := NewErrorRateLimiter()
	defer erl.Stop()

	serviceKey := "test-namespace/test-service"
	err1 := errors.New("first error type")
	err2 := errors.New("second error type")

	// Add some errors of first type
	erl.ShouldLogError(serviceKey, err1)
	erl.ShouldLogError(serviceKey, err1)

	// Reset with different error - should reset backoff
	erl.ResetServiceOnErrorChange(serviceKey, err2)

	// Verify backoff index was reset
	if index := erl.GetBackoffIndex(serviceKey); index != 0 {
		t.Errorf("Expected backoff index to be 0 after reset, got %d", index)
	}
}

func TestErrorRateLimiter_BackoffIndex(t *testing.T) {
	erl := NewErrorRateLimiter()
	defer erl.Stop()

	serviceKey := "test-namespace/test-service"
	err := errors.New("test error")

	// Initial state - no entry
	if index := erl.GetBackoffIndex(serviceKey); index != 0 {
		t.Errorf("Expected initial backoff index to be 0, got %d", index)
	}

	// First error
	erl.ShouldLogError(serviceKey, err)
	if index := erl.GetBackoffIndex(serviceKey); index != 1 {
		t.Errorf("Expected backoff index to be 1 after first error, got %d", index)
	}

	// Second error (should be suppressed due to backoff)
	erl.ShouldLogError(serviceKey, err)
	if index := erl.GetBackoffIndex(serviceKey); index != 1 {
		t.Errorf("Expected backoff index to remain 1 after suppressed error, got %d", index)
	}
}

func TestErrorRateLimiter_ConcurrentAccess(t *testing.T) {
	erl := NewErrorRateLimiter()
	defer erl.Stop()

	serviceKey := "test-namespace/test-service"
	err := errors.New("test error")

	// Test concurrent access
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			erl.ShouldLogError(serviceKey, err)
			erl.GetErrorCount(serviceKey)
			erl.GetBackoffIndex(serviceKey)
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify no race conditions occurred
	// If there's a race condition, the test would panic with "data race"
}

func TestErrorRateLimiter_MultipleServices(t *testing.T) {
	erl := NewErrorRateLimiter()
	defer erl.Stop()

	service1 := "namespace1/service1"
	service2 := "namespace2/service2"
	err := errors.New("test error")

	// Errors for different services should be tracked independently
	shouldLog1, _ := erl.ShouldLogError(service1, err)
	shouldLog2, _ := erl.ShouldLogError(service2, err)

	if !shouldLog1 || !shouldLog2 {
		t.Error("Expected first errors for both services to be logged")
	}

	// Second error for service1 should be suppressed
	shouldLog1, _ = erl.ShouldLogError(service1, err)
	if shouldLog1 {
		t.Error("Expected second error for service1 to be suppressed")
	}

	// Second error for service2 should be suppressed (second call for this service)
	shouldLog2, _ = erl.ShouldLogError(service2, err)
	if shouldLog2 {
		t.Error("Expected second error for service2 to be suppressed")
	}
}

func TestErrorRateLimiter_NilError(t *testing.T) {
	erl := NewErrorRateLimiter()
	defer erl.Stop()

	serviceKey := "test-namespace/test-service"

	// Should handle nil error gracefully
	shouldLog, _ := erl.ShouldLogError(serviceKey, nil)
	if !shouldLog {
		t.Error("Expected nil error to be logged")
	}

	// Hash of nil error should be empty
	if hashError(nil) != "" {
		t.Error("Expected hashError(nil) to return empty string")
	}
}

func TestHashError(t *testing.T) {
	err1 := errors.New("test error")
	err2 := errors.New("test error")
	err3 := errors.New("different error")

	hash1 := hashError(err1)
	hash2 := hashError(err2)
	hash3 := hashError(err3)

	// Same error message should have same hash
	if hash1 != hash2 {
		t.Error("Expected same error messages to have same hash")
	}

	// Different error message should have different hash
	if hash1 == hash3 {
		t.Error("Expected different error messages to have different hashes")
	}

	// Hash should not be empty
	if hash1 == "" {
		t.Error("Expected hash to be non-empty")
	}
}

func TestExtractServiceKeyFromConfig(t *testing.T) {
	// Test valid format
	config1 := routers.PortConfig{
		Name: "test-namespace/test-service:http",
	}
	serviceKey1 := extractServiceKeyFromConfig(config1)
	if serviceKey1 != "test-namespace/test-service" {
		t.Errorf("Expected 'test-namespace/test-service', got '%s'", serviceKey1)
	}

	// Test another valid format with different protocol
	config2 := routers.PortConfig{
		Name: "production/web-service:tcp",
	}
	serviceKey2 := extractServiceKeyFromConfig(config2)
	if serviceKey2 != "production/web-service" {
		t.Errorf("Expected 'production/web-service', got '%s'", serviceKey2)
	}

	// Test invalid format - no colon
	config3 := routers.PortConfig{
		Name: "invalid-format",
	}
	serviceKey3 := extractServiceKeyFromConfig(config3)
	if serviceKey3 != "" {
		t.Errorf("Expected empty string for invalid format, got '%s'", serviceKey3)
	}

	// Test invalid format - no slash
	config4 := routers.PortConfig{
		Name: "just-service:http",
	}
	serviceKey4 := extractServiceKeyFromConfig(config4)
	if serviceKey4 != "" {
		t.Errorf("Expected empty string for format without slash, got '%s'", serviceKey4)
	}

	// Test edge case - multiple colons
	config5 := routers.PortConfig{
		Name: "test-namespace/test-service:tcp:some-extra-info",
	}
	serviceKey5 := extractServiceKeyFromConfig(config5)
	if serviceKey5 != "test-namespace/test-service" {
		t.Errorf("Expected 'test-namespace/test-service' for multiple colons, got '%s'", serviceKey5)
	}

	// Test edge case - empty name
	config6 := routers.PortConfig{
		Name: "",
	}
	serviceKey6 := extractServiceKeyFromConfig(config6)
	if serviceKey6 != "" {
		t.Errorf("Expected empty string for empty name, got '%s'", serviceKey6)
	}
}

func TestErrorRateLimiter_BugReproduction(t *testing.T) {
	// Use mock clock to eliminate sleep delays
	startTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	mockClock := testutils.NewMockClock(startTime)
	erl := NewErrorRateLimiterWithTime(mockClock)
	defer erl.Stop()

	serviceKey := "default/web-service"
	err := errors.New("failed to parse port mapping: invalid port mapping 'http:3001': invalid external port 'http' in mapping 'http:3001' - must be a number between 1-65535. Valid format: 'externalPort:portname' or 'portname'. Example: '8080:http,8443:https'")

	t.Logf("=== Reproducing bug scenario from logs ===")

	// Simulate rapid successive errors like controller retries
	loggedCount := 0
	for i := 0; i < 12; i++ {
		shouldLog, reason := erl.ShouldLogError(serviceKey, err)
		if shouldLog {
			loggedCount++
			t.Logf("Call %d: ✅ LOGGED (shouldLog=true, reason='%s')", i+1, reason)
		} else {
			t.Logf("Call %d: ❌ SUPPRESSED (shouldLog=false, reason='%s')", i+1, reason)
		}

		// Simulate rapid controller retries using mock clock
		if i < 8 {
			mockClock.Advance(50 * time.Millisecond) // Very rapid retries like in logs
		} else {
			mockClock.Advance(2 * time.Second) // No more real sleep!
		}
	}

	// Expected behavior: should only log 1-2 times, not 12 times
	if loggedCount > 2 {
		t.Errorf("❌ BUG CONFIRMED: Logged %d times instead of 1-2. Rate limiting is not working!", loggedCount)
	} else {
		t.Logf("✅ Rate limiting working: Only logged %d times", loggedCount)
	}

	// Test backoff progression by advancing time
	t.Logf("\n=== Testing backoff progression ===")

	// Reset for clean test
	erl.ResetService(serviceKey)

	// Test exact backoff schedule
	testCases := []struct {
		callNum     int
		expectedLog bool
		description string
		timeAdvance time.Duration
	}{
		{1, true, "First error - immediate", 0},
		{2, false, "Second error - should be suppressed", 0},
		{3, false, "Third error - still suppressed", 30 * time.Second},
		{4, true, "After 1 minute - should log again", 30 * time.Second}, // Total 1 minute since last log
		{5, false, "After log - should be suppressed", 0},
		{6, false, "After 1 minute - still suppressed", 3 * time.Minute}, // Total 4 minutes since last log
		{7, true, "After 5 minutes - should log again", 2 * time.Minute}, // 2 more minutes = 5 minutes total since last log (at index 2)
	}

	for _, tc := range testCases {
		// Advance time using mock clock instead of real sleep
		mockClock.Advance(tc.timeAdvance)

		shouldLog, reason := erl.ShouldLogError(serviceKey, err)
		if shouldLog != tc.expectedLog {
			t.Errorf("Call %d (%s): expected shouldLog=%v, got shouldLog=%v, reason='%s'",
				tc.callNum, tc.description, tc.expectedLog, shouldLog, reason)
		} else {
			t.Logf("Call %d (%s): ✅ correct (shouldLog=%v)", tc.callNum, tc.description, shouldLog)
		}
	}

	// Remove the duplicate test loop that was causing confusion
}

func TestErrorRateLimiter_FilterErrorForReconcile(t *testing.T) {
	erl := NewErrorRateLimiter()
	defer erl.Stop()

	serviceKey := "test-namespace/test-service"
	err := errors.New("test error")

	// Test first error - should be returned to controller-runtime
	shouldReturnError, result, reason := erl.FilterErrorForReconcile(serviceKey, err)
	if !shouldReturnError {
		t.Errorf("Expected first error to be returned to controller-runtime, got filtered with reason: %s", reason)
	}
	if reason != "" {
		t.Errorf("Expected empty reason for first error, got: %s", reason)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("Expected no requeue for first error, got: %v", result.RequeueAfter)
	}

	// Test second error immediately after - should be filtered
	shouldReturnError, result, reason = erl.FilterErrorForReconcile(serviceKey, err)
	if shouldReturnError {
		t.Errorf("Expected second error to be filtered, but it was returned to controller-runtime")
	}
	if reason == "" {
		t.Errorf("Expected filter reason, got empty string")
	}
	if result.RequeueAfter == 0 {
		t.Errorf("Expected requeue delay for filtered error, got: %v", result.RequeueAfter)
	}

	// Verify error count increased
	if count := erl.GetErrorCount(serviceKey); count != 2 {
		t.Errorf("Expected error count to be 2, got %d", count)
	}
}

func TestErrorRateLimiter_FilterErrorForReconcile_Backoff(t *testing.T) {
	erl := NewErrorRateLimiter()
	defer erl.Stop()

	serviceKey := "test-namespace/test-service"
	err := errors.New("test error")

	// First error - should log
	shouldReturnError, _, _ := erl.FilterErrorForReconcile(serviceKey, err)
	if !shouldReturnError {
		t.Error("First error should be returned to controller-runtime")
	}

	// Test backoff progression
	// After first error (logged): currentBackoffIndex = 1, nextBackoffIndex = 2
	// For FilterErrorForReconcile, each suppressed error advances backoff level to avoid spam
	expectedBackoffs := []time.Duration{
		5 * time.Minute,  // 2nd error: nextBackoffIndex was 2, then advances to 3
		15 * time.Minute, // 3rd error: nextBackoffIndex was 3, then advances to 4
		60 * time.Minute, // 4th error: nextBackoffIndex was 4 (max), stays at 4
		60 * time.Minute, // 5th error: nextBackoffIndex stays at 4 (max)
	}

	for i, expectedBackoff := range expectedBackoffs {
		// Call FilterErrorForReconcile - this will be filtered since we're on the same error
		shouldReturnError, result, _ := erl.FilterErrorForReconcile(serviceKey, err)

		// Debug output
		t.Logf("Call %d: shouldReturnError=%v, result.RequeueAfter=%v, expected=%v",
			i+2, shouldReturnError, result.RequeueAfter, expectedBackoff)
		t.Logf("  Error count: %d, current backoff: %d, next backoff: %d",
			erl.GetErrorCount(serviceKey), erl.GetBackoffIndex(serviceKey), erl.GetNextBackoffIndex(serviceKey))

		// All these calls should be filtered (shouldReturnError = false)
		if shouldReturnError {
			t.Errorf("Error %d should be filtered, got returned", i+2)
		}

		// Check backoff duration for filtered errors
		if result.RequeueAfter != expectedBackoff {
			t.Errorf("Expected backoff %v for error %d, got %v", expectedBackoff, i+2, result.RequeueAfter)
		}
	}
}

func TestErrorRateLimiter_FilterErrorForReconcile_NewError(t *testing.T) {
	erl := NewErrorRateLimiter()
	defer erl.Stop()

	serviceKey := "test-namespace/test-service"
	err1 := errors.New("test error 1")
	err2 := errors.New("test error 2")

	// First error - should log
	shouldReturnError, _, _ := erl.FilterErrorForReconcile(serviceKey, err1)
	if !shouldReturnError {
		t.Error("First error should be returned to controller-runtime")
	}

	// Second error immediately after - should be filtered
	shouldReturnError, _, _ = erl.FilterErrorForReconcile(serviceKey, err1)
	if shouldReturnError {
		t.Error("Second same error should be filtered")
	}

	// Different error type - should log immediately
	shouldReturnError, result, reason := erl.FilterErrorForReconcile(serviceKey, err2)
	if !shouldReturnError {
		t.Errorf("Different error type should be returned to controller-runtime, got filtered with reason: %s", reason)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("Expected no requeue for different error type, got: %v", result.RequeueAfter)
	}
}

func TestErrorRateLimiter_FilterErrorForReconcile_DefaultBackoff(t *testing.T) {
	erl := NewErrorRateLimiter()
	defer erl.Stop()

	serviceKey := "test-namespace/test-service"
	err := errors.New("test error")

	// Test with service key that has no entry
	shouldReturnError, _, reason := erl.FilterErrorForReconcile(serviceKey, err)
	if !shouldReturnError {
		t.Errorf("Expected first error to be returned to controller-runtime, got filtered with reason: %s", reason)
	}

	// Manually remove the entry to test default behavior
	erl.ResetService(serviceKey)

	// Now test with no entry - should return default backoff
	shouldReturnError, _, reason = erl.FilterErrorForReconcile(serviceKey, err)
	if !shouldReturnError {
		t.Errorf("Expected first error to be returned, got filtered with reason: %s", reason)
	}

	// Reset again and test default behavior in getNextBackoffDuration
	erl.ResetService(serviceKey)
	defaultBackoff := erl.getNextBackoffDuration(serviceKey)
	expectedDefault := 1 * time.Minute
	if defaultBackoff != expectedDefault {
		t.Errorf("Expected default backoff %v, got %v", expectedDefault, defaultBackoff)
	}
}
