package testutils

import (
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	"kube-router-port-forward/config"
	"kube-router-port-forward/helpers"
)

// TestMultiPortService_ValidAnnotation tests multi-port service with valid annotation
func TestMultiPortService_ValidAnnotation(t *testing.T) {
	// Clear port tracking for test isolation
	helpers.ClearPortConflictTracking()

	// Create a multi-port service with annotation
	service := CreateTestMultiPortService(
		"multi-service",
		"default",
		[]TestPort{
			{Name: "http", Port: 8080, Protocol: v1.ProtocolTCP},
			{Name: "https", Port: 443, Protocol: v1.ProtocolTCP},
			{Name: "metrics", Port: 9090, Protocol: v1.ProtocolTCP},
		},
		"192.168.1.100",
		"http:8080,https:8443,metrics:9090",
	)

	// Test getPortConfigs function
	lbIP := helpers.GetLBIP(service)
	portConfigs, err := helpers.GetPortConfigs(service, lbIP, config.FilterAnnotation)
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
		if pc.FwdPort != int(helpers.GetServicePortByName(service, portName).Port) {
			t.Errorf("Expected internal port %d for %s, got %d", helpers.GetServicePortByName(service, portName).Port, portName, pc.FwdPort)
		}
	}
}

// TestServiceWithoutAnnotation_Skipped tests that services without annotation are skipped
func TestServiceWithoutAnnotation_Skipped(t *testing.T) {
	// Create a service without annotation
	service := CreateTestMultiPortService(
		"no-annotation-service",
		"default",
		[]TestPort{
			{Name: "http", Port: 80, Protocol: v1.ProtocolTCP},
		},
		"192.168.1.100",
		"", // No annotation
	)

	// Test getPortConfigs function - should return error
	lbIP := helpers.GetLBIP(service)
	_, err := helpers.GetPortConfigs(service, lbIP, config.FilterAnnotation)
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
	service := CreateTestServiceWithInvalidAnnotation(
		"invalid-service",
		"default",
		"192.168.1.100",
		"http:invalid_port",
	)

	// Test getPortConfigs function - should return error
	lbIP := helpers.GetLBIP(service)
	_, err := helpers.GetPortConfigs(service, lbIP, config.FilterAnnotation)
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
	service := CreateTestMultiPortService(
		"missing-port-service",
		"default",
		[]TestPort{
			{Name: "http", Port: 80, Protocol: v1.ProtocolTCP},
		},
		"192.168.1.100",
		"nonexistent:8080",
	)

	// Test getPortConfigs function - should return error
	lbIP := helpers.GetLBIP(service)
	_, err := helpers.GetPortConfigs(service, lbIP, config.FilterAnnotation)
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
	helpers.ClearPortConflictTracking()

	// Create first service
	service1 := CreateTestMultiPortService(
		"service1",
		"default",
		[]TestPort{
			{Name: "http", Port: 80, Protocol: v1.ProtocolTCP},
		},
		"192.168.1.100",
		"http:8080",
	)

	// First service should succeed
	lbIP1 := helpers.GetLBIP(service1)
	_, err1 := helpers.GetPortConfigs(service1, lbIP1, config.FilterAnnotation)
	if err1 != nil {
		t.Errorf("First service should succeed: %v", err1)
	}

	// Create second service with conflicting port
	service2 := CreateTestMultiPortService(
		"service2",
		"default",
		[]TestPort{
			{Name: "web", Port: 8080, Protocol: v1.ProtocolTCP},
		},
		"192.168.1.101",
		"web:8080", // Same external port as service1
	)

	// Second service should fail due to port conflict
	lbIP2 := helpers.GetLBIP(service2)
	_, err2 := helpers.GetPortConfigs(service2, lbIP2, config.FilterAnnotation)
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
	helpers.ClearPortConflictTracking()

	// Create a service with default port mapping
	service := CreateTestMultiPortService(
		"default-service",
		"default",
		[]TestPort{
			{Name: "http", Port: 80, Protocol: v1.ProtocolTCP},
			{Name: "https", Port: 443, Protocol: v1.ProtocolTCP},
		},
		"192.168.1.100",
		"http,https", // Default mapping - external = service port
	)

	lbIP := helpers.GetLBIP(service)
	portConfigs, err := helpers.GetPortConfigs(service, lbIP, config.FilterAnnotation)
	if err != nil {
		t.Fatalf("Failed to get port configs: %v", err)
	}

	// Verify external ports match service ports
	for _, pc := range portConfigs {
		portName := strings.TrimPrefix(pc.Name, "default/default-service:")
		servicePort := helpers.GetServicePortByName(service, portName)

		if pc.DstPort != int(servicePort.Port) {
			t.Errorf("Expected external port %d for %s, got %d", servicePort.Port, portName, pc.DstPort)
		}
	}
}
