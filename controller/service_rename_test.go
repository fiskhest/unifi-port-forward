package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"kube-router-port-forward/config"
	ctrl "sigs.k8s.io/controller-runtime"
)

// TestReconcile_ServiceRename_CleanupAndCreation tests service rename scenario
// where old service rules should be cleaned up and new rules created
func TestReconcile_ServiceRename_CleanupAndCreation(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create initial service with port forwarding annotation
	oldService := env.CreateTestService("default", "old-service",
		map[string]string{config.FilterAnnotation: "http:8080"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.100")

	// Create and reconcile old service
	if err := env.CreateService(ctx, oldService); err != nil {
		t.Fatalf("Failed to create old service: %v", err)
	}

	result, err := env.ReconcileService(oldService)
	env.AssertReconcileSuccess(t, result, err)

	// Verify old service rules exist
	env.AssertRuleExistsByName(t, "default/old-service:http")

	// Simulate service rename by creating new service and deleting old one
	newService := env.CreateTestService("default", "new-service",
		map[string]string{config.FilterAnnotation: "http:8080"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.100") // Same IP

	// Delete old service
	if err := env.DeleteServiceByName(ctx, "default", "old-service"); err != nil {
		t.Fatalf("Failed to delete old service: %v", err)
	}

	// Create new service
	if err := env.CreateService(ctx, newService); err != nil {
		t.Fatalf("Failed to create new service: %v", err)
	}

	// Reconcile old service deletion
	result, err = env.Controller.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "old-service",
			Namespace: "default",
		},
	})
	env.AssertReconcileSuccess(t, result, err)

	// Reconcile new service creation
	result, err = env.ReconcileService(newService)
	env.AssertReconcileSuccess(t, result, err)

	// Verify old rules are cleaned up and new rules are created
	env.AssertRuleDoesNotExistByName(t, "default/old-service:http")
	env.AssertRuleExistsByName(t, "default/new-service:http")

	// Verify new rule has correct configuration
	rule := env.MockRouter.GetPortForwardRuleByName("default/new-service:http")
	if rule == nil || rule.Fwd != "192.168.1.100" || rule.DstPort != "8080" {
		t.Error("New service rule doesn't have correct configuration")
	}

	t.Log("✅ Service rename cleanup and creation test passed")
}

// TestReconcile_ServiceRename_IPChange tests service rename with IP change
func TestReconcile_ServiceRename_IPChange(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create initial service
	oldService := env.CreateTestService("default", "database-service",
		map[string]string{config.FilterAnnotation: "mysql:3306"},
		[]corev1.ServicePort{{Name: "mysql", Port: 3306, Protocol: corev1.ProtocolTCP}},
		"192.168.1.50")

	// Create and reconcile old service
	if err := env.CreateService(ctx, oldService); err != nil {
		t.Fatalf("Failed to create database-service: %v", err)
	}

	result, err := env.ReconcileService(oldService)
	env.AssertReconcileSuccess(t, result, err)

	// Verify old rule exists
	env.AssertRuleExistsByName(t, "default/database-service:mysql")

	// Rename service to new name with different IP
	newService := env.CreateTestService("default", "new-database",
		map[string]string{config.FilterAnnotation: "mysql:3306"},
		[]corev1.ServicePort{{Name: "mysql", Port: 3306, Protocol: corev1.ProtocolTCP}},
		"192.168.1.60") // Different IP

	// Delete old service
	if err := env.DeleteServiceByName(ctx, "default", "database-service"); err != nil {
		t.Fatalf("Failed to delete database-service: %v", err)
	}

	// Create new service
	if err := env.CreateService(ctx, newService); err != nil {
		t.Fatalf("Failed to create new-database: %v", err)
	}

	// Reconcile old service deletion
	result, err = env.Controller.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "database-service",
			Namespace: "default",
		},
	})
	env.AssertReconcileSuccess(t, result, err)

	// Reconcile new service
	result, err = env.ReconcileService(newService)
	env.AssertReconcileSuccess(t, result, err)

	// Verify rules are correctly managed
	env.AssertRuleDoesNotExistByName(t, "default/database-service:mysql")
	env.AssertRuleExistsByName(t, "default/new-database:mysql")

	// Verify new rule has updated IP
	rule := env.MockRouter.GetPortForwardRuleByName("default/new-database:mysql")
	if rule == nil || rule.Fwd != "192.168.1.60" {
		t.Error("New service rule doesn't have correct updated IP")
	}

	t.Log("✅ Service rename with IP change test passed")
}

// TestReconcile_ServiceRename_AnnotationChange tests service rename with annotation changes
func TestReconcile_ServiceRename_AnnotationChange(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create initial service with single port
	oldService := env.CreateTestService("default", "web-service",
		map[string]string{config.FilterAnnotation: "http:8080"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.100")

	// Create and reconcile old service
	if err := env.CreateService(ctx, oldService); err != nil {
		t.Fatalf("Failed to create web-service: %v", err)
	}

	result, err := env.ReconcileService(oldService)
	env.AssertReconcileSuccess(t, result, err)

	// Verify old rule exists
	env.AssertRuleExistsByName(t, "default/web-service:http")

	// Create new service with multiple port annotations
	newService := env.CreateTestService("default", "frontend-service",
		map[string]string{config.FilterAnnotation: "http:8080,https:8443"},
		[]corev1.ServicePort{
			{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP},
		},
		"192.168.1.100")

	// Delete old service
	if err := env.DeleteServiceByName(ctx, "default", "web-service"); err != nil {
		t.Fatalf("Failed to delete web-service: %v", err)
	}

	// Create new service
	if err := env.CreateService(ctx, newService); err != nil {
		t.Fatalf("Failed to create frontend-service: %v", err)
	}

	// Reconcile old service deletion
	result, err = env.Controller.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "web-service",
			Namespace: "default",
		},
	})
	env.AssertReconcileSuccess(t, result, err)

	// Reconcile new service
	result, err = env.ReconcileService(newService)
	env.AssertReconcileSuccess(t, result, err)

	// Verify old rule is deleted and new rules are created
	env.AssertRuleDoesNotExistByName(t, "default/web-service:http")
	env.AssertRuleExistsByName(t, "default/frontend-service:http")
	env.AssertRuleExistsByName(t, "default/frontend-service:https")

	t.Log("✅ Service rename with annotation change test passed")
}

// TestReconcile_ServiceRename_NameConflict tests edge case where service is renamed
// to a name that already exists
func TestReconcile_ServiceRename_NameConflict(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create two existing services
	service1 := env.CreateTestService("default", "app-v1",
		map[string]string{config.FilterAnnotation: "http:8080"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.100")

	service2 := env.CreateTestService("default", "app-v2",
		map[string]string{config.FilterAnnotation: "http:8081"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.101")

	// Create both services
	if err := env.CreateService(ctx, service1); err != nil {
		t.Fatalf("Failed to create app-v1: %v", err)
	}
	if err := env.CreateService(ctx, service2); err != nil {
		t.Fatalf("Failed to create app-v2: %v", err)
	}

	// Reconcile both services
	result, err := env.ReconcileService(service1)
	env.AssertReconcileSuccess(t, result, err)

	result, err = env.ReconcileService(service2)
	env.AssertReconcileSuccess(t, result, err)

	// Verify both rules exist
	env.AssertRuleExistsByName(t, "default/app-v1:http")
	env.AssertRuleExistsByName(t, "default/app-v2:http")

	// Simulate rename attempt: delete app-v1 and create new app (would conflict with app-v2)
	if err := env.DeleteServiceByName(ctx, "default", "app-v1"); err != nil {
		t.Fatalf("Failed to delete app-v1: %v", err)
	}

	// Create new service with name that would conflict with existing one
	conflictService := env.CreateTestService("default", "app",
		map[string]string{config.FilterAnnotation: "http:8082"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.102")

	// Create the conflicting service
	if err := env.CreateService(ctx, conflictService); err != nil {
		t.Fatalf("Failed to create conflicting app service: %v", err)
	}

	// Reconcile deletion and creation
	result, err = env.Controller.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "app-v1",
			Namespace: "default",
		},
	})
	env.AssertReconcileSuccess(t, result, err)

	result, err = env.ReconcileService(conflictService)
	env.AssertReconcileSuccess(t, result, err)

	// Verify state: app-v1 rule deleted, app-v2 unchanged, app rule created
	env.AssertRuleDoesNotExistByName(t, "default/app-v1:http")
	env.AssertRuleExistsByName(t, "default/app-v2:http") // Should still exist
	env.AssertRuleExistsByName(t, "default/app:http")    // New rule created

	// Verify the new app service doesn't interfere with app-v2
	appV2Rule := env.MockRouter.GetPortForwardRuleByName("default/app-v2:http")
	appRule := env.MockRouter.GetPortForwardRuleByName("default/app:http")

	if appV2Rule == nil || appRule == nil {
		t.Error("Both rules should exist")
	}

	// Verify they have different IPs (no interference)
	if appV2Rule.Fwd == appRule.Fwd {
		t.Error("Rules should have different IPs")
	}

	t.Log("✅ Service rename name conflict test passed")
}
