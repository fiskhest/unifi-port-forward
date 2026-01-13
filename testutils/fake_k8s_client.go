package testutils

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	client "sigs.k8s.io/controller-runtime/pkg/client"
	"unifi-port-forwarder/pkg/config"
)

// FakeKubernetesClient simulates Kubernetes operations for testing
type FakeKubernetesClient struct {
	Services map[string]*v1.Service
	mu       sync.RWMutex
	scheme   *runtime.Scheme
}

// NewFakeKubernetesClient creates a new fake Kubernetes client
func NewFakeKubernetesClient(t *testing.T, scheme *runtime.Scheme) *FakeKubernetesClient {
	return &FakeKubernetesClient{
		Services: make(map[string]*v1.Service),
		mu:       sync.RWMutex{},
		scheme:   scheme,
	}
}

// Get implements controller-runtime client.Client interface
func (f *FakeKubernetesClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	f.mu.RLock()
	defer f.mu.RUnlock()

	service, exists := f.Services[key.String()]
	if !exists {
		return errors.NewNotFound(v1.Resource("services"), key.Name)
	}

	// Use deep copy to avoid reference issues in tests
	serviceCopy := service.DeepCopy()
	dstValue := reflect.ValueOf(obj).Elem()
	srcValue := reflect.ValueOf(serviceCopy).Elem()
	dstValue.Set(srcValue)

	return nil
}

// Create implements controller-runtime client.Client interface
func (f *FakeKubernetesClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	service, ok := obj.(*v1.Service)
	if !ok {
		return fmt.Errorf("fake client only supports Service objects")
	}

	// Store a deep copy to avoid reference issues
	key := fmt.Sprintf("%s/%s", service.Namespace, service.Name)
	f.Services[key] = service.DeepCopy()
	return nil
}

// Update implements controller-runtime client.Client interface
func (f *FakeKubernetesClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	service, ok := obj.(*v1.Service)
	if !ok {
		return fmt.Errorf("fake client only supports Service objects")
	}

	// Store a deep copy to avoid reference issues
	key := fmt.Sprintf("%s/%s", service.Namespace, service.Name)
	f.Services[key] = service.DeepCopy()
	return nil
}

// Delete implements controller-runtime client.Client interface
func (f *FakeKubernetesClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	service, ok := obj.(*v1.Service)
	if !ok {
		return fmt.Errorf("fake client only supports Service objects")
	}

	key := fmt.Sprintf("%s/%s", service.Namespace, service.Name)
	delete(f.Services, key)
	return nil
}

// List implements controller-runtime client.Client interface
func (f *FakeKubernetesClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	f.mu.RLock()
	defer f.mu.RUnlock()

	serviceList, ok := list.(*v1.ServiceList)
	if !ok {
		return fmt.Errorf("fake client only supports ServiceList")
	}

	services := make([]v1.Service, 0, len(f.Services))
	i := 0
	for _, service := range f.Services {
		services[i] = *service
		i++
	}

	serviceList.Items = services
	return nil
}

// Patch implements controller-runtime client.Client interface (basic implementation)
func (f *FakeKubernetesClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	// For now, just implement as no-op since not used in our controller
	return nil
}

// DeleteAllOf implements controller-runtime client.Client interface (basic implementation)
func (f *FakeKubernetesClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	_, ok := obj.(*v1.Service)
	if !ok {
		return fmt.Errorf("fake client only supports Service objects")
	}

	// Simple implementation - clear all services
	f.Services = make(map[string]*v1.Service)
	return nil
}

// Status implements controller-runtime client.Client interface
func (f *FakeKubernetesClient) Status() client.StatusWriter {
	return &FakeStatusWriter{client: f}
}

// SubResource implements controller-runtime client.Client interface
func (f *FakeKubernetesClient) SubResource(subResource string) client.SubResourceClient {
	return &FakeSubResourceClient{client: f}
}

// FakeStatusWriter implements status updates
type FakeStatusWriter struct {
	client *FakeKubernetesClient
}

func (f *FakeStatusWriter) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	// For now, just call Update on main client - ignore subresource specific options
	return f.client.Update(ctx, subResource)
}

func (f *FakeStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	// For now, just call Update on main client - ignore subresource specific options
	return f.client.Update(ctx, obj)
}

func (f *FakeStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	// For now, just call Patch on main client - ignore subresource specific options
	return f.client.Patch(ctx, obj, patch)
}

// FakeSubResourceClient implements subresource operations
type FakeSubResourceClient struct {
	client *FakeKubernetesClient
}

func (f *FakeSubResourceClient) Get(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceGetOption) error {
	// For now, just call Get on main client
	return f.client.Get(ctx, client.ObjectKeyFromObject(obj), subResource)
}

func (f *FakeSubResourceClient) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	// For now, just call Create on main client with subresource
	return f.client.Create(ctx, subResource)
}

func (f *FakeSubResourceClient) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	// For now, just call Update on main client
	return f.client.Update(ctx, obj)
}

func (f *FakeSubResourceClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	// For now, just call Patch on main client
	return f.client.Patch(ctx, obj, patch)
}

// TestPort represents a port configuration for testing
type TestPort struct {
	Name     string
	Port     int32
	Protocol v1.Protocol
}

// CreateTestMultiPortService creates a test LoadBalancer service with multiple ports
func CreateTestMultiPortService(name, namespace string, ports []TestPort, ip string, annotation string) *v1.Service {
	annotations := make(map[string]string)
	if annotation != "" {
		annotations[config.FilterAnnotation] = annotation
	}

	var servicePorts []v1.ServicePort
	for _, port := range ports {
		servicePorts = append(servicePorts, v1.ServicePort{
			Name:     port.Name,
			Port:     port.Port,
			Protocol: port.Protocol,
		})
	}

	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: v1.ServiceSpec{
			Type:  v1.ServiceTypeLoadBalancer,
			Ports: servicePorts,
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

// CreateTestServiceWithInvalidAnnotation creates a service with invalid annotation
func CreateTestServiceWithInvalidAnnotation(name, namespace string, ip string, invalidAnnotation string) *v1.Service {
	return CreateTestMultiPortService(name, namespace, []TestPort{
		{Name: "http", Port: 80, Protocol: v1.ProtocolTCP},
	}, ip, invalidAnnotation)
}

// Additional interface methods (minimal implementations)
func (f *FakeKubernetesClient) Scheme() *runtime.Scheme {
	return f.scheme
}

func (f *FakeKubernetesClient) RESTMapper() meta.RESTMapper {
	// For now, return nil - not used by our controller
	return nil
}

func (f *FakeKubernetesClient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, nil
}

func (f *FakeKubernetesClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	return true, nil
}
