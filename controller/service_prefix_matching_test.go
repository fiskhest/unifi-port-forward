package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"kube-router-port-forward/config"
	ctrl "sigs.k8s.io/controller-runtime"
)

// TestReconcile_SimilarServiceNames_NoInterference tests that services with similar names
// don't interfere with each other's port forward rules
func TestReconcile_SimilarServiceNames_NoInterference(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create two services with similar names: test-service and test
	// This tests the substring bug scenario
	longService := env.CreateTestService("default", "test-service",
		map[string]string{config.FilterAnnotation: "http:8080"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.100")

	shortService := env.CreateTestService("default", "test",
		map[string]string{config.FilterAnnotation: "https:8443"},
		[]corev1.ServicePort{{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP}},
		"192.168.1.101")

	// Create both services
	if err := env.CreateService(ctx, longService); err != nil {
		t.Fatalf("Failed to create test-service: %v", err)
	}
	if err := env.CreateService(ctx, shortService); err != nil {
		t.Fatalf("Failed to create test service: %v", err)
	}

	// Reconcile both services
	result, err := env.ReconcileService(longService)
	env.AssertReconcileSuccess(t, result, err)

	result, err = env.ReconcileService(shortService)
	env.AssertReconcileSuccess(t, result, err)

	// Verify both services have their rules
	env.AssertRuleExistsByName(t, "default/test-service:http")
	env.AssertRuleExistsByName(t, "default/test:https")

	// Verify rules have correct IPs
	rule := env.MockRouter.GetPortForwardRuleByName("default/test-service:http")
	if rule == nil || rule.Fwd != "192.168.1.100" {
		t.Error("test-service rule doesn't have correct IP")
	}

	rule = env.MockRouter.GetPortForwardRuleByName("default/test:https")
	if rule == nil || rule.Fwd != "192.168.1.101" {
		t.Error("test rule doesn't have correct IP")
	}

	t.Log("✅ Similar service names test passed - no interference detected")
}

// TestReconcile_SubstringServiceNames_CorrectMatching tests the specific bug scenario
// where substring service names could cause incorrect rule matching
func TestReconcile_SubstringServiceNames_CorrectMatching(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create services with substring names: api-service and api
	apiService := env.CreateTestService("default", "api-service",
		map[string]string{config.FilterAnnotation: "http:8080"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.200")

	shortApiService := env.CreateTestService("default", "api",
		map[string]string{config.FilterAnnotation: "http:8081"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.201")

	// Create both services
	if err := env.CreateService(ctx, apiService); err != nil {
		t.Fatalf("Failed to create api-service: %v", err)
	}
	if err := env.CreateService(ctx, shortApiService); err != nil {
		t.Fatalf("Failed to create api service: %v", err)
	}

	// Reconcile both services
	result, err := env.ReconcileService(apiService)
	env.AssertReconcileSuccess(t, result, err)

	result, err = env.ReconcileService(shortApiService)
	env.AssertReconcileSuccess(t, result, err)

	// Verify correct rule names and IPs
	env.AssertRuleExistsByName(t, "default/api-service:http")
	env.AssertRuleExistsByName(t, "default/api:http")

	// Verify the critical test: api service rules should NOT match api service prefix
	apiRules := env.GetRuleNamesWithPrefix("default/api:")
	expectedApiRules := []string{"default/api:http"}

	for i, ruleName := range apiRules {
		if ruleName != expectedApiRules[i] {
			t.Errorf("Expected api rules %v, got %v", expectedApiRules, apiRules)
		}
	}

	// Test the prefix matching bug: ensure api-service rules are not incorrectly matched
	apiServiceRules := env.GetRuleNamesWithPrefix("default/api-service:")
	expectedApiServiceRules := []string{"default/api-service:http"}

	for i, ruleName := range apiServiceRules {
		if ruleName != expectedApiServiceRules[i] {
			t.Errorf("Expected api-service rules %v, got %v", expectedApiServiceRules, apiServiceRules)
		}
	}

	t.Log("✅ Substring service names test passed - correct matching verified")
}

// TestReconcile_DeleteService_OtherUnaffected tests that deleting one service
// doesn't affect port forward rules of services with similar names
func TestReconcile_DeleteService_OtherUnaffected(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create services with similar names: webapp and web
	webappService := env.CreateTestService("default", "webapp",
		map[string]string{config.FilterAnnotation: "http:8080"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.150")

	webService := env.CreateTestService("default", "web",
		map[string]string{config.FilterAnnotation: "https:8081"},
		[]corev1.ServicePort{{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP}},
		"192.168.1.151")

	// Create both services
	if err := env.CreateService(ctx, webappService); err != nil {
		t.Fatalf("Failed to create webapp service: %v", err)
	}
	if err := env.CreateService(ctx, webService); err != nil {
		t.Fatalf("Failed to create web service: %v", err)
	}

	// Reconcile both services to create rules
	result, err := env.ReconcileService(webappService)
	env.AssertReconcileSuccess(t, result, err)

	result, err = env.ReconcileService(webService)
	env.AssertReconcileSuccess(t, result, err)

	// Verify both rules exist
	env.AssertRuleExistsByName(t, "default/webapp:http")
	env.AssertRuleExistsByName(t, "default/web:https")

	// Delete the web service (shorter name)
	if err := env.DeleteServiceByName(ctx, "default", "web"); err != nil {
		t.Fatalf("Failed to delete web service: %v", err)
	}

	// Reconcile deletion
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "web",
			Namespace: "default",
		},
	}
	result, err = env.Controller.Reconcile(ctx, req)
	env.AssertReconcileSuccess(t, result, err)

	// Verify web service rule is deleted but webapp rule remains
	env.AssertRuleDoesNotExistByName(t, "default/web:https")
	env.AssertRuleExistsByName(t, "default/webapp:http") // This should still exist!

	t.Log("✅ Service deletion test passed - other service unaffected")
}

// TestReconcile_ComplexPrefixScenarios tests more complex prefix scenarios
func TestReconcile_ComplexPrefixScenarios(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create multiple services with complex name patterns
	services := []struct {
		name        string
		namespace   string
		annotations map[string]string
		ports       []corev1.ServicePort
		lbIP        string
	}{
		{
			name:        "frontend-v1",
			namespace:   "default",
			annotations: map[string]string{config.FilterAnnotation: "http:8080"},
			ports:       []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
			lbIP:        "192.168.1.100",
		},
		{
			name:        "frontend",
			namespace:   "default",
			annotations: map[string]string{config.FilterAnnotation: "http:8082"},
			ports:       []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
			lbIP:        "192.168.1.101",
		},
		{
			name:        "frontend-v2",
			namespace:   "default",
			annotations: map[string]string{config.FilterAnnotation: "https:8444"},
			ports:       []corev1.ServicePort{{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP}},
			lbIP:        "192.168.1.102",
		},
	}

	// Create and reconcile all services
	for _, svc := range services {
		service := env.CreateTestService(svc.namespace, svc.name, svc.annotations, svc.ports, svc.lbIP)
		if err := env.CreateService(ctx, service); err != nil {
			t.Fatalf("Failed to create service %s: %v", svc.name, err)
		}

		result, err := env.ReconcileService(service)
		env.AssertReconcileSuccess(t, result, err)
	}

	// Verify all rules exist with correct names
	expectedRules := []string{
		"default/frontend-v1:http",
		"default/frontend:http",
		"default/frontend-v2:https",
	}

	for _, ruleName := range expectedRules {
		env.AssertRuleExistsByName(t, ruleName)
	}

	// Verify prefix matching works correctly
	frontendRules := env.GetRuleNamesWithPrefix("default/frontend:")
	if len(frontendRules) != 1 {
		t.Errorf("Expected 1 rule for 'frontend' prefix, got %d: %v", len(frontendRules), frontendRules)
	}

	frontendV1Rules := env.GetRuleNamesWithPrefix("default/frontend-v1:")
	if len(frontendV1Rules) != 1 {
		t.Errorf("Expected 1 rule for 'frontend-v1' prefix, got %d: %v", len(frontendV1Rules), frontendV1Rules)
	}

	// Delete frontend service and ensure only its rule is deleted
	if err := env.DeleteServiceByName(ctx, "default", "frontend"); err != nil {
		t.Fatalf("Failed to delete frontend service: %v", err)
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "frontend",
			Namespace: "default",
		},
	}
	result, err := env.Controller.Reconcile(ctx, req)
	env.AssertReconcileSuccess(t, result, err)

	// Verify only frontend rule is deleted, v1 and v2 remain
	env.AssertRuleDoesNotExistByName(t, "default/frontend:http")
	env.AssertRuleExistsByName(t, "default/frontend-v1:http")
	env.AssertRuleExistsByName(t, "default/frontend-v2:https")

	t.Log("✅ Complex prefix scenarios test passed")
}
