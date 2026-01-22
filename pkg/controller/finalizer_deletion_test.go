package controller

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	// "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"unifi-port-forward/pkg/config"
)

func TestFinalizerDeletion_RealScenario(t *testing.T) {
	// Create test environment
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	// Create a service with finalizer that's marked for deletion
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-service",
			Namespace:         "default",
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
			Finalizers:        []string{config.FinalizerLabel},
		},
	}

	// Add the service to the fake client
	ctx := context.Background()
	err := env.FakeClient.Create(ctx, service)
	if err != nil {
		t.Fatalf("Failed to create test service: %v", err)
	}

	// Test the finalizer cleanup directly
	result, err := env.Controller.handleFinalizerCleanup(ctx, service)

	// Verify no error was returned
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify we're not requeuing (finalizer should be removed)
	if result.Requeue || result.RequeueAfter > 0 {
		t.Fatalf("Expected no requeue, got: Requeue=%v, RequeueAfter=%v", result.Requeue, result.RequeueAfter)
	}

	// Get the updated service to check finalizer was removed
	updatedService := &corev1.Service{}
	err = env.FakeClient.Get(ctx, types.NamespacedName{Name: "test-service", Namespace: "default"}, updatedService)
	if err != nil {
		t.Fatalf("Failed to get updated service: %v", err)
	}

	// CRITICAL: Verify finalizer was removed
	if controllerutil.ContainsFinalizer(updatedService, config.FinalizerLabel) {
		t.Fatal("❌ FINALIZER WAS NOT REMOVED - this would cause kubectl delete to hang forever!")
	}

	t.Log("✅ Finalizer successfully removed during deletion - kubectl delete will not hang!")
}

func TestFinalizerDeletion_WithCleanupErrors(t *testing.T) {
	// Create test environment
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	// Configure mock router to fail cleanup
	env.MockRouter.SetSimulatedFailure("RemovePort", true)

	// Create a service with finalizer that's marked for deletion
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-service",
			Namespace:         "default",
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
			Finalizers:        []string{config.FinalizerLabel},
		},
	}

	// Add the service to the fake client
	ctx := context.Background()
	err := env.FakeClient.Create(ctx, service)
	if err != nil {
		t.Fatalf("Failed to create test service: %v", err)
	}

	// Test the finalizer cleanup with cleanup errors
	result, err := env.Controller.handleFinalizerCleanup(ctx, service)

	// Verify no error was returned (even though cleanup failed)
	if err != nil {
		t.Fatalf("Expected no error despite cleanup failure, got: %v", err)
	}

	// Verify we're not requeuing (finalizer should still be removed)
	if result.Requeue || result.RequeueAfter > 0 {
		t.Fatalf("Expected no requeue despite cleanup failure, got: Requeue=%v, RequeueAfter=%v", result.Requeue, result.RequeueAfter)
	}

	// Get the updated service to check finalizer was removed
	updatedService := &corev1.Service{}
	err = env.FakeClient.Get(ctx, types.NamespacedName{Name: "test-service", Namespace: "default"}, updatedService)
	if err != nil {
		t.Fatalf("Failed to get updated service: %v", err)
	}

	// CRITICAL: Verify finalizer was removed even with cleanup errors
	if controllerutil.ContainsFinalizer(updatedService, config.FinalizerLabel) {
		t.Fatal("❌ FINALIZER WAS NOT REMOVED despite cleanup errors - this would cause kubectl delete to hang forever!")
	}

	t.Log("✅ Finalizer successfully removed even with cleanup errors - kubectl delete will not hang!")
}
