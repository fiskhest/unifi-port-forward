package controller

import (
	"context"
	"reflect"
	"testing"
	"time"

	"kube-router-port-forward/testutils"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ControllerTestEnv provides a test environment for controller tests
type ControllerTestEnv struct {
	MockRouter *testutils.MockRouter
	Controller *PortForwardReconciler
	Clock      *testutils.MockClock
	FakeClient *testutils.FakeKubernetesClient
}

// NewControllerTestEnv creates a new test environment
func NewControllerTestEnv(t *testing.T) *ControllerTestEnv {
	// Create mock router
	mockRouter := testutils.NewMockRouter()

	// Create mock clock starting at a fixed time
	startTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	mockClock := testutils.NewMockClock(startTime)

	// Create scheme for controller runtime
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)

	// Create simplified fake client for reconciliation testing
	fakeClient := testutils.NewFakeKubernetesClient(t, scheme)

	// Create controller with client assignment
	controller := &PortForwardReconciler{
		Client: fakeClient,
		Router: mockRouter,
		Scheme: scheme,
	}

	return &ControllerTestEnv{
		MockRouter: mockRouter,
		Controller: controller,
		Clock:      mockClock,
		FakeClient: fakeClient,
	}
}

// Cleanup cleans up test environment
func (env *ControllerTestEnv) Cleanup() {
	// Clean up any resources if needed
}

// CreateTestService creates a test service with the given parameters
func (env *ControllerTestEnv) CreateTestService(namespace, name string, annotations map[string]string, ports []corev1.ServicePort, lbIP string) *corev1.Service {
	if annotations == nil {
		annotations = make(map[string]string)
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: ports,
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{},
			},
		},
	}

	// Add LoadBalancer IP if provided
	if lbIP != "" {
		service.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
			{
				IP:       lbIP,
				Hostname: name + "." + namespace + ".svc.cluster.local",
			},
		}
	}

	return service
}

// CreateService adds a service to the fake Kubernetes client
func (env *ControllerTestEnv) CreateService(ctx context.Context, service *corev1.Service) error {
	if env.FakeClient != nil {
		return env.FakeClient.Create(ctx, service)
	}
	return nil
}

// UpdateService updates a service in the fake Kubernetes client
func (env *ControllerTestEnv) UpdateService(ctx context.Context, service *corev1.Service) error {
	if env.FakeClient != nil {
		return env.FakeClient.Update(ctx, service)
	}
	return nil
}

// DeleteService deletes a service from the fake Kubernetes client
func (env *ControllerTestEnv) DeleteService(ctx context.Context, service *corev1.Service) error {
	if env.FakeClient != nil {
		return env.FakeClient.Delete(ctx, service)
	}
	return nil
}

// ReconcileService calls the controller's Reconcile method for a given service
func (env *ControllerTestEnv) ReconcileService(service *corev1.Service) (ctrl.Result, error) {
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      service.Name,
			Namespace: service.Namespace,
		},
	}
	return env.Controller.Reconcile(context.Background(), req)
}

// AssertReconcileSuccess verifies that reconciliation succeeded with no requeue
func (env *ControllerTestEnv) AssertReconcileSuccess(t *testing.T, result ctrl.Result, err error) {
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if !reflect.DeepEqual(result, ctrl.Result{}) {
		t.Errorf("Expected empty result (no requeue), got: %+v", result)
	}
}

// AssertReconcileError verifies that reconciliation returned expected error
func (env *ControllerTestEnv) AssertReconcileError(t *testing.T, expectedErr string, result ctrl.Result, err error) {
	if err == nil {
		t.Error("Expected error, got nil")
		return
	}
	if expectedErr != "" && err.Error() != expectedErr {
		t.Errorf("Expected error '%s', got '%s'", expectedErr, err.Error())
	}
	if !reflect.DeepEqual(result, ctrl.Result{}) {
		t.Errorf("Expected empty result (no requeue), got: %+v", result)
	}
}

// AssertPortForwardRuleExists verifies a port forward rule exists
func (env *ControllerTestEnv) AssertPortForwardRuleExists(t *testing.T, port, dstIP string) {
	if !env.MockRouter.HasPortForward(port, dstIP) {
		t.Errorf("Expected port forward rule for port %s to %s to exist", port, dstIP)
	}
}

// AssertPortForwardRuleDoesNotExist verifies a port forward rule does not exist
func (env *ControllerTestEnv) AssertPortForwardRuleDoesNotExist(t *testing.T, port, dstIP string) {
	if env.MockRouter.HasPortForward(port, dstIP) {
		t.Errorf("Expected port forward rule for port %s to %s to not exist", port, dstIP)
	}
}
