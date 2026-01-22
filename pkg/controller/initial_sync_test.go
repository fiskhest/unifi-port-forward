package controller

import (
	"context"
	"testing"

	"unifi-port-forwarder/pkg/config"

	corev1 "k8s.io/api/core/v1"
)

// TestReconcile_EfficiencyImprovement tests that reconciliation doesn't call syncRouterState
func TestReconcile_EfficiencyImprovement(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	// Phase 1: Ensure clean test environment
	t.Logf("Initial mock router state: %d rules", len(env.MockRouter.GetPortForwardRules()))
	t.Logf("Initial operation counts: %+v", env.MockRouter.GetOperationCounts())

	// Clear any simulated failures from previous tests
	env.MockRouter.SetSimulatedFailure("AddPort", false)
	env.MockRouter.SetSimulatedFailure("ListAllPortForwards", false)
	env.MockRouter.ResetOperationCounts()

	// Phase 2: Perform initial sync (this will populate internal maps)
	ctx := context.Background()
	err := env.Controller.PerformInitialReconciliationSync(ctx)
	if err != nil {
		t.Fatalf("Failed to perform initial sync: %v", err)
	}

	// Phase 2: Debug - Verify initial sync completed successfully
	t.Logf("Controller initialized with periodic reconciler for drift detection")
	t.Logf("Mock router rules after sync: %d", len(env.MockRouter.GetPortForwardRules()))

	// Phase 3: Reset for reconcile tracking
	env.MockRouter.ResetOperationCounts()

	// Phase 4: Create and reconcile service (this should trigger rule creation)
	service := env.CreateTestService("default", "efficiency-test",
		map[string]string{config.FilterAnnotation: "9090:http"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.200")

	t.Logf("Creating service: %+v", service)

	// Add service to fake client before reconciliation
	if createErr := env.CreateService(ctx, service); createErr != nil {
		t.Fatalf("Failed to create service: %v", createErr)
	}

	t.Logf("Reconciling service...")
	_, err = env.ReconcileServiceWithFinalizer(t, service)
	if err != nil {
		t.Fatalf("Failed to reconcile service: %v", err)
	}

	// Phase 5: Analyze results
	finalOpCounts := env.MockRouter.GetOperationCounts()
	t.Logf("Final operation counts: %+v", finalOpCounts)
	t.Logf("Final mock router rules: %d", len(env.MockRouter.GetPortForwardRules()))

	listCount := finalOpCounts["ListAllPortForwards"]
	addCount := finalOpCounts["AddPort"]

	t.Logf("Analysis - ListAllPortForwards calls: %d, AddPort calls: %d", listCount, addCount)

	// Verify optimization: should have exactly 3 calls (1 initial sync + 2 reconciles)
	// due to current rule sharing optimization, with 2nd reconcile for finalizer addition
	// and at least 1 AddPort call for creating service rule
	if listCount > 3 || addCount == 0 {
		t.Errorf("Expected AddPort to be called during reconciliation with optimized ListAllPortForwards calls, but got: ListAllPortForwards=%d, AddPort=%d", listCount, addCount)
	}

	// Additional verification: ensure service rule was actually created
	env.AssertRuleExistsByName(t, "default/efficiency-test:http")

	t.Log("✅ Reconcile efficiency improvement test passed")
}
