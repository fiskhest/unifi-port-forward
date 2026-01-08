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
			"unifi-port-forwarder/ports": "http:8080",
		}, "192.168.1.100"),
	}

	// Create matching router rules
	routerRules := []*unifi.PortForward{
		{
			Name:    "default/perfect-service:http",
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
			"unifi-port-forwarder/ports": "http:8080",
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
					"unifi-port-forwarder/ports": "http:8080",
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
					"unifi-port-forwarder/ports": "http:8080",
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
					"unifi-port-forwarder/ports": "http:8080",
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
					"unifi-port-forwarder/ports": "http:8080",
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
					"unifi-port-forwarder/ports": "80:8080",
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
			name: "no drift scenario with 80:8080 mapping",
			services: []*corev1.Service{
				createTestServiceWithLB("default", "test1", map[string]string{
					"unifi-port-forwarder/ports": "80:8080",
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
			expectedDrift: map[string]bool{"default/test1": false},
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
	if portAnn, exists := annotations["unifi-port-forwarder/ports"]; exists {
		// Parse port mappings to determine what service ports to create
		mappings := strings.Split(portAnn, ",")
		for _, mapping := range mappings {
			mapping = strings.TrimSpace(mapping)
			if mapping == "" {
				continue
			}

			parts := strings.Split(mapping, ":")
			var portName string

			if len(parts) >= 1 {
				portName = parts[0]

				switch portName {
				case "80":
					// Create port named "80" for mapping "80:8080"
					servicePorts = append(servicePorts, corev1.ServicePort{
						Name:     "80",
						Port:     80,
						Protocol: corev1.ProtocolTCP,
					})
				case "443":
					// Create port named "443" for mapping "443:8443"
					servicePorts = append(servicePorts, corev1.ServicePort{
						Name:     "443",
						Port:     443,
						Protocol: corev1.ProtocolTCP,
					})
				case "3306":
					// Create port named "3306" for mapping "3306:3306"
					servicePorts = append(servicePorts, corev1.ServicePort{
						Name:     "3306",
						Port:     3306,
						Protocol: corev1.ProtocolTCP,
					})
				case "http":
					// Create port named "http" with port 80
					servicePorts = append(servicePorts, corev1.ServicePort{
						Name:     "http",
						Port:     80,
						Protocol: corev1.ProtocolTCP,
					})
				case "https":
					// Create port named "https" with port 443
					servicePorts = append(servicePorts, corev1.ServicePort{
						Name:     "https",
						Port:     443,
						Protocol: corev1.ProtocolTCP,
					})
				default:
					// For any other port name, use the name as port number if it's numeric
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
