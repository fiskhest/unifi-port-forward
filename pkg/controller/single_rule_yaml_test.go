package controller

import (
	"context"
	"testing"

	"unifi-port-forwarder/pkg/config"

	corev1 "k8s.io/api/core/v1"
)

// TestReconcile_SingleRuleYaml_PortRemoval tests the exact user scenario:
// 1. Apply single-rule.yaml equivalent
// 2. Edit away https port from web-service
// 3. Verify no rollback validation failures
func TestReconcile_SingleRuleYaml_PortRemoval(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	// Step 1: Create web-service with both ports (from single-rule.yaml)
	webService := env.CreateTestService("unifi-port-forwarder", "web-service",
		map[string]string{config.FilterAnnotation: "89:http,91:https"},
		[]corev1.ServicePort{
			{Name: "http", Port: 8080, Protocol: corev1.ProtocolTCP},
			{Name: "https", Port: 8181, Protocol: corev1.ProtocolTCP},
		},
		"192.168.72.6")

	// Create the service in the fake k8s client
	if err := env.CreateService(context.Background(), webService); err != nil {
		t.Fatalf("Failed to create web-service: %v", err)
	}

	// Reconcile initial service - should create both port 89 and 91 rules
	_, err := env.ReconcileServiceWithFinalizer(t, webService)
	if err != nil {
		t.Errorf("Failed to reconcile initial web-service: %v", err)
	}

	// Verify both rules are created
	env.AssertRuleExistsByName(t, "unifi-port-forwarder/web-service:http")
	env.AssertRuleExistsByName(t, "unifi-port-forwarder/web-service:https")

	// Step 2: Edit away https port from web-service (the user's scenario)
	updatedWebService := webService.DeepCopy()
	updatedWebService.Annotations = map[string]string{
		config.FilterAnnotation: "89:http", // https port removed
	}
	updatedWebService.ResourceVersion = "2"

	// Update the service in the "cluster"
	if err := env.UpdateService(context.Background(), updatedWebService); err != nil {
		t.Fatalf("Failed to update web-service: %v", err)
	}

	// This is the critical test - should succeed without rollback validation failures
	// The DELETE operation for port 91 should use correct LoadBalancer IP (192.168.72.6)
	_, err = env.ReconcileServiceWithFinalizer(t, updatedWebService)
	if err != nil {
		t.Errorf("UPDATE reconciliation failed (rollback validation issue): %v", err)
	}

	// Verify final state - only http rule should remain
	env.AssertRuleExistsByName(t, "unifi-port-forwarder/web-service:http")
	env.AssertRuleDoesNotExistByName(t, "unifi-port-forwarder/web-service:https")

	t.Log("✅ Single-rule.yaml port removal test passed - no rollback validation errors")
}
