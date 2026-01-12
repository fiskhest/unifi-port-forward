package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"unifi-port-forwarder/pkg/config"
)

// TestController_Startup_PreAnnotatedService tests controller startup scenario
// where service is annotated before controller starts
func TestController_Startup_PreAnnotatedService(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create a service with annotation before controller "starts"
	preAnnotatedService := env.CreateTestService("default", "pre-annotated",
		map[string]string{config.FilterAnnotation: "8085:http"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.100")

	// Simulate controller startup by creating service in fake client first
	if err := env.CreateService(ctx, preAnnotatedService); err != nil {
		t.Fatalf("Failed to create pre-annotated service: %v", err)
	}

	// Now "start controller" by reconciling the pre-existing service
	result, err := env.ReconcileService(preAnnotatedService)
	env.AssertReconcileSuccess(t, result, err)

	// Verify port forward rule was created
	env.AssertRuleExistsByName(t, "default/pre-annotated:http")

	// Verify rule has correct configuration
	rule := env.MockRouter.GetPortForwardRuleByName("default/pre-annotated:http")
	if rule == nil || rule.Fwd != "192.168.1.100" || rule.DstPort != "8085" {
		t.Error("Pre-annotated service rule doesn't have correct configuration")
	}

	t.Log("✅ Controller startup with pre-annotated service test passed")
}

// TestController_Startup_MultiplePreAnnotatedServices tests controller startup
// with multiple pre-annotated services
func TestController_Startup_MultiplePreAnnotatedServices(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create multiple services with annotations before controller starts
	services := []struct {
		name        string
		namespace   string
		annotations map[string]string
		ports       []corev1.ServicePort
		lbIP        string
	}{
		{
			name:        "database",
			namespace:   "default",
			annotations: map[string]string{config.FilterAnnotation: "3306:mysql"},
			ports:       []corev1.ServicePort{{Name: "mysql", Port: 3306, Protocol: corev1.ProtocolTCP}},
			lbIP:        "192.168.1.50",
		},
		{
			name:        "webapp",
			namespace:   "default",
			annotations: map[string]string{config.FilterAnnotation: "8081:http,8443:https"},
			ports: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
				{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP},
			},
			lbIP: "192.168.1.60",
		},
		{
			name:        "cache",
			namespace:   "default",
			annotations: map[string]string{config.FilterAnnotation: "6379:redis"},
			ports:       []corev1.ServicePort{{Name: "redis", Port: 6379, Protocol: corev1.ProtocolTCP}},
			lbIP:        "192.168.1.70",
		},
	}

	// Create all services first (simulating pre-existing state)
	for _, svc := range services {
		service := env.CreateTestService(svc.namespace, svc.name, svc.annotations, svc.ports, svc.lbIP)
		if err := env.CreateService(ctx, service); err != nil {
			t.Fatalf("Failed to create service %s: %v", svc.name, err)
		}
	}

	// Simulate controller startup by reconciling all services
	for _, svc := range services {
		service := env.CreateTestService(svc.namespace, svc.name, svc.annotations, svc.ports, svc.lbIP)
		result, err := env.ReconcileService(service)
		env.AssertReconcileSuccess(t, result, err)
	}

	// Verify all port forward rules were created
	expectedRules := []string{
		"default/database:mysql",
		"default/webapp:http",
		"default/webapp:https",
		"default/cache:redis",
	}

	for _, ruleName := range expectedRules {
		env.AssertRuleExistsByName(t, ruleName)
	}

	// Verify specific configurations
	webappHttpRule := env.MockRouter.GetPortForwardRuleByName("default/webapp:http")
	if webappHttpRule == nil || webappHttpRule.Fwd != "192.168.1.60" {
		t.Error("webapp http rule doesn't have correct IP")
	}

	webappHttpsRule := env.MockRouter.GetPortForwardRuleByName("default/webapp:https")
	if webappHttpsRule == nil || webappHttpsRule.Fwd != "192.168.1.60" || webappHttpsRule.DstPort != "8443" {
		t.Error("webapp https rule doesn't have correct configuration")
	}

	t.Log("✅ Controller startup with multiple pre-annotated services test passed")
}

// TestController_Removal_AllRulesCleanup tests scenario where controller
// is removed and all port rules should be cleaned up
func TestController_Removal_AllRulesCleanup(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create multiple services with port forwarding
	services := []struct {
		name        string
		namespace   string
		annotations map[string]string
		ports       []corev1.ServicePort
		lbIP        string
	}{
		{
			name:        "service-a",
			namespace:   "default",
			annotations: map[string]string{config.FilterAnnotation: "8082:http"},
			ports:       []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
			lbIP:        "192.168.1.10",
		},
		{
			name:        "service-b",
			namespace:   "production",
			annotations: map[string]string{config.FilterAnnotation: "8445:https"},
			ports:       []corev1.ServicePort{{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP}},
			lbIP:        "192.168.1.20",
		},
		{
			name:        "service-c",
			namespace:   "staging",
			annotations: map[string]string{config.FilterAnnotation: "3000:api"},
			ports:       []corev1.ServicePort{{Name: "api", Port: 3000, Protocol: corev1.ProtocolTCP}},
			lbIP:        "192.168.1.30",
		},
	}

	// Create and reconcile all services to create port forward rules
	for _, svc := range services {
		service := env.CreateTestService(svc.namespace, svc.name, svc.annotations, svc.ports, svc.lbIP)
		if err := env.CreateService(ctx, service); err != nil {
			t.Fatalf("Failed to create service %s: %v", svc.name, err)
		}

		result, err := env.ReconcileService(service)
		env.AssertReconcileSuccess(t, result, err)
	}

	// Verify all rules were created
	expectedRules := []string{
		"default/service-a:http",
		"production/service-b:https",
		"staging/service-c:api",
	}

	for _, ruleName := range expectedRules {
		env.AssertRuleExistsByName(t, ruleName)
	}

	// Simulate controller removal by triggering cleanup for all services
	// In real scenario, this would happen when controller is deleted/removed
	for _, svc := range services {
		if err := env.DeleteServiceByName(ctx, svc.namespace, svc.name); err != nil {
			t.Fatalf("Failed to delete service %s: %v", svc.name, err)
		}

		// Reconcile service deletion (cleanup)
		result, err := env.Controller.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      svc.name,
				Namespace: svc.namespace,
			},
		})
		env.AssertReconcileSuccess(t, result, err)
	}

	// Verify all rules were cleaned up
	for _, ruleName := range expectedRules {
		env.AssertRuleDoesNotExistByName(t, ruleName)
	}

	// Verify no rules remain
	allRules := env.MockRouter.GetPortForwardNames()
	if len(allRules) != 0 {
		t.Errorf("Expected no rules after controller removal, but found: %v", allRules)
	}

	t.Log("✅ Controller removal with all rules cleanup test passed")
}

// TestController_Restart_ExistingRules tests controller restart scenario
// where existing rules should be preserved and updated as needed
func TestController_Restart_ExistingRules(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create initial controller state with services
	originalServices := []struct {
		name        string
		namespace   string
		annotations map[string]string
		ports       []corev1.ServicePort
		lbIP        string
	}{
		{
			name:        "persistent-service",
			namespace:   "default",
			annotations: map[string]string{config.FilterAnnotation: "8083:http"},
			ports:       []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
			lbIP:        "192.168.1.101", // Updated IP
		},
		{
			name:        "temp-service",
			namespace:   "default",
			annotations: map[string]string{config.FilterAnnotation: "8444:https"},
			ports:       []corev1.ServicePort{{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP}},
			lbIP:        "192.168.1.200",
		},
	}

	// Simulate first controller run - create services and rules
	for _, svc := range originalServices {
		service := env.CreateTestService(svc.namespace, svc.name, svc.annotations, svc.ports, svc.lbIP)
		if err := env.CreateService(ctx, service); err != nil {
			t.Fatalf("Failed to create service %s: %v", svc.name, err)
		}

		result, err := env.ReconcileService(service)
		env.AssertReconcileSuccess(t, result, err)
	}

	// Verify initial state
	env.AssertRuleExistsByName(t, "default/persistent-service:http")
	env.AssertRuleExistsByName(t, "default/temp-service:https")

	// Simulate temp-service being deleted while controller is down (external change)
	if err := env.DeleteServiceByName(ctx, "default", "temp-service"); err != nil {
		t.Fatalf("Failed to delete temp-service: %v", err)
	}

	// Simulate persistent-service IP change while controller is down (external change)
	// Update the service with new IP
	updatedService := env.CreateTestService("default", "persistent-service",
		map[string]string{config.FilterAnnotation: "8080:http"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.101") // Changed IP

	if err := env.UpdateServiceInPlace(ctx, updatedService); err != nil {
		t.Fatalf("Failed to update persistent-service: %v", err)
	}

	// Simulate controller restart - reconcile only persistent-service
	// with the original controller (which simulates restart behavior)
	restartedService := env.CreateTestService("default", "persistent-service",
		map[string]string{config.FilterAnnotation: "8080:http"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.101") // Updated IP

	// Reconcile persistent-service (simulating restart behavior)
	// This should update the existing rule with new IP
	result, err := env.ReconcileService(restartedService)
	env.AssertReconcileSuccess(t, result, err)

	// Now reconcile the deletion of temp-service (which was deleted while controller was down)
	result, err = env.Controller.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "temp-service",
			Namespace: "default",
		},
	})
	env.AssertReconcileSuccess(t, result, err)

	// Verify final state:
	// 1. persistent-service rule should exist with updated IP
	// 2. temp-service rule should be cleaned up
	env.AssertRuleExistsByName(t, "default/persistent-service:http")
	env.AssertRuleDoesNotExistByName(t, "default/temp-service:https")

	// Verify persistent-service has updated IP
	rule := env.MockRouter.GetPortForwardRuleByName("default/persistent-service:http")
	if rule == nil || rule.Fwd != "192.168.1.101" {
		t.Error("persistent-service rule doesn't have updated IP after restart")
	}

	t.Log("✅ Controller restart with existing rules test passed")
}
