package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

// TestPortForwardReconciler_Init tests controller initialization
func TestPortForwardReconciler_Init(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	// Test initialization
	controller := env.Controller

	// Cache should be initialized with default TTL
	// This is indirectly tested by successful creation
	if controller == nil {
		t.Error("Controller should not be nil after initialization")
	}

	t.Log("Controller initialization test passed")
}

// TestPortForwardReconciler_IPChange_UpdateCorrectly tests IP change logic
func TestPortForwardReconciler_IPChange_UpdateCorrectly(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	controller := env.Controller

	// Create service with initial IP
	service := env.CreateTestService("default", "test-service", map[string]string{
		"kube-port-forward-controller/ports": "http:8080",
	}, []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}}, "192.168.1.100")

	// Create initial port forward rule in mock router that we'll later update
	initialRule := env.CreatePortForwardRule("default/test-service:http", "192.168.1.100", "8080", "80", "TCP")
	env.MockRouter.PortForwards = append(env.MockRouter.PortForwards, initialRule)

	// Test that updateDestinationIPs works with IP change
	ctx := context.Background()
	// This should not error - it should generate port configs with current IP (192.168.1.100)
	// then attempt to update them to new IP (192.168.1.101)
	err := controller.updateDestinationIPs(ctx, service, "192.168.1.101")
	if err != nil {
		t.Errorf("updateDestinationIPs should not fail: %v", err)
	}

	// Verify that the port forward rule was updated with new IP
	foundRule, found, _ := env.MockRouter.CheckPort(ctx, 8080)
	if !found {
		t.Error("Port forward rule should still exist after update")
	} else if foundRule.DestinationIP != "192.168.1.101" {
		t.Errorf("Expected destination IP to be updated to 192.168.1.101, got %s", foundRule.DestinationIP)
	}

	t.Log("IP change logic test passed")
}

// TestPortForwardReconciler_shouldProcessService tests service annotation validation
func TestPortForwardReconciler_shouldProcessService(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	controller := env.Controller

	// Test service without annotation
	serviceWithoutAnnotation := env.CreateTestService("default", "test-service-1", nil,
		[]corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}}, "192.168.1.100")

	if controller.shouldProcessService(context.Background(), serviceWithoutAnnotation, "") {
		t.Error("Service without port forwarding annotation should not be processed")
	}

	t.Log("Service processing logic tests passed")
}

// TestPortForwardReconciler_ParseIntField tests parseIntField helper
func TestPortForwardReconciler_ParseIntField(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	controller := env.Controller

	// Test valid integer string
	result := controller.parseIntField("123")
	if result != 123 {
		t.Errorf("Expected 123, got %d", result)
	}

	// Test empty string
	result = controller.parseIntField("")
	if result != 0 {
		t.Errorf("Expected 0 for empty string, got %d", result)
	}

	// Test invalid string
	result = controller.parseIntField("invalid")
	if result != 0 {
		t.Errorf("Expected 0 for invalid string, got %d", result)
	}

	t.Log("parseIntField test passed")
}
