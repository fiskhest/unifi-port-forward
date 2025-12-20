package controller

import (
	"testing"
	"time"

	"kube-router-port-forward/testutils"

	"github.com/filipowm/go-unifi/unifi"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ControllerTestEnv provides a test environment for controller tests
type ControllerTestEnv struct {
	MockRouter *testutils.MockRouter
	Controller *PortForwardReconciler
	Clock      *testutils.MockClock
}

// NewControllerTestEnv creates a new test environment
func NewControllerTestEnv(t *testing.T) *ControllerTestEnv {
	// Create mock router
	mockRouter := testutils.NewMockRouter()

	// Create mock clock starting at a fixed time
	startTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	mockClock := testutils.NewMockClock(startTime)

	// Create controller
	controller := &PortForwardReconciler{
		Router: mockRouter,
	}

	return &ControllerTestEnv{
		MockRouter: mockRouter,
		Controller: controller,
		Clock:      mockClock,
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

// CreateTestServiceWithPortAnnotation creates a test service with port forwarding annotation
func (env *ControllerTestEnv) CreateTestServiceWithPortAnnotation(namespace, name, portAnnotation, lbIP string) *corev1.Service {
	annotations := map[string]string{
		"kube-port-forward-controller/ports": portAnnotation,
	}

	ports := []corev1.ServicePort{
		{
			Name:     "http",
			Port:     80,
			Protocol: corev1.ProtocolTCP,
		},
	}

	return env.CreateTestService(namespace, name, annotations, ports, lbIP)
}

// CreatePortForwardRule creates a mock port forward rule
func (env *ControllerTestEnv) CreatePortForwardRule(name, dstIP, dstPort, fwdPort, protocol string) unifi.PortForward {
	return unifi.PortForward{
		ID:            "test-id-" + name,
		Name:          name,
		DestinationIP: dstIP,
		DstPort:       dstPort,
		FwdPort:       fwdPort,
		Proto:         protocol,
		Enabled:       true,
		PfwdInterface: "wan",
		Src:           "any",
	}
}

// AddPortForwardRule adds a port forward rule to mock router
func (env *ControllerTestEnv) AddPortForwardRule(rule unifi.PortForward) {
	env.MockRouter.AddPortForwardRule(rule)
}

// GetPortForwardCount returns the number of port forward rules
func (env *ControllerTestEnv) GetPortForwardCount() int {
	return env.MockRouter.GetPortForwardCount()
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

// AdvanceTime advances the mock clock
func (env *ControllerTestEnv) AdvanceTime(d time.Duration) {
	env.Clock.Advance(d)
}

// SetCurrentTime sets the mock clock to a specific time
func (env *ControllerTestEnv) SetCurrentTime(t time.Time) {
	env.Clock.SetTime(t)
}

// GetCurrentTime returns the current mock time
func (env *ControllerTestEnv) GetCurrentTime() time.Time {
	return env.Clock.Now()
}
