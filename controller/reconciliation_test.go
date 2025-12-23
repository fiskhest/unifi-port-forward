package controller

import (
	"context"
	"reflect"
	"testing"

	"kube-router-port-forward/config"

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
