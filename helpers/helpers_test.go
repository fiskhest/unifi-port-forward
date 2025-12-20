package helpers

import (
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kube-router-port-forward/config"
	"kube-router-port-forward/testutils"
)

// TestGetLBIP tests the GetLBIP helper function
func TestGetLBIP(t *testing.T) {
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
		Status: v1.ServiceStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{
					{
						IP:       "192.168.1.100",
						Hostname: "test-service.default.svc.cluster.local",
					},
				},
			},
		},
	}

	ip := GetLBIP(service)
	if ip != "192.168.1.100" {
		t.Errorf("Expected IP 192.168.1.100, got %s", ip)
	}

	// Test service with no LoadBalancer IP
	serviceNoIP := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service-no-ip",
			Namespace: "default",
		},
		Status: v1.ServiceStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{},
			},
		},
	}

	ip = GetLBIP(serviceNoIP)
	if ip != "" {
		t.Errorf("Expected empty IP, got %s", ip)
	}

	// Test service with multiple LoadBalancer IPs
	serviceMultiIP := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service-multi",
			Namespace: "default",
		},
		Status: v1.ServiceStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{
					{
						IP:       "192.168.1.100",
						Hostname: "test-service-multi-0.default.svc.cluster.local",
					},
					{
						IP:       "192.168.1.101",
						Hostname: "test-service-multi-1.default.svc.cluster.local",
					},
				},
			},
		},
	}

	ip = GetLBIP(serviceMultiIP)
	if ip != "192.168.1.100" {
		t.Errorf("Expected first IP 192.168.1.100, got %s", ip)
	}
}

// TestMultiPortService_ValidAnnotation tests multi-port service with valid annotation
func TestMultiPortService_ValidAnnotation(t *testing.T) {
	// Clear port tracking for test isolation
	ClearPortConflictTracking()

	// Create a multi-port service with annotation
	service := testutils.CreateTestMultiPortService(
		"multi-service",
		"default",
		[]testutils.TestPort{
			{Name: "http", Port: 8080, Protocol: v1.ProtocolTCP},
			{Name: "https", Port: 443, Protocol: v1.ProtocolTCP},
			{Name: "metrics", Port: 9090, Protocol: v1.ProtocolTCP},
		},
		"192.168.1.100",
		"http:8080,https:8443,metrics:9090",
	)

	// Test getPortConfigs function
	lbIP := GetLBIP(service)
	portConfigs, err := GetPortConfigs(service, lbIP, config.FilterAnnotation)
	if err != nil {
		t.Fatalf("Failed to get port configs: %v", err)
	}

	// Verify we got 3 port configs
	if len(portConfigs) != 3 {
		t.Errorf("Expected 3 port configs, got %d", len(portConfigs))
	}

	// Verify external port mappings
	expectedMappings := map[string]int{
		"http":    8080,
		"https":   8443,
		"metrics": 9090,
	}

	for _, pc := range portConfigs {
		portName := strings.TrimPrefix(pc.Name, "default/multi-service:")
		expectedPort, exists := expectedMappings[portName]
		if !exists {
			t.Errorf("Unexpected port name: %s", pc.Name)
		}
		if pc.DstPort != expectedPort {
			t.Errorf("Expected external port %d for %s, got %d", expectedPort, portName, pc.DstPort)
		}
		if pc.FwdPort != int(GetServicePortByName(service, portName).Port) {
			t.Errorf("Expected internal port %d for %s, got %d", GetServicePortByName(service, portName).Port, portName, pc.FwdPort)
		}
	}
}

// TestServiceWithoutAnnotation_Skipped tests that services without annotation are skipped
func TestServiceWithoutAnnotation_Skipped(t *testing.T) {
	// Create a service without annotation
	service := testutils.CreateTestMultiPortService(
		"no-annotation-service",
		"default",
		[]testutils.TestPort{
			{Name: "http", Port: 80, Protocol: v1.ProtocolTCP},
		},
		"192.168.1.100",
		"", // No annotation
	)

	// Test getPortConfigs function - should return error
	lbIP := GetLBIP(service)
	_, err := GetPortConfigs(service, lbIP, config.FilterAnnotation)
	if err == nil {
		t.Error("Expected error for service without annotation")
	}

	if !strings.Contains(err.Error(), "no port annotation found") {
		t.Errorf("Expected 'no port annotation found' error, got: %v", err)
	}
}

// TestInvalidAnnotationSyntax_Error tests invalid annotation syntax
func TestInvalidAnnotationSyntax_Error(t *testing.T) {
	// Create a service with invalid annotation
	service := testutils.CreateTestServiceWithInvalidAnnotation(
		"invalid-service",
		"default",
		"192.168.1.100",
		"http:invalid_port",
	)

	// Test getPortConfigs function - should return error
	lbIP := GetLBIP(service)
	_, err := GetPortConfigs(service, lbIP, config.FilterAnnotation)
	if err == nil {
		t.Error("Expected error for invalid annotation syntax")
	}

	if !strings.Contains(err.Error(), "invalid external port") {
		t.Errorf("Expected 'invalid external port' error, got: %v", err)
	}
}

// TestPortNameNotFound_Error tests annotation with non-existent port name
func TestPortNameNotFound_Error(t *testing.T) {
	// Create a service with annotation referencing non-existent port
	service := testutils.CreateTestMultiPortService(
		"missing-port-service",
		"default",
		[]testutils.TestPort{
			{Name: "http", Port: 80, Protocol: v1.ProtocolTCP},
		},
		"192.168.1.100",
		"nonexistent:8080",
	)

	// Test getPortConfigs function - should return error
	lbIP := GetLBIP(service)
	_, err := GetPortConfigs(service, lbIP, config.FilterAnnotation)
	if err == nil {
		t.Error("Expected error for non-existent port name")
	}

	if !strings.Contains(err.Error(), "non-existent port") {
		t.Errorf("Expected 'non-existent port' error, got: %v", err)
	}
}

// TestPortConflictDetection_Error tests port conflict detection
func TestPortConflictDetection_Error(t *testing.T) {
	// Clear port tracking for test isolation
	ClearPortConflictTracking()

	// Create first service
	service1 := testutils.CreateTestMultiPortService(
		"service1",
		"default",
		[]testutils.TestPort{
			{Name: "http", Port: 80, Protocol: v1.ProtocolTCP},
		},
		"192.168.1.100",
		"http:8080",
	)

	// First service should succeed
	lbIP1 := GetLBIP(service1)
	_, err1 := GetPortConfigs(service1, lbIP1, config.FilterAnnotation)
	if err1 != nil {
		t.Errorf("First service should succeed: %v", err1)
	}

	// Create second service with conflicting port
	service2 := testutils.CreateTestMultiPortService(
		"service2",
		"default",
		[]testutils.TestPort{
			{Name: "web", Port: 8080, Protocol: v1.ProtocolTCP},
		},
		"192.168.1.101",
		"web:8080", // Same external port as service1
	)

	// Second service should fail due to port conflict
	lbIP2 := GetLBIP(service2)
	_, err2 := GetPortConfigs(service2, lbIP2, config.FilterAnnotation)
	if err2 == nil {
		t.Error("Expected port conflict error for second service")
	} else {
		t.Logf("Got error: %v", err2)
		if !strings.Contains(err2.Error(), "already used by service") {
			t.Errorf("Expected port conflict error, got: %v", err2)
		}
	}
}

// TestDefaultPortMapping tests default port mapping (external = service port)
func TestDefaultPortMapping(t *testing.T) {
	// Clear port tracking for test isolation
	ClearPortConflictTracking()

	// Create a service with default port mapping
	service := testutils.CreateTestMultiPortService(
		"default-service",
		"default",
		[]testutils.TestPort{
			{Name: "http", Port: 80, Protocol: v1.ProtocolTCP},
			{Name: "https", Port: 443, Protocol: v1.ProtocolTCP},
		},
		"192.168.1.100",
		"http,https", // Default mapping - external = service port
	)

	lbIP := GetLBIP(service)
	portConfigs, err := GetPortConfigs(service, lbIP, config.FilterAnnotation)
	if err != nil {
		t.Fatalf("Failed to get port configs: %v", err)
	}

	// Verify external ports match service ports
	for _, pc := range portConfigs {
		portName := strings.TrimPrefix(pc.Name, "default/default-service:")
		servicePort := GetServicePortByName(service, portName)

		if pc.DstPort != int(servicePort.Port) {
			t.Errorf("Expected external port %d for %s, got %d", servicePort.Port, portName, pc.DstPort)
		}
	}
}
