package controller

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/filipowm/go-unifi/unifi"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDriftDetector_AnalyzeAllServicesDrift(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	// Create a service with perfectly matching router rules
	services := []*corev1.Service{
		createTestServiceWithLB("default", "perfect-service", map[string]string{
			"unifi-port-forward.fiskhe.st/ports": "8080:http",
		}, "192.168.1.100"),
	}

	// Create matching router rules
	routerRules := []*unifi.PortForward{
		{
			Name:    "default/perfect-service:http",
			DstPort: "8080",
			FwdPort: "8080",
			Fwd:     "192.168.1.100",
			Proto:   "tcp",
			Enabled: true,
		},
	}

	// Create drift detector
	detector := &DriftDetector{
		Router: nil,
	}

	// Test analysis
	analyses, err := detector.AnalyzeAllServicesDrift(context.Background(), services, routerRules)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Should have no drift
	analysis := analyses[0]
	if analysis.HasDrift {
		t.Error("Expected no drift for perfectly matched service")
	}

	if len(analysis.MissingRules) != 0 {
		t.Errorf("Expected 0 missing rules, got %d", len(analysis.MissingRules))
	}

	if len(analysis.WrongRules) != 0 {
		t.Errorf("Expected 0 wrong rules, got %d", len(analysis.WrongRules))
	}

	if len(analysis.ExtraRules) != 0 {
		t.Errorf("Expected 0 extra rules, got %d", len(analysis.ExtraRules))
	}
}

func TestDriftDetector_AggressiveOwnership(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	// Test setup

	// Create a service
	services := []*corev1.Service{
		createTestServiceWithLB("default", "my-service", map[string]string{
			"unifi-port-forward.fiskhe.st/ports": "8080:http",
		}, "192.168.1.100"),
	}

	// Create router rule with same port+protocol but different name (manual rule)
	routerRules := []*unifi.PortForward{
		{
			Name:    "someone-elses-manual-rule",
			DstPort: "8080",
			FwdPort: "80",
			Fwd:     "192.168.1.100",
			Proto:   "tcp",
			Enabled: true,
		},
	}

	// Create drift detector
	detector := &DriftDetector{
		Router: nil,
	}

	// Test analysis
	analyses, err := detector.AnalyzeAllServicesDrift(context.Background(), services, routerRules)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Should detect ownership conflict
	analysis := analyses[0]
	if !analysis.HasDrift {
		t.Error("Expected drift due to ownership conflict")
	}

	if len(analysis.WrongRules) != 1 {
		t.Errorf("Expected 1 wrong rule, got %d", len(analysis.WrongRules))
	}

	wrongRule := analysis.WrongRules[0]
	if wrongRule.MismatchType != "ownership" {
		t.Errorf("Expected 'ownership' mismatch, got '%s'", wrongRule.MismatchType)
	}

	if wrongRule.Current.Name != "someone-elses-manual-rule" {
		t.Errorf("Expected current rule name 'someone-elses-manual-rule', got '%s'", wrongRule.Current.Name)
	}

	if wrongRule.Desired.Name != "default/my-service:http" {
		t.Errorf("Expected desired rule name 'default/my-service:http', got '%s'", wrongRule.Desired.Name)
	}
}

func TestDriftDetector_FwdPortChangeDetection(t *testing.T) {
	env := NewControllerTestEnv(t)
	defer env.Cleanup()

	// Test scenario: User manually changes FwdPort on router from 3001 to 3003
	// This reproduces the specific issue reported by the user

	// Create a service that wants DstPort=8080, FwdPort=3001
	services := []*corev1.Service{
		createTestServiceWithLB("default", "web-service2", map[string]string{
			"unifi-port-forward.fiskhe.st/ports": "8080:3001",
		}, "192.168.1.100"),
	}

	// Router has rule with different FwdPort (manual change from 3001 to 3003)
	routerRules := []*unifi.PortForward{
		{
			Name:    "default/web-service2:3001", // Same name (belongs to our service)
			DstPort: "8080",                      // Same external port
			FwdPort: "3003",                      // Different internal port (manual change!)
			Fwd:     "192.168.1.100",             // Same destination IP
			Proto:   "tcp",                       // Same protocol
			Enabled: true,                        // Same enabled state
		},
	}

	// Create drift detector
	detector := &DriftDetector{
		Router: nil,
	}

	// Test analysis
	analyses, err := detector.AnalyzeAllServicesDrift(context.Background(), services, routerRules)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Should detect FwdPort change as missing + extra rules (can't update FwdPort in UniFi API)
	analysis := analyses[0]
	if !analysis.HasDrift {
		t.Error("Expected drift due to FwdPort change")
	}

	// Should have missing rule for desired config and extra rule for old config
	if len(analysis.MissingRules) != 1 {
		t.Errorf("Expected 1 missing rule, got %d", len(analysis.MissingRules))
	}

	if len(analysis.ExtraRules) != 1 {
		t.Errorf("Expected 1 extra rule, got %d", len(analysis.ExtraRules))
	}

	// Check missing rule (desired config not in router)
	missingRule := analysis.MissingRules[0]
	if missingRule.DstPort != 8080 {
		t.Errorf("Expected missing rule DstPort 8080, got %d", missingRule.DstPort)
	}
	if missingRule.FwdPort != 3001 {
		t.Errorf("Expected missing rule FwdPort 3001, got %d", missingRule.FwdPort)
	}

	// Check extra rule (router has old config)
	extraRule := analysis.ExtraRules[0]
	if extraRule.DstPort != "8080" {
		t.Errorf("Expected extra rule DstPort '8080', got '%s'", extraRule.DstPort)
	}
	if extraRule.FwdPort != "3003" {
		t.Errorf("Expected extra rule FwdPort '3003', got '%s'", extraRule.FwdPort)
	}
}

func TestDriftDetector_MixedScenarios(t *testing.T) {
	// Test multiple drift scenarios
	tests := []struct {
		name          string
		services      []*corev1.Service
		routerRules   []*unifi.PortForward
		expectedDrift map[string]bool
	}{
		{
			name: "missing rule scenario",
			services: []*corev1.Service{
				createTestServiceWithLB("default", "test1", map[string]string{
					"unifi-port-forward.fiskhe.st/ports": "8080:http",
				}, "192.168.1.100"),
			},
			routerRules: []*unifi.PortForward{
				// No rules on router
			},
			expectedDrift: map[string]bool{"default/test1": true},
		},
		{
			name: "extra rule scenario - duplicate port mapping",
			services: []*corev1.Service{
				createTestServiceWithLB("default", "test1", map[string]string{
					"unifi-port-forward.fiskhe.st/ports": "8080:http",
				}, "192.168.1.100"),
			},
			routerRules: []*unifi.PortForward{
				{
					Name:    "default/test1:http",
					DstPort: "8080",
					FwdPort: "80",
					Fwd:     "192.168.1.100",
					Proto:   "tcp",
					Enabled: true,
				},
				{
					Name:    "default/test1:extra",
					DstPort: "9090",
					FwdPort: "9090",
					Fwd:     "192.168.1.100",
					Proto:   "tcp",
					Enabled: true,
				},
			},
			expectedDrift: map[string]bool{"default/test1": true},
		},
		{
			name: "no router rules scenario",
			services: []*corev1.Service{
				createTestServiceWithLB("default", "test1", map[string]string{
					"unifi-port-forward.fiskhe.st/ports": "8080:http",
				}, "192.168.1.100"),
			},
			routerRules: []*unifi.PortForward{
				// No rules on router
			},
			expectedDrift: map[string]bool{"default/test1": true},
		},
		{
			name: "extra rule scenario - identical duplicate",
			services: []*corev1.Service{
				createTestServiceWithLB("default", "test1", map[string]string{
					"unifi-port-forward.fiskhe.st/ports": "8080:http",
				}, "192.168.1.100"),
			},
			routerRules: []*unifi.PortForward{
				{
					Name:    "default/test1:http",
					DstPort: "8080",
					FwdPort: "80",
					Fwd:     "192.168.1.100",
					Proto:   "tcp",
					Enabled: true,
				},
				{
					Name:    "default/test1:extra",
					DstPort: "9090",
					FwdPort: "9090",
					Fwd:     "192.168.1.100",
					Proto:   "tcp",
					Enabled: true,
				},
			},
			expectedDrift: map[string]bool{"default/test1": true},
		},
		{
			name: "correct port mapping scenario",
			services: []*corev1.Service{
				createTestServiceWithLB("default", "test1", map[string]string{
					"unifi-port-forward.fiskhe.st/ports": "8080:80",
				}, "192.168.1.100"),
			},
			routerRules: []*unifi.PortForward{
				{
					Name:    "default/test1:80",
					DstPort: "80",
					FwdPort: "8080",
					Fwd:     "192.168.1.100",
					Proto:   "tcp",
					Enabled: true,
				},
				{
					Name:    "default/test1:extra",
					DstPort: "9090",
					FwdPort: "9090",
					Fwd:     "192.168.1.100",
					Proto:   "tcp",
					Enabled: true,
				},
			},
			expectedDrift: map[string]bool{"default/test1": true},
		},
		{
			name: "no drift scenario with 8080:80 mapping",
			services: []*corev1.Service{
				createTestServiceWithLB("default", "test1", map[string]string{
					"unifi-port-forward.fiskhe.st/ports": "8080:80",
				}, "192.168.1.100"),
			},
			routerRules: []*unifi.PortForward{
				{
					Name:    "default/test1:80",
					DstPort: "8080",
					FwdPort: "80",
					Fwd:     "192.168.1.100",
					Proto:   "tcp",
					Enabled: true,
				},
			},
			expectedDrift: map[string]bool{"default/test1": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := NewControllerTestEnv(t)
			defer env.Cleanup()

			// Create drift detector
			detector := &DriftDetector{
				Router: nil,
			}

			// Test analysis
			analyses, err := detector.AnalyzeAllServicesDrift(context.Background(), tt.services, tt.routerRules)
			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}

			// Verify drift expectations
			for serviceName, expectedDrift := range tt.expectedDrift {
				var serviceAnalysis *DriftAnalysis
				for _, analysis := range analyses {
					if analysis.ServiceName == serviceName {
						serviceAnalysis = analysis
						break
					}
				}

				if serviceAnalysis == nil {
					t.Errorf("Expected analysis for service %s", serviceName)
					continue
				}

				if serviceAnalysis.HasDrift != expectedDrift {
					t.Errorf("Expected drift %v for service %s, got %v", expectedDrift, serviceName, serviceAnalysis.HasDrift)
				}
			}
		})
	}
}

// Helper function to create test service with LoadBalancer IP
func createTestServiceWithLB(namespace, name string, annotations map[string]string, lbIP string) *corev1.Service {
	// Create a simple service port matching the annotation
	var servicePorts []corev1.ServicePort

	// Based on annotations, create appropriate service ports
	if portAnn, exists := annotations["unifi-port-forward.fiskhe.st/ports"]; exists {
		// Parse port mappings to determine what service ports to create
		mappings := strings.Split(portAnn, ",")
		for _, mapping := range mappings {
			mapping = strings.TrimSpace(mapping)
			if mapping == "" {
				continue
			}

			parts := strings.Split(mapping, ":")
			var portName string

			// New format: externalPort:serviceName or just serviceName
			if len(parts) == 1 {
				// Single port name: "http" -> use service port as external port
				portName = parts[0]
			} else if len(parts) == 2 {
				// External port mapping: "8080:http" -> port name is "http"
				portName = parts[1]
			} else {
				continue
			}

			switch portName {
			case "http":
				// Create port named "http" with port 8080
				servicePorts = append(servicePorts, corev1.ServicePort{
					Name:     "http",
					Port:     8080,
					Protocol: corev1.ProtocolTCP,
				})
			case "https":
				// Create port named "https" with port 443
				servicePorts = append(servicePorts, corev1.ServicePort{
					Name:     "https",
					Port:     443,
					Protocol: corev1.ProtocolTCP,
				})
			case "80":
				// Create port named "80" with port 8080
				servicePorts = append(servicePorts, corev1.ServicePort{
					Name:     "80",
					Port:     8080,
					Protocol: corev1.ProtocolTCP,
				})
			case "3306":
				// Create port named "3306" with port 3306
				servicePorts = append(servicePorts, corev1.ServicePort{
					Name:     "3306",
					Port:     3306,
					Protocol: corev1.ProtocolTCP,
				})
			default:
				// For any other port name, use name as port number if it's numeric
				if portNum, err := strconv.Atoi(portName); err == nil {
					servicePorts = append(servicePorts, corev1.ServicePort{
						Name:     portName,
						Port:     int32(portNum),
						Protocol: corev1.ProtocolTCP,
					})
				}
			}
		}
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Ports: servicePorts,
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{
						IP: lbIP,
					},
				},
			},
		},
	}
}
