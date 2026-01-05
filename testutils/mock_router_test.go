package testutils

import (
	"context"
	"testing"

	"unifi-port-forwarder/pkg/routers"
)

// TestMockRouter_SimulatedFailure tests basic mock router failure logic
func TestMockRouter_SimulatedFailure(t *testing.T) {
	mockRouter := NewMockRouter()

	// Test 1: Verify initial state
	if mockRouter.ShouldOperationFail("AddPort") {
		t.Error("ShouldOperationFail should initially return false")
	}

	// Test 2: Enable simulated failure
	mockRouter.SetSimulatedFailure("AddPort", true)
	if !mockRouter.ShouldOperationFail("AddPort") {
		t.Error("ShouldOperationFail should return true after SetSimulatedFailure")
	}

	// Test 3: Verify operation counts are updated
	ops := mockRouter.GetOperationCounts()
	if count, exists := ops["SetSimulatedFailure"]; !exists || count != 1 {
		t.Errorf("Expected SetSimulatedFailure count to be 1, got %v", ops)
	}

	// Test 4: Create a simple port config and verify failure works
	ctx := context.Background()
	config := routers.PortConfig{
		Name:      "test/fail:http",
		DstPort:   8080,
		FwdPort:   80,
		DstIP:     "192.168.1.100",
		Protocol:  "tcp",
		Enabled:   true,
		Interface: "wan",
		SrcIP:     "any",
	}

	err := mockRouter.AddPort(ctx, config)
	if err == nil {
		t.Error("Expected AddPort to fail due to simulated failure")
	} else {
		t.Logf("✅ AddPort correctly failed with simulated failure: %v", err)
	}

	// Test 5: Disable simulated failure and try again
	mockRouter.SetSimulatedFailure("AddPort", false)
	if mockRouter.ShouldOperationFail("AddPort") {
		t.Error("ShouldOperationFail should return false after disabling")
	}

	err2 := mockRouter.AddPort(ctx, config)
	if err2 != nil {
		t.Errorf("Expected AddPort to succeed after disabling failure, got: %v", err2)
	} else {
		t.Log("✅ AddPort succeeded after disabling simulated failure")
	}

	t.Log("✅ Mock router simulated failure test passed")
}
