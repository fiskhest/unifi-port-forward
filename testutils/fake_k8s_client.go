package testutils

import (
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FakeKubernetesClient simulates Kubernetes operations for testing
type FakeKubernetesClient struct {
	Services map[string]*v1.Service
	mu       sync.RWMutex
}

// NewFakeKubernetesClient creates a new fake Kubernetes client
func NewFakeKubernetesClient() *FakeKubernetesClient {
	return &FakeKubernetesClient{
		Services: make(map[string]*v1.Service),
	}
}

// AddService adds a service to the fake client
func (f *FakeKubernetesClient) AddService(service *v1.Service) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := fmt.Sprintf("%s/%s", service.Namespace, service.Name)
	f.Services[key] = service
}

// GetService gets a service from the fake client
func (f *FakeKubernetesClient) GetService(namespace, name string) (*v1.Service, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	key := fmt.Sprintf("%s/%s", namespace, name)
	service, exists := f.Services[key]
	return service, exists
}

// UpdateService updates a service in the fake client
func (f *FakeKubernetesClient) UpdateService(service *v1.Service) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := fmt.Sprintf("%s/%s", service.Namespace, service.Name)
	f.Services[key] = service
}

// DeleteService deletes a service from the fake client
func (f *FakeKubernetesClient) DeleteService(namespace, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := fmt.Sprintf("%s/%s", namespace, name)
	delete(f.Services, key)
}

// GetServiceCount returns the number of services
func (f *FakeKubernetesClient) GetServiceCount() int {
	f.mu.RLock()
	defer f.mu.RUnlock()

	return len(f.Services)
}

// ClearServices clears all services
func (f *FakeKubernetesClient) ClearServices() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.Services = make(map[string]*v1.Service)
}

// CreateTestLoadBalancerService creates a test LoadBalancer service
func CreateTestLoadBalancerService(name, namespace string, port int32, ip string, annotations map[string]string) *v1.Service {
	if annotations == nil {
		annotations = make(map[string]string)
	}

	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{
				{
					Port:     port,
					Protocol: v1.ProtocolTCP,
				},
			},
		},
		Status: v1.ServiceStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{
					{
						IP:       ip,
						Hostname: fmt.Sprintf("%s.%s.svc.cluster.local", name, namespace),
					},
				},
			},
		},
	}

	return service
}

// CreateTestLoadBalancerServiceNoIP creates a test LoadBalancer service without IP
func CreateTestLoadBalancerServiceNoIP(name, namespace string, port int32, annotations map[string]string) *v1.Service {
	if annotations == nil {
		annotations = make(map[string]string)
	}

	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{
				{
					Port:     port,
					Protocol: v1.ProtocolTCP,
				},
			},
		},
		Status: v1.ServiceStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{}, // Empty ingress - no IP
			},
		},
	}

	return service
}

// CreateTestLoadBalancerServiceWithMultipleIPs creates a test LoadBalancer service with multiple IPs
func CreateTestLoadBalancerServiceWithMultipleIPs(name, namespace string, port int32, ips []string, annotations map[string]string) *v1.Service {
	if annotations == nil {
		annotations = make(map[string]string)
	}

	var ingress []v1.LoadBalancerIngress
	for i, ip := range ips {
		ingress = append(ingress, v1.LoadBalancerIngress{
			IP:       ip,
			Hostname: fmt.Sprintf("%s-%d.%s.svc.cluster.local", name, i, namespace),
		})
	}

	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{
				{
					Port:     port,
					Protocol: v1.ProtocolTCP,
				},
			},
		},
		Status: v1.ServiceStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: ingress,
			},
		},
	}

	return service
}

// CreateTestClusterIPService creates a test ClusterIP service
func CreateTestClusterIPService(name, namespace string, port int32) *v1.Service {
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeClusterIP,
			Ports: []v1.ServicePort{
				{
					Port:     port,
					Protocol: v1.ProtocolTCP,
				},
			},
		},
	}

	return service
}

// ServiceEvent represents a service lifecycle event for testing
type ServiceEvent struct {
	Type       string // "add", "update", "delete"
	Service    *v1.Service
	OldService *v1.Service // for update events
}

// ServiceEventTracker tracks service events for testing
type ServiceEventTracker struct {
	Events []ServiceEvent
	mu     sync.RWMutex
}

// NewServiceEventTracker creates a new service event tracker
func NewServiceEventTracker() *ServiceEventTracker {
	return &ServiceEventTracker{
		Events: make([]ServiceEvent, 0),
	}
}

// AddEvent adds an event to the tracker
func (t *ServiceEventTracker) AddEvent(eventType string, service *v1.Service, oldService *v1.Service) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.Events = append(t.Events, ServiceEvent{
		Type:       eventType,
		Service:    service,
		OldService: oldService,
	})
}

// GetEvents returns all events
func (t *ServiceEventTracker) GetEvents() []ServiceEvent {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]ServiceEvent, len(t.Events))
	copy(result, t.Events)
	return result
}

// GetEventCount returns the number of events
func (t *ServiceEventTracker) GetEventCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return len(t.Events)
}

// ClearEvents clears all events
func (t *ServiceEventTracker) ClearEvents() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.Events = make([]ServiceEvent, 0)
}

// HasEventType checks if an event of the specified type occurred
func (t *ServiceEventTracker) HasEventType(eventType string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, event := range t.Events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

// GetEventsByType returns events of the specified type
func (t *ServiceEventTracker) GetEventsByType(eventType string) []ServiceEvent {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []ServiceEvent
	for _, event := range t.Events {
		if event.Type == eventType {
			result = append(result, event)
		}
	}
	return result
}

// HasServiceEvent checks if an event occurred for a specific service
func (t *ServiceEventTracker) HasServiceEvent(eventType, serviceName, namespace string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, event := range t.Events {
		if event.Type == eventType &&
			event.Service.Name == serviceName &&
			event.Service.Namespace == namespace {
			return true
		}
	}
	return false
}
