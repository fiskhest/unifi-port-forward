package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"kube-router-port-forward/config"
	ctrl "sigs.k8s.io/controller-runtime"
)

// TestReconcile_FailedCleanup_ServiceDeletion tests scenarios where
// cleanup fails during service deletion
func TestReconcile_FailedCleanup_ServiceDeletion(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create a service with port forwarding
	service := env.CreateTestService("default", "cleanup-test",
		map[string]string{config.FilterAnnotation: "http:8080"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.100")

	// Create and reconcile service to create port forward rule
	if err := env.CreateService(ctx, service); err != nil {
		t.Fatalf("Failed to create cleanup-test service: %v", err)
	}

	result, err := env.ReconcileService(service)
	env.AssertReconcileSuccess(t, result, err)

	// Verify rule exists
	env.AssertRuleExistsByName(t, "default/cleanup-test:http")

	// Enable simulated failure for RemovePort operation
	env.MockRouter.SetSimulatedFailure("RemovePort", true)

	// Delete service
	if err := env.DeleteServiceByName(ctx, "default", "cleanup-test"); err != nil {
		t.Fatalf("Failed to delete cleanup-test service: %v", err)
	}

	// Reset operation counts to ensure clean state
	env.MockRouter.ResetOperationCounts()

	// Enable simulated failure for RemovePort operation
	env.MockRouter.SetSimulatedFailure("RemovePort", true)

	// Reconcile deletion - should fail due to simulated failure
	_, err = env.Controller.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "cleanup-test",
			Namespace: "default",
		},
	})

	// Should fail due to cleanup failure
	if err == nil {
		t.Errorf("Expected cleanup failure during service deletion, but got none. Operation counts: %v", env.MockRouter.GetOperationCounts())
	}

	// Verify RemovePort was attempted
	ops := env.MockRouter.GetOperationCounts()
	if count, exists := ops["RemovePort"]; !exists || count == 0 {
		t.Errorf("Expected RemovePort to be attempted during cleanup, got: %v", ops)
	}

	// Disable simulated failure
	env.MockRouter.SetSimulatedFailure("RemovePort", false)

	// Reconcile again - should succeed
	result, err = env.Controller.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "cleanup-test",
			Namespace: "default",
		},
	})
	env.AssertReconcileSuccess(t, result, err)

	// Verify rule is finally deleted
	env.AssertRuleDoesNotExistByName(t, "default/cleanup-test:http")

	t.Log("✅ Failed cleanup service deletion test passed")
}

// TestReconcile_RetryLogic_FailedOperations tests retry logic
// for failed operations
func TestReconcile_RetryLogic_FailedOperations(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create a service
	service := env.CreateTestService("default", "retry-test",
		map[string]string{config.FilterAnnotation: "http:8080"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.100")

	// Create service
	if err := env.CreateService(ctx, service); err != nil {
		t.Fatalf("Failed to create retry-test service: %v", err)
	}

	// Enable simulated failure for AddPort operation
	env.MockRouter.SetSimulatedFailure("AddPort", true)

	// First reconciliation attempt - should fail
	result, err := env.ReconcileService(service)
	if err == nil {
		t.Error("Expected AddPort failure on first attempt, but got none")
	}

	// Verify rule doesn't exist due to failure
	env.AssertRuleDoesNotExistByName(t, "default/retry-test:http")

	// Disable simulated failure
	env.MockRouter.SetSimulatedFailure("AddPort", false)

	// Second reconciliation attempt - should succeed
	result, err = env.ReconcileService(service)
	env.AssertReconcileSuccess(t, result, err)

	// Verify rule is created on retry
	env.AssertRuleExistsByName(t, "default/retry-test:http")

	// Verify operation counts
	ops := env.MockRouter.GetOperationCounts()
	if ops["AddPort"] < 2 {
		t.Errorf("Expected at least 2 AddPort calls due to retry, got %d", ops["AddPort"])
	}

	t.Log("✅ Retry logic failed operations test passed")
}

// TestReconcile_PartialFailure_Scenarios tests scenarios where
// operations partially fail
func TestReconcile_PartialFailure_Scenarios(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create multiple services
	services := []struct {
		name        string
		namespace   string
		annotations map[string]string
		ports       []corev1.ServicePort
		lbIP        string
	}{
		{
			name:        "partial-service-1",
			namespace:   "default",
			annotations: map[string]string{config.FilterAnnotation: "http:8080"},
			ports:       []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
			lbIP:        "192.168.1.100",
		},
		{
			name:        "partial-service-2",
			namespace:   "default",
			annotations: map[string]string{config.FilterAnnotation: "https:8081"},
			ports:       []corev1.ServicePort{{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP}},
			lbIP:        "192.168.1.101",
		},
	}

	// Create both services
	for i, svc := range services {
		service := env.CreateTestService(svc.namespace, svc.name, svc.annotations, svc.ports, svc.lbIP)
		if err := env.CreateService(ctx, service); err != nil {
			t.Fatalf("Failed to create service %d: %v", i+1, err)
		}

		// Simulate failure for only the second service
		if i == 1 {
			env.MockRouter.SetSimulatedFailure("AddPort", true)
		}

		// Reconcile
		result, err := env.ReconcileService(service)
		if i == 1 {
			// Second service should fail
			if err == nil {
				t.Error("Expected AddPort failure for partial-service-2, but got none")
			}
		} else {
			// First service should succeed
			env.AssertReconcileSuccess(t, result, err)
		}

		// Reset failure for next iteration
		if i == 1 {
			env.MockRouter.SetSimulatedFailure("AddPort", false)
		}
	}

	// Verify only first service rule exists
	env.AssertRuleExistsByName(t, "default/partial-service-1:http")
	env.AssertRuleDoesNotExistByName(t, "default/partial-service-2:https")

	// Verify operation counts reflect partial success
	ops := env.MockRouter.GetOperationCounts()
	if ops["AddPort"] != 2 {
		t.Errorf("Expected exactly 2 AddPort calls, got %d", ops["AddPort"])
	}

	t.Log("✅ Partial failure scenarios test passed")
}

// TestReconcile_RouterCommunication_Failures tests router communication
// failure scenarios
func TestReconcile_RouterCommunication_Failures(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create a service
	service := env.CreateTestService("default", "comm-test",
		map[string]string{config.FilterAnnotation: "http:8080"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.100")

	// Create service
	if err := env.CreateService(ctx, service); err != nil {
		t.Fatalf("Failed to create comm-test service: %v", err)
	}

	// Test various router communication failures
	failureScenarios := []struct {
		operation   string
		description string
	}{
		{"ListAllPortForwards", "Failed to list existing rules"},
		{"AddPort", "Failed to create new rule"},
		{"UpdatePort", "Failed to update existing rule"},
		{"RemovePort", "Failed to delete rule"},
	}

	for _, scenario := range failureScenarios {
		// Reset operation counts
		env.MockRouter.ResetOperationCounts()

		// Enable simulated failure
		env.MockRouter.SetSimulatedFailure(scenario.operation, true)

		// Attempt reconciliation
		_, err := env.ReconcileService(service)
		if err == nil {
			t.Errorf("Expected failure for %s scenario, but got none", scenario.description)
		}

		// Disable simulated failure
		env.MockRouter.SetSimulatedFailure(scenario.operation, false)

		// Verify operation was attempted
		ops := env.MockRouter.GetOperationCounts()
		if count, exists := ops[scenario.operation]; !exists || count == 0 {
			t.Errorf("Expected %s operation to be attempted in %s scenario", scenario.operation, scenario.description)
		}
	}

	t.Log("✅ Router communication failures test passed")
}
