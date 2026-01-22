package helpers

import (
	"sync"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestRuleBelongsToService(t *testing.T) {
	testCases := []struct {
		name        string
		ruleName    string
		namespace   string
		serviceName string
		expected    bool
	}{
		// Exact matches
		{
			name:        "exact match with port",
			ruleName:    "default/web:http",
			namespace:   "default",
			serviceName: "web",
			expected:    true,
		},
		{
			name:        "exact match with complex service name",
			ruleName:    "prod/web-service:https",
			namespace:   "prod",
			serviceName: "web-service",
			expected:    true,
		},
		// Different services - should NOT match
		{
			name:        "different service name prefix",
			ruleName:    "default/web-service2:http",
			namespace:   "default",
			serviceName: "web",
			expected:    false,
		},
		{
			name:        "different service name suffix",
			ruleName:    "default/web2:http",
			namespace:   "default",
			serviceName: "web",
			expected:    false,
		},
		{
			name:        "different namespace",
			ruleName:    "prod/web:http",
			namespace:   "default",
			serviceName: "web",
			expected:    false,
		},
		// Edge cases
		{
			name:        "no port separator",
			ruleName:    "default/web",
			namespace:   "default",
			serviceName: "web",
			expected:    false,
		},
		{
			name:        "no namespace separator",
			ruleName:    "web:http",
			namespace:   "default",
			serviceName: "web",
			expected:    false,
		},
		{
			name:        "similar but different service name",
			ruleName:    "default/web-service:http",
			namespace:   "default",
			serviceName: "web",
			expected:    false,
		},
		{
			name:        "service name is prefix of rule service",
			ruleName:    "default/webapp:http",
			namespace:   "default",
			serviceName: "web",
			expected:    false,
		},
		{
			name:        "rule service is prefix of service name",
			ruleName:    "default/web:http",
			namespace:   "default",
			serviceName: "webapp",
			expected:    false,
		},
		// Complex cases
		{
			name:        "multiple dashes and numbers",
			ruleName:    "kube-system/api-server-v2:8080",
			namespace:   "kube-system",
			serviceName: "api-server-v2",
			expected:    true,
		},
		{
			name:        "similar with different numbers",
			ruleName:    "kube-system/api-server-v3:8080",
			namespace:   "kube-system",
			serviceName: "api-server-v2",
			expected:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := RuleBelongsToService(tc.ruleName, tc.namespace, tc.serviceName)
			if result != tc.expected {
				t.Errorf("RuleBelongsToService(%q, %q, %q) = %v; expected %v",
					tc.ruleName, tc.namespace, tc.serviceName, result, tc.expected)
			}
		})
	}
}

func TestUnmarkPortUsed(t *testing.T) {
	// Clear any existing tracking
	ClearPortConflictTracking()

	// Mark a port as used
	port := 8080
	serviceKey := "test/service"
	markPortUsed(port, serviceKey)

	// Verify port is marked
	if err := CheckPortConflict(port, serviceKey); err != nil {
		t.Errorf("Expected no conflict for own service, got error: %v", err)
	}

	// Unmark the port
	UnmarkPortUsed(port)

	// Verify port is no longer marked
	if err := CheckPortConflict(port, "different/service"); err != nil {
		t.Errorf("Expected no conflict after unmarking, got error: %v", err)
	}
}

func TestUnmarkPortsForService(t *testing.T) {
	// Clear any existing tracking
	ClearPortConflictTracking()

	// Mark multiple ports for a service
	serviceKey := "test/multiport-service"
	ports := []int{80, 443, 8080}

	for _, port := range ports {
		markPortUsed(port, serviceKey)
	}

	// Verify all ports are marked
	for _, port := range ports {
		if err := CheckPortConflict(port, serviceKey); err != nil {
			t.Errorf("Expected no conflict for own service on port %d, got error: %v", port, err)
		}
	}

	// Unmark all ports for the service
	UnmarkPortsForService(serviceKey)

	// Verify all ports are no longer marked
	for _, port := range ports {
		if err := CheckPortConflict(port, "different/service"); err != nil {
			t.Errorf("Expected no conflict after unmarking service on port %d, got error: %v", port, err)
		}
	}
}

func TestPortConflictTracking_ConcurrentAccess(t *testing.T) {
	// Clear any existing tracking
	ClearPortConflictTracking()

	const numGoroutines = 10
	const numOperations = 100

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	// Launch multiple goroutines to test concurrent access
	for i := range numGoroutines {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := range numOperations {
				port := goroutineID*100 + j
				serviceKey := "test/concurrent-service"

				// Mark port
				markPortUsed(port, serviceKey)

				// Check conflict
				err := CheckPortConflict(port, serviceKey)
				if err != nil {
					errors <- err
					return
				}

				// Unmark port
				UnmarkPortUsed(port)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent access error: %v", err)
	}
}

func TestClearPortConflictTracking_InProduction(t *testing.T) {
	// This test documents that ClearPortConflictTracking should not be used in production
	// It's only for test isolation

	// Mark some ports
	markPortUsed(8080, "test/service1")
	markPortUsed(9090, "test/service2")

	// Clear all tracking
	ClearPortConflictTracking()

	// Verify all tracking is cleared
	if err := CheckPortConflict(8080, "any/service"); err != nil {
		t.Errorf("Expected no conflict after clearing tracking, got error: %v", err)
	}

	if err := CheckPortConflict(9090, "any/service"); err != nil {
		t.Errorf("Expected no conflict after clearing tracking, got error: %v", err)
	}
}

func TestParseIntField(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"80", 80},
		{"443", 443},
		{"8080", 8080},
		{"0", 0},
		{"", 0},          // Empty string returns 0
		{"invalid", 0},   // Invalid string returns 0
		{"-1", 0},        // Negative numbers return 0
		{"-100", 0},      // Negative numbers return 0
		{"99999", 99999}, // Large valid number
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseIntField(tt.input)
			if result != tt.expected {
				t.Errorf("ParseIntField(%q) = %d; expected %d", tt.input, result, tt.expected)
			}
		})
	}
}
