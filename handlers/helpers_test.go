package handlers

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestGetLBIP tests the GetLBIP helper function
func TestGetLBIP(t *testing.T) {
	// Test service with LoadBalancer IP
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
