package utils

import (
	"testing"

	"unifi-port-forward/pkg/ports"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const testAnnotation = "unifi-port-forward.fiskhe.st/mapping"

func serviceWithPorts(annotation string, servicePorts ...v1.ServicePort) *v1.Service {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-service",
			Namespace:   "default",
			Annotations: map[string]string{testAnnotation: annotation},
		},
		Spec: v1.ServiceSpec{
			Type:  v1.ServiceTypeLoadBalancer,
			Ports: servicePorts,
		},
	}
}

func tcpPort(name string, port int32) v1.ServicePort {
	return v1.ServicePort{Name: name, Port: port, Protocol: v1.ProtocolTCP}
}

func TestParsePortMappingAnnotation(t *testing.T) {
	tests := []struct {
		name       string
		annotation string
		want       map[string]string // port name -> external spec ("" when defaulted from the service)
		wantErr    bool
	}{
		{
			name:       "single port mapping",
			annotation: "8888:http",
			want:       map[string]string{"http": "8888"},
		},
		{
			name:       "bare port name defaults to the service port",
			annotation: "http",
			want:       map[string]string{"http": ""},
		},
		{
			name:       "range mapping",
			annotation: "8000-8100:game",
			want:       map[string]string{"game": "8000-8100"},
		},
		{
			name:       "several mappings",
			annotation: "80:http,443:https,8000-8100:game",
			want:       map[string]string{"http": "80", "https": "443", "game": "8000-8100"},
		},
		{
			name:       "repeated port name coalesces into a list",
			annotation: "80:https,443:https",
			want:       map[string]string{"https": "80,443"},
		},
		{
			name:       "repeated port name coalesces range and port",
			annotation: "8000-8100:game,9000:game",
			want:       map[string]string{"game": "8000-8100,9000"},
		},

		{name: "invalid port", annotation: "notaport:http", wantErr: true},
		{name: "port out of range", annotation: "70000:http", wantErr: true},
		{name: "inverted range", annotation: "8100-8000:http", wantErr: true},
		{name: "too many colons", annotation: "80:http:extra", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mappings, err := parsePortMappingAnnotation(tt.annotation)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parsePortMappingAnnotation(%q) = %v, want error", tt.annotation, mappings)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePortMappingAnnotation(%q) returned error: %v", tt.annotation, err)
			}

			if len(mappings) != len(tt.want) {
				t.Fatalf("got %d mappings, want %d: %+v", len(mappings), len(tt.want), mappings)
			}
			for _, mapping := range mappings {
				want, ok := tt.want[mapping.PortName]
				if !ok {
					t.Errorf("unexpected mapping for port name %q", mapping.PortName)
					continue
				}
				if got := mapping.ExternalPorts.String(); got != want {
					t.Errorf("port %q external spec = %q, want %q", mapping.PortName, got, want)
				}
			}
		})
	}
}

func TestGetPortConfigsPortSpecs(t *testing.T) {
	tests := []struct {
		name        string
		annotation  string
		servicePort v1.ServicePort
		wantDst     string
		wantFwd     string
	}{
		{
			name:        "single external port forwards to the service port",
			annotation:  "8888:http",
			servicePort: tcpPort("http", 1234),
			wantDst:     "8888",
			wantFwd:     "1234",
		},
		{
			name:        "bare name uses the service port on both ends",
			annotation:  "http",
			servicePort: tcpPort("http", 3000),
			wantDst:     "3000",
			wantFwd:     "3000",
		},
		{
			name:        "range forwards to a same-sized range at the service port",
			annotation:  "8000-8100:game",
			servicePort: tcpPort("game", 8000),
			wantDst:     "8000-8100",
			wantFwd:     "8000-8100",
		},
		{
			name:        "range is offset onto a different service port",
			annotation:  "9000-9002:game",
			servicePort: tcpPort("game", 30000),
			wantDst:     "9000-9002",
			wantFwd:     "30000-30002",
		},
		{
			name:        "list collapses onto the single service port",
			annotation:  "80:https,443:https",
			servicePort: tcpPort("https", 8443),
			wantDst:     "80,443",
			wantFwd:     "8443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ClearPortConflictTracking()

			service := serviceWithPorts(tt.annotation, tt.servicePort)
			configs, err := GetPortConfigs(service, "192.168.1.50", testAnnotation)
			if err != nil {
				t.Fatalf("GetPortConfigs returned error: %v", err)
			}
			if len(configs) != 1 {
				t.Fatalf("got %d configs, want 1: %+v", len(configs), configs)
			}

			if got := configs[0].DstPort.String(); got != tt.wantDst {
				t.Errorf("DstPort = %q, want %q", got, tt.wantDst)
			}
			if got := configs[0].FwdPort.String(); got != tt.wantFwd {
				t.Errorf("FwdPort = %q, want %q", got, tt.wantFwd)
			}
		})
	}
}

func nodePortService(annotation string, servicePorts ...v1.ServicePort) *v1.Service {
	service := serviceWithPorts(annotation, servicePorts...)
	service.Spec.Type = v1.ServiceTypeNodePort
	return service
}

func TestGetPortConfigsNodePortForwardsToNodePort(t *testing.T) {
	tests := []struct {
		name        string
		annotation  string
		servicePort v1.ServicePort
		wantDst     string
		wantFwd     string
	}{
		{
			name:        "single external port forwards to the nodePort",
			annotation:  "8080:http",
			servicePort: v1.ServicePort{Name: "http", Port: 80, NodePort: 31234, Protocol: v1.ProtocolTCP},
			wantDst:     "8080",
			wantFwd:     "31234",
		},
		{
			name:        "bare name uses the service port externally, nodePort internally",
			annotation:  "http",
			servicePort: v1.ServicePort{Name: "http", Port: 80, NodePort: 31234, Protocol: v1.ProtocolTCP},
			wantDst:     "80",
			wantFwd:     "31234",
		},
		{
			// nodePorts are allocated one at a time and are not contiguous, so a
			// range must not be offset onto nodePort..nodePort+N.
			name:        "range collapses onto the single nodePort",
			annotation:  "27015-27020:game",
			servicePort: v1.ServicePort{Name: "game", Port: 27015, NodePort: 31234, Protocol: v1.ProtocolUDP},
			wantDst:     "27015-27020",
			wantFwd:     "31234",
		},
		{
			name:        "list collapses onto the single nodePort",
			annotation:  "80:web,443:web",
			servicePort: v1.ServicePort{Name: "web", Port: 8080, NodePort: 31500, Protocol: v1.ProtocolTCP},
			wantDst:     "80,443",
			wantFwd:     "31500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ClearPortConflictTracking()

			service := nodePortService(tt.annotation, tt.servicePort)
			configs, err := GetPortConfigs(service, "192.168.1.21", testAnnotation)
			if err != nil {
				t.Fatalf("GetPortConfigs returned error: %v", err)
			}
			if len(configs) != 1 {
				t.Fatalf("got %d configs, want 1: %+v", len(configs), configs)
			}

			if got := configs[0].DstPort.String(); got != tt.wantDst {
				t.Errorf("DstPort = %q, want %q", got, tt.wantDst)
			}
			if got := configs[0].FwdPort.String(); got != tt.wantFwd {
				t.Errorf("FwdPort = %q, want %q", got, tt.wantFwd)
			}
		})
	}
}

func TestGetPortConfigsNodePortWithoutAllocatedNodePort(t *testing.T) {
	ClearPortConflictTracking()

	service := nodePortService("8080:http", v1.ServicePort{Name: "http", Port: 80, Protocol: v1.ProtocolTCP})
	if _, err := GetPortConfigs(service, "192.168.1.21", testAnnotation); err == nil {
		t.Error("expected an error when the nodePort has not been allocated yet")
	}
}

func TestGetPortConfigsRangeRunningPastMaxPort(t *testing.T) {
	ClearPortConflictTracking()

	// 10 external ports starting at service port 65530 would need port 65539.
	service := serviceWithPorts("8000-8009:game", tcpPort("game", 65530))
	if _, err := GetPortConfigs(service, "192.168.1.50", testAnnotation); err == nil {
		t.Error("expected an error when the forward range runs past the maximum port")
	}
}

func TestGetPortConfigsRejectsOverlappingExternalPorts(t *testing.T) {
	ClearPortConflictTracking()

	service := serviceWithPorts(
		"8000-8100:game,8050:admin",
		tcpPort("game", 8000),
		tcpPort("admin", 9000),
	)

	if _, err := GetPortConfigs(service, "192.168.1.50", testAnnotation); err == nil {
		t.Error("expected an error when two mappings claim overlapping external ports")
	}
}

func TestGetPortConfigsMarksEveryPortInRange(t *testing.T) {
	ClearPortConflictTracking()

	service := serviceWithPorts("8000-8100:game", tcpPort("game", 8000))
	if _, err := GetPortConfigs(service, "192.168.1.50", testAnnotation); err != nil {
		t.Fatalf("GetPortConfigs returned error: %v", err)
	}

	// A different service asking for a port inside the range must be rejected.
	if err := CheckPortConflict(ports.FromPort(8050), "other/service"); err == nil {
		t.Error("expected a conflict for a port inside an already claimed range")
	}
	// A port outside the range is still free.
	if err := CheckPortConflict(ports.FromPort(8101), "other/service"); err != nil {
		t.Errorf("expected no conflict outside the claimed range, got: %v", err)
	}
	// The claiming service itself sees no conflict.
	if err := CheckPortConflict(ports.FromPort(8050), "default/test-service"); err != nil {
		t.Errorf("expected no conflict for the owning service, got: %v", err)
	}
}
