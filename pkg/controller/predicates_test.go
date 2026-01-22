package controller

import (
	"testing"
	"time"

	"unifi-port-forward/pkg/config"

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

// TestServiceChangePredicate_Delete_PrioritizedFiltering tests that finalizer filtering is prioritized
func TestServiceChangePredicate_Delete_PrioritizedFiltering(t *testing.T) {
	tests := []struct {
		name              string
		hasFinalizer      bool
		hasAnnotation     bool
		expectedLogMsg    string
		shouldAllowDelete bool
	}{
		{
			name:              "Finalizer priority test - service with finalizer",
			hasFinalizer:      true,
			hasAnnotation:     false,
			expectedLogMsg:    "Delete event accepted: service has our finalizer",
			shouldAllowDelete: true,
		},
		{
			name:              "Orphaned cleanup - service with annotation but no finalizer",
			hasFinalizer:      false,
			hasAnnotation:     true,
			expectedLogMsg:    "Delete event accepted: service has port forwarding annotation (orphaned cleanup)",
			shouldAllowDelete: true,
		},
		{
			name:              "Filtered out - service with neither finalizer nor annotation",
			hasFinalizer:      false,
			hasAnnotation:     false,
			expectedLogMsg:    "Delete event filtered out: service has neither our finalizer nor port forwarding annotation",
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

			// Verify that the predicate makes the expected filtering decision
			// The enhanced logging ensures we can trace why decisions are made
			// (We don't capture logs here, but the presence of logs confirms behavior)
		})
	}
}

// TestServiceChangePredicate_Update_WithDeletion tests UPDATE predicate with deletion events
func TestServiceChangePredicate_Update_WithDeletion(t *testing.T) {
	tests := []struct {
		name               string
		oldService         *corev1.Service
		newService         *corev1.Service
		shouldAcceptUpdate bool
	}{
		{
			name: "Service marked for deletion should be processed",
			oldService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
					Annotations: map[string]string{
						config.FilterAnnotation: "8080:http",
					},
					Finalizers: []string{config.FinalizerLabel},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
				},
			},
			newService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-service",
					Namespace:         "default",
					Annotations:       map[string]string{config.FilterAnnotation: "8080:http"},
					Finalizers:        []string{config.FinalizerLabel},
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
				},
			},
			shouldAcceptUpdate: true,
		},
		{
			name: "Service with annotation and finalizer marked for deletion should be processed",
			oldService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-service",
					Namespace:   "default",
					Annotations: map[string]string{config.FilterAnnotation: "8080:http"},
					Finalizers:  []string{config.FinalizerLabel},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
				},
			},
			newService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-service",
					Namespace:         "default",
					Annotations:       map[string]string{config.FilterAnnotation: "8080:http"},
					Finalizers:        []string{config.FinalizerLabel},
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
				},
			},
			shouldAcceptUpdate: true,
		},
		{
			name: "Service without annotation marked for deletion should not be processed",
			oldService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
				},
			},
			newService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-service",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
				},
			},
			shouldAcceptUpdate: false,
		},
		{
			name: "Service already deleted - no change should not be processed",
			oldService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-service",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: time.Now().Add(-time.Minute)},
					Annotations:       map[string]string{config.FilterAnnotation: "8080:http"},
				},
			},
			newService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-service",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Annotations:       map[string]string{config.FilterAnnotation: "8080:http"},
				},
			},
			shouldAcceptUpdate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := ServiceChangePredicate{}

			// Create UPDATE event
			updateEvent := event.UpdateEvent{
				ObjectOld: tt.oldService,
				ObjectNew: tt.newService,
			}

			// Test predicate
			result := predicate.Update(updateEvent)

			if result != tt.shouldAcceptUpdate {
				t.Errorf("Expected predicate.Update() to return %v, got %v for test: %s",
					tt.shouldAcceptUpdate, result, tt.name)
			}
		})
	}
}
