package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"kube-router-port-forward/config"
)

// TestReconcile_SimpleMultipleServices tests basic multiple service scenarios
func TestReconcile_SimpleMultipleServices(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create simple multiple services
	service1 := env.CreateTestService("default", "simple-service-1",
		map[string]string{config.FilterAnnotation: "http:9010"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.10")

	service2 := env.CreateTestService("default", "simple-service-2",
		map[string]string{config.FilterAnnotation: "http:9011"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.11")

	// Create both services
	if err := env.CreateService(ctx, service1); err != nil {
		t.Fatalf("Failed to create simple-service-1: %v", err)
	}

	if err := env.CreateService(ctx, service2); err != nil {
		t.Fatalf("Failed to create simple-service-2: %v", err)
	}

	// Reconcile both services
	result, err := env.ReconcileService(service1)
	env.AssertReconcileSuccess(t, result, err)

	result, err = env.ReconcileService(service2)
	env.AssertReconcileSuccess(t, result, err)

	// Verify both rules exist
	env.AssertRuleExistsByName(t, "default/simple-service-1:http")
	env.AssertRuleExistsByName(t, "default/simple-service-2:http")

	t.Log("✅ Simple multiple services test passed")
}
