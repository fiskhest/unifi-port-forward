package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"kube-router-port-forward/config"
)

// TestReconcile_SimpleErrorScenario tests basic error simulation
func TestReconcile_SimpleErrorScenario(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create a service
	service := env.CreateTestService("default", "simple-error",
		map[string]string{config.FilterAnnotation: "http:8080"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.100")

	// Enable simulated failure BEFORE reconciliation
	env.MockRouter.SetSimulatedFailure("AddPort", true)

	// Create service
	if err := env.CreateService(ctx, service); err != nil {
		t.Fatalf("Failed to create simple-error service: %v", err)
	}

	// Reconcile - should fail
	_, err := env.ReconcileService(service)
	if err == nil {
		t.Errorf("Expected AddPort failure, but got none. Ops: %v", env.MockRouter.GetOperationCounts())
	}

	// Verify operation was attempted
	ops := env.MockRouter.GetOperationCounts()
	if count, exists := ops["AddPort"]; !exists || count == 0 {
		t.Errorf("Expected AddPort to be attempted, got: %v", ops)
	}

	// Disable failure
	env.MockRouter.SetSimulatedFailure("AddPort", false)

	// Reconcile again - should succeed
	result, err := env.ReconcileService(service)
	env.AssertReconcileSuccess(t, result, err)

	// Verify rule exists
	env.AssertRuleExistsByName(t, "default/simple-error:http")

	t.Log("✅ Simple error scenario test passed")
}
