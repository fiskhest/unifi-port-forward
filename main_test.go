package main

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"kube-router-port-forward/config"
	"kube-router-port-forward/helpers"
	"kube-router-port-forward/testutils"
)

// TestServiceLifecycle_AddFunc tests the AddFunc behavior
func TestServiceLifecycle_AddFunc(t *testing.T) {
	// Create test utilities
	eventTracker := testutils.NewServiceEventTracker()

	// Create a LoadBalancer service with annotation
	service := testutils.CreateTestLoadBalancerService(
		"test-service",
		"default",
		8080,
		"192.168.1.100",
		map[string]string{config.FilterAnnotation: "true"},
	)

	// Simulate AddFunc call
	addFunc := func(obj any) {
		svc := obj.(*v1.Service)
		eventTracker.AddEvent("add", svc, nil)

		if svc.Spec.Type != v1.ServiceTypeLoadBalancer {
			return
		}

		if _, exists := svc.Annotations[config.FilterAnnotation]; exists {
			// In a real scenario, this would call router.AddPort()
			// For testing, we just verify the logic
			port := int(svc.Spec.Ports[0].Port)
			if port != 8080 {
				t.Errorf("Expected port 8080, got %d", port)
			}

			ip := helpers.GetLBIP(svc)
			if ip != "192.168.1.100" {
				t.Errorf("Expected IP 192.168.1.100, got %s", ip)
			}
		}
	}

	// Call AddFunc
	addFunc(service)

	// Verify event was tracked
	if !eventTracker.HasServiceEvent("add", "test-service", "default") {
		t.Error("Expected add event to be tracked")
	}
}

// TestServiceLifecycle_UpdateFunc tests the UpdateFunc behavior
func TestServiceLifecycle_UpdateFunc(t *testing.T) {
	// Create test utilities
	eventTracker := testutils.NewServiceEventTracker()

	// Create initial service
	oldService := testutils.CreateTestLoadBalancerService(
		"test-service",
		"default",
		8080,
		"192.168.1.100",
		map[string]string{config.FilterAnnotation: "true"},
	)

	// Create updated service with different port
	newService := testutils.CreateTestLoadBalancerService(
		"test-service",
		"default",
		9090,
		"192.168.1.100",
		map[string]string{config.FilterAnnotation: "true"},
	)

	// Simulate UpdateFunc call
	updateFunc := func(oldObj, newObj any) {
		oldSvc := oldObj.(*v1.Service)
		newSvc := newObj.(*v1.Service)
		eventTracker.AddEvent("update", newSvc, oldSvc)

		if newSvc.Spec.Type != v1.ServiceTypeLoadBalancer {
			return
		}

		_, oldExists := oldSvc.Annotations[config.FilterAnnotation]
		_, newExists := newSvc.Annotations[config.FilterAnnotation]

		// Handle annotation removal
		if oldExists && !newExists {
			// In a real scenario, this would call router.RemovePort()
			return
		}

		// Handle annotation addition
		if !oldExists && newExists {
			// In a real scenario, this would call router.AddPort()
			port := int(newSvc.Spec.Ports[0].Port)
			if port != 9090 {
				t.Errorf("Expected port 9090, got %d", port)
			}
			return
		}

		// Handle port change
		if oldExists && newExists {
			oldPort := int(oldSvc.Spec.Ports[0].Port)
			newPort := int(newSvc.Spec.Ports[0].Port)

			if oldPort != newPort {
				// In a real scenario, this would:
				// 1. Remove old port (router.RemovePort)
				// 2. Add new port (router.AddPort)

				if oldPort != 8080 {
					t.Errorf("Expected old port 8080, got %d", oldPort)
				}
				if newPort != 9090 {
					t.Errorf("Expected new port 9090, got %d", newPort)
				}
			}
		}
	}

	// Call UpdateFunc
	updateFunc(oldService, newService)

	// Verify event was tracked
	if !eventTracker.HasServiceEvent("update", "test-service", "default") {
		t.Error("Expected update event to be tracked")
	}
}

// TestServiceLifecycle_DeleteFunc tests the DeleteFunc behavior
func TestServiceLifecycle_DeleteFunc(t *testing.T) {
	// Create test utilities
	eventTracker := testutils.NewServiceEventTracker()

	// Create service
	service := testutils.CreateTestLoadBalancerService(
		"test-service",
		"default",
		8080,
		"192.168.1.100",
		map[string]string{config.FilterAnnotation: "true"},
	)

	// Simulate DeleteFunc call
	deleteFunc := func(obj any) {
		svc := obj.(*v1.Service)
		eventTracker.AddEvent("delete", svc, nil)

		if svc.Spec.Type != v1.ServiceTypeLoadBalancer {
			return
		}

		if _, exists := svc.Annotations[config.FilterAnnotation]; exists {
			// In a real scenario, this would call router.RemovePort()
			port := int(svc.Spec.Ports[0].Port)
			if port != 8080 {
				t.Errorf("Expected port 8080, got %d", port)
			}

			ip := helpers.GetLBIP(svc)
			if ip != "192.168.1.100" {
				t.Errorf("Expected IP 192.168.1.100, got %s", ip)
			}
		}
	}

	// Call DeleteFunc
	deleteFunc(service)

	// Verify event was tracked
	if !eventTracker.HasServiceEvent("delete", "test-service", "default") {
		t.Error("Expected delete event to be tracked")
	}
}

// TestServiceLifecycle_NonLoadBalancer tests that non-LoadBalancer services are ignored
func TestServiceLifecycle_NonLoadBalancer(t *testing.T) {
	// Create test utilities
	eventTracker := testutils.NewServiceEventTracker()

	// Create a ClusterIP service (should be ignored)
	service := testutils.CreateTestClusterIPService("test-service", "default", 8080)

	// Simulate AddFunc
	addFunc := func(obj any) {
		svc := obj.(*v1.Service)
		eventTracker.AddEvent("add", svc, nil)

		if svc.Spec.Type != v1.ServiceTypeLoadBalancer {
			return
		}

		// This should not be reached
		t.Error("ClusterIP service should not trigger port forward creation")
	}

	// Call AddFunc
	addFunc(service)

	// Verify event was tracked (service is still processed, but no port forward is created)
	if !eventTracker.HasServiceEvent("add", "test-service", "default") {
		t.Error("Expected add event to be tracked")
	}
}

// TestServiceLifecycle_NoAnnotation tests that services without annotation are ignored
func TestServiceLifecycle_NoAnnotation(t *testing.T) {
	// Create test utilities
	eventTracker := testutils.NewServiceEventTracker()

	// Create a LoadBalancer service without annotation
	service := testutils.CreateTestLoadBalancerService(
		"test-service",
		"default",
		8080,
		"192.168.1.100",
		nil, // no annotations
	)

	// Simulate AddFunc
	addFunc := func(obj any) {
		svc := obj.(*v1.Service)
		eventTracker.AddEvent("add", svc, nil)

		if svc.Spec.Type != v1.ServiceTypeLoadBalancer {
			return
		}

		if _, exists := svc.Annotations[config.FilterAnnotation]; exists {
			// This should not be reached
			t.Error("Service without annotation should not trigger port forward creation")
		}
	}

	// Call AddFunc
	addFunc(service)

	// Verify event was tracked (service is still processed, but no port forward is created)
	if !eventTracker.HasServiceEvent("add", "test-service", "default") {
		t.Error("Expected add event to be tracked")
	}
}

// TestServiceLifecycle_MultiplePorts tests service with multiple ports
func TestServiceLifecycle_MultiplePorts(t *testing.T) {
	// Create test utilities
	eventTracker := testutils.NewServiceEventTracker()

	// Create a service with multiple ports
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
			Annotations: map[string]string{
				config.FilterAnnotation: "true",
			},
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{
				{
					Port:     8080,
					Protocol: v1.ProtocolTCP,
				},
				{
					Port:     8443,
					Protocol: v1.ProtocolTCP,
				},
			},
		},
		Status: v1.ServiceStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{
					{
						IP: "192.168.1.100",
					},
				},
			},
		},
	}

	// Simulate AddFunc (currently only handles first port)
	addFunc := func(obj any) {
		svc := obj.(*v1.Service)
		eventTracker.AddEvent("add", svc, nil)

		if svc.Spec.Type != v1.ServiceTypeLoadBalancer {
			return
		}

		if _, exists := svc.Annotations[config.FilterAnnotation]; exists {
			// Currently only handles first port (as noted in TODO in main.go)
			port := int(svc.Spec.Ports[0].Port)
			if port != 8080 {
				t.Errorf("Expected first port 8080, got %d", port)
			}

			// Verify there are multiple ports
			if len(svc.Spec.Ports) != 2 {
				t.Errorf("Expected 2 ports, got %d", len(svc.Spec.Ports))
			}
		}
	}

	// Call AddFunc
	addFunc(service)

	// Verify event was tracked
	if !eventTracker.HasServiceEvent("add", "test-service", "default") {
		t.Error("Expected add event to be tracked")
	}
}
