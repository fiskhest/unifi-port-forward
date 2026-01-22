package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/event"
	"unifi-port-forward/pkg/config"
)

// TestFinalizerFix_SimpleStaleObject tests the core fix for stale service objects
func TestFinalizerFix_SimpleStaleObject(t *testing.T) {
	predicate := ServiceChangePredicate{}

	// Test case 1: Service with finalizer should be processed (existing behavior)
	serviceWithFinalizer := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-service",
			Namespace:  "default",
			Finalizers: []string{config.FinalizerLabel},
		},
	}
	deleteEvent := event.DeleteEvent{
		Object: serviceWithFinalizer,
	}
	if !predicate.Delete(deleteEvent) {
		t.Error("Expected service with finalizer to be processed for deletion")
	}

	// Test case 2: Service with annotation but no finalizer should be processed (new behavior - orphaned cleanup)
	serviceWithAnnotation := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
			Annotations: map[string]string{
				config.FilterAnnotation: "8080:8081:tcp",
			},
		},
	}
	deleteEvent = event.DeleteEvent{
		Object: serviceWithAnnotation,
	}
	if !predicate.Delete(deleteEvent) {
		t.Error("Expected service with annotation but no finalizer to be processed for orphaned cleanup")
	}

	// Test case 3: Service with neither should not be processed
	serviceWithNeither := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
	}
	deleteEvent = event.DeleteEvent{
		Object: serviceWithNeither,
	}
	if predicate.Delete(deleteEvent) {
		t.Error("Expected service with neither finalizer nor annotation to NOT be processed")
	}

	t.Log("✅ Finalizer fix predicate test passed")
}
