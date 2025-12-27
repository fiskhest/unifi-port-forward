package controller

import (
	"context"
	"reflect"
	"testing"

	"kube-router-port-forward/pkg/config"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// TestReconcile_RealServiceCreation tests actual Reconcile method
func TestReconcile_RealServiceCreation(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	// Defensive check: ensure controller is not nil
	// if env.Controller == nil {
	// 	t.Fatal("Controller should not be nil")
	// 	return
	// }

	// Create test service with port forwarding annotation
	service := env.CreateTestService("default", "test-service",
		map[string]string{config.FilterAnnotation: "http:8080"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.100")

	// Call actual Reconcile method
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      service.Name,
			Namespace: service.Namespace,
		},
	}

	result, err := env.Controller.Reconcile(context.Background(), req)

	// Verify reconciliation success
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
		return
	}
	if !reflect.DeepEqual(result, ctrl.Result{}) {
		t.Errorf("Expected empty result (no requeue), got: %+v", result)
		return
	}

	t.Logf("✅ Service reconciliation test passed - verified service: %s/%s", service.Namespace, service.Name)
}

// TestReconcile_ServiceUpdate_PortChange tests port changes trigger rule updates
func TestReconcile_ServiceUpdate_PortChange(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	// Defensive check: ensure controller is not nil
	// if env.Controller == nil {
	// 	t.Fatal("Controller should not be nil")
	// 	return
	// }

	ctx := context.Background()

	// Create initial service
	initialService := env.CreateTestService("default", "test-service",
		map[string]string{config.FilterAnnotation: "http:8080"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.100")

	// Initial reconciliation
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      initialService.Name,
			Namespace: initialService.Namespace,
		},
	}

	result, err := env.Controller.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("Initial reconciliation failed: %v", err)
		return
	}
	if !reflect.DeepEqual(result, ctrl.Result{}) {
		t.Errorf("Initial reconciliation should not requeue: %+v", result)
		return
	}

	// Update service with new port
	updatedService := initialService.DeepCopy()
	updatedService.Annotations[config.FilterAnnotation] = "http:8081"

	// Update in fake client
	err = env.UpdateService(ctx, updatedService)
	if err != nil {
		t.Fatalf("Failed to update service: %v", err)
	}

	// Second reconciliation
	result, err = env.Controller.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("Update reconciliation failed: %v", err)
		return
	}
	if !reflect.DeepEqual(result, ctrl.Result{}) {
		t.Errorf("Update reconciliation should not requeue: %+v", result)
		return
	}

	t.Logf("✅ Port change reconciliation test passed")
}

// TestReconcile_ServiceDeletion tests service deletion triggers cleanup
func TestReconcile_ServiceDeletion(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	// Defensive check: ensure controller is not nil
	// if env.Controller == nil {
	// 	t.Fatal("Controller should not be nil")
	// 	return
	// }

	ctx := context.Background()

	// Create service with annotation
	service := env.CreateTestService("default", "test-service",
		map[string]string{config.FilterAnnotation: "http:8080"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.100")

	// Initial reconciliation - should create rule
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      service.Name,
			Namespace: service.Namespace,
		},
	}

	result, err := env.Controller.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("Initial reconciliation failed: %v", err)
		return
	}
	if !reflect.DeepEqual(result, ctrl.Result{}) {
		t.Errorf("Initial reconciliation should not requeue: %+v", result)
		return
	}

	// Delete service from fake client (simulating service deletion)
	err = env.DeleteService(ctx, service)
	if err != nil {
		t.Fatalf("Failed to delete service: %v", err)
	}

	// Second reconciliation - should handle deletion gracefully
	result, err = env.Controller.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("Deletion reconciliation failed: %v", err)
		return
	}
	if !reflect.DeepEqual(result, ctrl.Result{}) {
		t.Errorf("Deletion reconciliation should not requeue: %+v", result)
		return
	}

	t.Logf("✅ Service deletion reconciliation test passed")
}

// TestReconcile_NonLoadBalancer_Ignored tests that ClusterIP services are ignored
func TestReconcile_NonLoadBalancer_Ignored(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	// Create ClusterIP service (should be ignored)
	service := env.CreateTestService("default", "clusterip-service",
		map[string]string{config.FilterAnnotation: "http:8080"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.100")

	// Change service type to ClusterIP
	service.Spec.Type = corev1.ServiceTypeClusterIP

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      service.Name,
			Namespace: service.Namespace,
		},
	}

	result, err := env.Controller.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("ClusterIP reconciliation failed: %v", err)
		return
	}
	if !reflect.DeepEqual(result, ctrl.Result{}) {
		t.Errorf("ClusterIP reconciliation should not requeue: %+v", result)
		return
	}

	t.Logf("✅ ClusterIP service ignored test passed")
}

// TestReconcile_NoAnnotation_Ignored tests that services without annotation are ignored
func TestReconcile_NoAnnotation_Ignored(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	// Create LoadBalancer service without annotation
	service := env.CreateTestService("default", "no-annotation-service",
		nil, // no annotations
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.100")

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      service.Name,
			Namespace: service.Namespace,
		},
	}

	result, err := env.Controller.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("No annotation reconciliation failed: %v", err)
		return
	}
	if !reflect.DeepEqual(result, ctrl.Result{}) {
		t.Errorf("No annotation reconciliation should not requeue: %+v", result)
		return
	}

	t.Logf("✅ No annotation service ignored test passed")
}

// TestReconcile_NoLBIP_Ignored tests that services without LoadBalancer IP are ignored
func TestReconcile_NoLBIP_Ignored(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	// Create LoadBalancer service without IP
	service := env.CreateTestService("default", "no-ip-service",
		map[string]string{config.FilterAnnotation: "http:8080"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"") // no IP

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      service.Name,
			Namespace: service.Namespace,
		},
	}

	result, err := env.Controller.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("No IP reconciliation failed: %v", err)
		return
	}
	if !reflect.DeepEqual(result, ctrl.Result{}) {
		t.Errorf("No IP reconciliation should not requeue: %+v", result)
		return
	}

	t.Logf("✅ No LoadBalancer IP service ignored test passed")
}

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
	_, err := env.ReconcileService(service)
	if err == nil {
		t.Error("Expected AddPort failure on first attempt, but got none")
	}

	// Verify rule doesn't exist due to failure
	env.AssertRuleDoesNotExistByName(t, "default/retry-test:http")

	// Disable simulated failure
	env.MockRouter.SetSimulatedFailure("AddPort", false)

	// Second reconciliation attempt - should succeed
	result, err := env.ReconcileService(service)
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

	// Test 1: ListAllPortForwards failure
	env.MockRouter.ResetOperationCounts()
	env.MockRouter.SetSimulatedFailure("ListAllPortForwards", true)
	_, err := env.ReconcileService(service)
	if err == nil {
		t.Error("Expected failure for Failed to list existing rules scenario, but got none")
	}
	env.MockRouter.SetSimulatedFailure("ListAllPortForwards", false)
	ops := env.MockRouter.GetOperationCounts()
	if count, exists := ops["ListAllPortForwards"]; !exists || count == 0 {
		t.Error("Expected ListAllPortForwards operation to be attempted in Failed to list existing rules scenario")
	}

	// Test 2: AddPort failure (create new service to test creation)
	newService := env.CreateTestService("default", "comm-test-2",
		map[string]string{config.FilterAnnotation: "http:8081"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.101")

	if err := env.CreateService(ctx, newService); err != nil {
		t.Fatalf("Failed to create comm-test-2 service: %v", err)
	}

	env.MockRouter.ResetOperationCounts()
	env.MockRouter.SetSimulatedFailure("AddPort", true)
	_, err = env.ReconcileService(newService)
	if err == nil {
		t.Error("Expected failure for Failed to create new rule scenario, but got none")
		t.Logf("Operation counts: %v", env.MockRouter.GetOperationCounts())
	}
	env.MockRouter.SetSimulatedFailure("AddPort", false)
	ops = env.MockRouter.GetOperationCounts()
	if count, exists := ops["AddPort"]; !exists || count == 0 {
		t.Error("Expected AddPort operation to be attempted in Failed to create new rule scenario")
		t.Logf("All operation counts: %v", ops)
	}

	// Test 3: UpdatePort failure - temporarily simplified test
	// TODO: This test needs a proper implementation but for now we'll skip it
	// to focus on getting other tests passing
	// Test 3: UpdatePort failure - temporarily skipped
	// Test 3: UpdatePort failure - create proper update scenario
	// First create a service that will be updated
	updateTestService := env.CreateTestService("default", "update-test",
		map[string]string{config.FilterAnnotation: "http:8083"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.200")

	if err := env.CreateService(ctx, updateTestService); err != nil {
		t.Fatalf("Failed to create update-test service: %v", err)
	}

	// Initial reconciliation to create the rule
	_, err = env.ReconcileService(updateTestService)
	if err != nil {
		t.Fatalf("Failed to initially reconcile update-test service: %v", err)
	}

	// Verify rule was created
	env.AssertRuleExistsByName(t, "default/update-test:http")

	// Now modify the service to trigger an UpdatePort (same port, different IP)
	env.MockRouter.ResetOperationCounts()
	env.MockRouter.SetSimulatedFailure("UpdatePort", true)

	// Update the service IP to trigger an update (keeping same port)
	// Also set change context annotation to simulate IP change detection
	updateTestService.Spec.LoadBalancerIP = "192.168.1.201" // Different IP

	// Set change context annotation to simulate IP change detection
	changeContextJSON := `{"service_key":"default/update-test","ip_changed":true,"old_ip":"192.168.1.200","new_ip":"192.168.1.201"}`
	if updateTestService.Annotations == nil {
		updateTestService.Annotations = make(map[string]string)
	}
	updateTestService.Annotations["kube-port-forward-controller/change-context"] = changeContextJSON

	if err := env.UpdateService(ctx, updateTestService); err != nil {
		t.Fatalf("Failed to update service for UpdatePort test: %v", err)
	}

	// Reconcile should attempt to update the rule and fail
	_, err = env.ReconcileService(updateTestService)
	if err == nil {
		t.Error("Expected failure for Failed to update existing rule scenario, but got none")
	}

	env.MockRouter.SetSimulatedFailure("UpdatePort", false)
	ops = env.MockRouter.GetOperationCounts()
	if count, exists := ops["UpdatePort"]; !exists || count == 0 {
		t.Error("Expected UpdatePort operation to be attempted in Failed to update existing rule scenario")
	}

	t.Log("✅ UpdatePort failure test completed")

	// UpdatePort test skipped - no assertions needed
	t.Log("ℹ️  UpdatePort test completed (skipped)")

	// Test 4: RemovePort failure (delete service to trigger removal)
	deleteService := env.CreateTestService("default", "comm-test-delete",
		map[string]string{config.FilterAnnotation: "http:8084"},
		[]corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		"192.168.1.210")

	if err := env.CreateService(ctx, deleteService); err != nil {
		t.Fatalf("Failed to create comm-test-delete service: %v", err)
	}

	// First reconcile to create rule
	_, err = env.ReconcileService(deleteService)
	if err != nil {
		t.Fatalf("Failed to initially reconcile comm-test-delete service: %v", err)
	}

	env.MockRouter.ResetOperationCounts()
	env.MockRouter.SetSimulatedFailure("RemovePort", true)
	// Delete the service to trigger removal
	if err := env.DeleteServiceByName(ctx, "default", "comm-test-delete"); err != nil {
		t.Fatalf("Failed to delete service for RemovePort test: %v", err)
	}
	_, err = env.Controller.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "comm-test-delete",
			Namespace: "default",
		},
	})
	if err == nil {
		t.Error("Expected failure for Failed to delete rule scenario, but got none")
	}
	env.MockRouter.SetSimulatedFailure("RemovePort", false)
	ops = env.MockRouter.GetOperationCounts()
	if count, exists := ops["RemovePort"]; !exists || count == 0 {
		t.Error("Expected RemovePort operation to be attempted in Failed to delete rule scenario")
	}

	t.Log("✅ Router communication failures test passed")
}

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
		return
	}

	// Verify they have different IPs (no interference)
	if appV2Rule.Fwd == appRule.Fwd {
		t.Error("Rules should have different IPs")
	}

	t.Log("✅ Service rename name conflict test passed")
}

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
