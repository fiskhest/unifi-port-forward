package controller

import (
	"testing"

	"unifi-port-forwarder/pkg/config"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// TestServiceChangePredicate_Delete tests the delete predicate specifically
func TestServiceChangePredicate_Delete(t *testing.T) {
	tests := []struct {
		name              string
		hasFinalizer      bool
		hasAnnotation     bool
		shouldAllowDelete bool
	}{
		{
			name:              "Service with finalizer should be processed",
			hasFinalizer:      true,
			hasAnnotation:     true,
			shouldAllowDelete: true,
		},
		{
			name:              "Service with finalizer but no annotation should be processed",
			hasFinalizer:      true,
			hasAnnotation:     false,
			shouldAllowDelete: true,
		},
		{
			name:              "Service without finalizer should be processed (orphaned cleanup)",
			hasFinalizer:      false,
			hasAnnotation:     true,
			shouldAllowDelete: true,
		},
		{
			name:              "Service with neither should not be processed",
			hasFinalizer:      false,
			hasAnnotation:     false,
			shouldAllowDelete: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := ServiceChangePredicate{}

			// Create test service
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-service",
					Namespace:   "default",
					Annotations: make(map[string]string),
				},
			}

			// Add finalizer if specified
			if tt.hasFinalizer {
				service.Finalizers = append(service.Finalizers, config.FinalizerLabel)
			}

			// Add annotation if specified
			if tt.hasAnnotation {
				service.Annotations[config.FilterAnnotation] = "8080:8081:tcp"
			}

			// Create delete event
			deleteEvent := event.DeleteEvent{
				Object: service,
			}

			// Test predicate
			result := predicate.Delete(deleteEvent)

			if result != tt.shouldAllowDelete {
				t.Errorf("Expected predicate.Delete() to return %v, got %v for test: %s",
					tt.shouldAllowDelete, result, tt.name)
			}
		})
	}
}

// TestServiceChangePredicate_Delete_NonFinalized tests that services without finalizers but with annotations are processed (orphaned cleanup)
func TestServiceChangePredicate_Delete_NonFinalized(t *testing.T) {
	predicate := ServiceChangePredicate{}

	// Create service without finalizer but with annotation
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
			Annotations: map[string]string{
				config.FilterAnnotation: "8080:8081:tcp",
			},
		},
	}
	deleteEvent := event.DeleteEvent{
		Object: service,
	}

	// Test that predicate DOES allow the event (for orphaned rule cleanup)
	if !predicate.Delete(deleteEvent) {
		t.Error("Expected predicate to allow delete event for service with annotation but no finalizer (orphaned cleanup)")
	}
}
