package controller

import (
	// "strings"
	"testing"
	// "time"
	"github.com/filipowm/go-unifi/unifi"

	"unifi-port-forward/pkg/routers"

	corev1 "k8s.io/api/core/v1"
	// metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestChangeDetection_OtherChanges(t *testing.T) {
	// Test other change detection logic (IP event publishing removed)
	changeContext := &ChangeContext{
		AnnotationChanged: true,
		OldAnnotation:     "8080:http",
		NewAnnotation:     "80:http",
		ServiceKey:        "default/test-service",
		ServiceNamespace:  "default",
		ServiceName:       "test-service",
	}

	if !changeContext.HasRelevantChanges() {
		t.Error("Expected annotation change to be detected")
	}

	if !changeContext.AnnotationChanged {
		t.Error("Expected AnnotationChanged to be true")
	}

	if changeContext.SpecChanged {
		t.Error("Expected only annotation change")
	}
}

func TestChangeDetection_AnnotationChange(t *testing.T) {
	// Test annotation change detection logic
	changeContext := &ChangeContext{
		AnnotationChanged: true,
		OldAnnotation:     "8080:http",
		NewAnnotation:     "8080:http,8443:https",
		ServiceKey:        "default/test-service",
		ServiceNamespace:  "default",
		ServiceName:       "test-service",
	}

	if !changeContext.HasRelevantChanges() {
		t.Error("Expected annotation change to be detected")
	}

	if !changeContext.AnnotationChanged {
		t.Error("Expected AnnotationChanged to be true")
	}

	if changeContext.SpecChanged {
		t.Error("Expected only annotation change")
	}
}

func TestChangeDetection_SpecChange(t *testing.T) {
	// Test spec change detection logic
	changeContext := &ChangeContext{
		SpecChanged: true,
		PortChanges: []PortChangeDetail{
			{
				ChangeType: "added",
				NewPort:    &corev1.ServicePort{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP},
			},
		},
		ServiceKey:       "default/test-service",
		ServiceNamespace: "default",
		ServiceName:      "test-service",
	}

	if !changeContext.HasRelevantChanges() {
		t.Error("Expected spec change to be detected")
	}

	if !changeContext.SpecChanged {
		t.Error("Expected SpecChanged to be true")
	}

	if changeContext.AnnotationChanged {
		t.Error("Expected only spec change")
	}
}

func TestChangeAnalysis_PortChanges(t *testing.T) {
	// Test port change analysis
	oldPorts := []corev1.ServicePort{
		{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
		{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP},
	}

	newPorts := []corev1.ServicePort{
		{Name: "http", Port: 8080, Protocol: corev1.ProtocolTCP}, // Changed port
		{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP},
	}

	changes := analyzePortChanges(oldPorts, newPorts)

	if len(changes) != 1 {
		t.Errorf("Expected 1 change, got %d", len(changes))
	}

	// Check that http port change was detected
	change := changes[0]
	if change.ChangeType != "modified" {
		t.Errorf("Expected modified change, got %s", change.ChangeType)
	}

	if change.OldPort.Port != 80 || change.NewPort.Port != 8080 {
		t.Errorf("Expected port change from 80 to 8080, got %d to %d",
			change.OldPort.Port, change.NewPort.Port)
	}
}

// TODO: Potential cleanup, delete?
// func TestChangeContextSerializationFormat(t *testing.T) {
// 	// Test that new format excludes redundant fields and is properly formatted
// 	context := &ChangeContext{
// 		AnnotationChanged: false,
// 		OldAnnotation:     "80:http",
// 		NewAnnotation:     "http:81",
// 		SpecChanged:       true,
// 		ServiceKey:        "test-namespace/test-service",
// 		ServiceNamespace:  "test-namespace",
// 		ServiceName:       "test-service",
// 	}

// 	serialized, err := serializeChangeContext(context)
// 	if err != nil {
// 		t.Fatalf("Failed to serialize: %v", err)
// 	}

// 	// Verify it contains expected fields
// 	if !strings.Contains(serialized, `"spec_changed": true`) {
// 		t.Error("Missing spec_changed field")
// 	}
// 	if !strings.Contains(serialized, `"service_key": "test-namespace/test-service"`) {
// 		t.Error("Missing service_key field")
// 	}

// 	// Verify it excludes redundant fields
// 	if strings.Contains(serialized, "service_namespace") {
// 		t.Error("Should not contain service_namespace field")
// 	}
// 	if strings.Contains(serialized, "service_name") {
// 		t.Error("Should not contain service_name field")
// 	}

// 	// Verify it's properly formatted (contains newlines and indentation)
// 	if !strings.Contains(serialized, "\n") {
// 		t.Error("Should be multi-line formatted")
// 	}
// 	if !strings.Contains(serialized, "  ") {
// 		t.Error("Should contain indentation")
// 	}

// 	t.Logf("Serialization format output:\n%s", serialized)
// }

// func TestAnalyzeChanges_DeletionDetection(t *testing.T) {
// 	tests := []struct {
// 		name              string
// 		oldService        *corev1.Service
// 		newService        *corev1.Service
// 		expectDeletion    bool
// 		expectEarlyReturn bool
// 	}{
// 		{
// 			name: "Service marked for deletion",
// 			oldService: &corev1.Service{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:      "test-service",
// 					Namespace: "default",
// 					Annotations: map[string]string{
// 						"unifi-port-forward.fiskhe.st/mapping": "8080:http",
// 					},
// 				},
// 				Spec: corev1.ServiceSpec{
// 					Ports: []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
// 				},
// 			},
// 			newService: &corev1.Service{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:              "test-service",
// 					Namespace:         "default",
// 					Annotations:       map[string]string{"unifi-port-forward.fiskhe.st/mapping": "8080:http"},
// 					DeletionTimestamp: &metav1.Time{Time: time.Now()},
// 				},
// 				Spec: corev1.ServiceSpec{
// 					Ports: []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
// 				},
// 			},
// 			expectDeletion:    true,
// 			expectEarlyReturn: true,
// 		},
// 		{
// 			name: "Service already deleted - no change",
// 			oldService: &corev1.Service{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:              "test-service",
// 					Namespace:         "default",
// 					DeletionTimestamp: &metav1.Time{Time: time.Now().Add(-time.Minute)},
// 				},
// 			},
// 			newService: &corev1.Service{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:              "test-service",
// 					Namespace:         "default",
// 					DeletionTimestamp: &metav1.Time{Time: time.Now()},
// 				},
// 			},
// 			expectDeletion:    false,
// 			expectEarlyReturn: false,
// 		},
// 		{
// 			name: "Service not deleted - no change",
// 			oldService: &corev1.Service{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:      "test-service",
// 					Namespace: "default",
// 				},
// 			},
// 			newService: &corev1.Service{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:      "test-service",
// 					Namespace: "default",
// 				},
// 			},
// 			expectDeletion:    false,
// 			expectEarlyReturn: false,
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			context := analyzeChanges(tt.oldService, tt.newService)

// 			if context.DeletionChanged != tt.expectDeletion {
// 				t.Errorf("Expected DeletionChanged=%v, got %v", tt.expectDeletion, context.DeletionChanged)
// 			}

// 			if tt.expectDeletion && !context.HasRelevantChanges() {
// 				t.Error("Expected HasRelevantChanges()=true when deletion detected")
// 			}

// 			if !tt.expectDeletion && context.DeletionChanged {
// 				t.Error("Expected DeletionChanged=false when no deletion detected")
// 			}

// 			// Verify early return behavior - when deletion is detected, other changes should not be analyzed
// 			if tt.expectEarlyReturn {
// 				if context.IPChanged || context.AnnotationChanged || context.SpecChanged {
// 					t.Error("Expected early return - no other changes should be analyzed when deletion detected")
// 				}
// 			}
// 		})
// 	}
// }

func TestHasRelevantChanges_WithDeletionChanged(t *testing.T) {
	tests := []struct {
		name           string
		changeContext  *ChangeContext
		expectedResult bool
	}{
		{
			name: "DeletionChanged only",
			changeContext: &ChangeContext{
				ServiceKey:      "default/test-service",
				DeletionChanged: true,
			},
			expectedResult: true,
		},
		{
			name: "DeletionChanged with other changes",
			changeContext: &ChangeContext{
				ServiceKey:        "default/test-service",
				DeletionChanged:   true,
				IPChanged:         true,
				AnnotationChanged: true,
			},
			expectedResult: true,
		},
		{
			name: "No changes",
			changeContext: &ChangeContext{
				ServiceKey: "default/test-service",
			},
			expectedResult: false,
		},
		{
			name: "Other changes without deletion",
			changeContext: &ChangeContext{
				ServiceKey:        "default/test-service",
				IPChanged:         true,
				AnnotationChanged: true,
			},
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.changeContext.HasRelevantChanges()
			if result != tt.expectedResult {
				t.Errorf("Expected HasRelevantChanges()=%v, got %v", tt.expectedResult, result)
			}
		})
	}
}

func TestCollectRulesForService(t *testing.T) {
	// Test collecting rules from port configurations
	configs := []routers.PortConfig{
		{
			Name:     "default/webapp:http",
			DstPort:  8080,
			FwdPort:  80,
			Protocol: "tcp",
		},
		{
			Name:     "default/webapp:https",
			DstPort:  8443,
			FwdPort:  443,
			Protocol: "tcp",
		},
		{
			Name:     "default/webapp:db",
			DstPort:  3306,
			FwdPort:  3306,
			Protocol: "tcp",
		},
	}

	rules := collectRulesForService(configs)

	if len(rules) != 3 {
		t.Errorf("Expected 3 rules, got %d", len(rules))
	}

	expectedRules := []string{
		"default/webapp:http",
		"default/webapp:https",
		"default/webapp:db",
	}

	for i, expected := range expectedRules {
		if i >= len(rules) || rules[i] != expected {
			t.Errorf("Expected rule %d to be '%s', got '%s'", i, expected, rules[i])
		}
	}
}

func TestCompareIPsWithRouterState_RealIPChange(t *testing.T) {
	// Test case: Real IP change - router has different IP than desired
	desiredIP := "192.168.1.100"
	currentRules := []*unifi.PortForward{
		{
			DstPort: "80",
			Fwd:     "192.168.1.50", // Different IP
			Name:    "default/test-service:http",
		},
	}

	ipChanged, oldIP, newIP := compareIPsWithRouterState(desiredIP, currentRules)

	if !ipChanged {
		t.Errorf("Expected IP changed to be true, got false")
	}
	if oldIP != "192.168.1.50" {
		t.Errorf("Expected old IP to be '192.168.1.50', got '%s'", oldIP)
	}
	if newIP != "192.168.1.100" {
		t.Errorf("Expected new IP to be '192.168.1.100', got '%s'", newIP)
	}
}

func TestCompareIPsWithRouterState_NoIPChange(t *testing.T) {
	// Test case: No IP change - router already has correct IP
	desiredIP := "192.168.1.100"
	currentRules := []*unifi.PortForward{
		{
			DstPort: "80",
			Fwd:     "192.168.1.100", // Same IP
			Name:    "default/test-service:http",
		},
	}

	ipChanged, oldIP, newIP := compareIPsWithRouterState(desiredIP, currentRules)

	if ipChanged {
		t.Errorf("Expected IP changed to be false, got true")
	}
	if oldIP != "" {
		t.Errorf("Expected old IP to be empty, got '%s'", oldIP)
	}
	if newIP != "" {
		t.Errorf("Expected new IP to be empty, got '%s'", newIP)
	}
}

func TestCompareIPsWithRouterState_ServiceStatusEmpty(t *testing.T) {
	// Test case: Service status empty but router has correct IP
	// This was the original bug scenario
	desiredIP := "" // Empty service IP (simulating service status issue)
	currentRules := []*unifi.PortForward{
		{
			DstPort: "89",
			Fwd:     "192.168.72.6", // Router has correct IP
			Name:    "unifi-port-forward/web-service:http",
		},
	}

	ipChanged, oldIP, newIP := compareIPsWithRouterState(desiredIP, currentRules)

	if ipChanged {
		t.Errorf("Expected IP changed to be false when desired IP is empty, got true")
	}
	if oldIP != "" {
		t.Errorf("Expected old IP to be empty when desired IP is empty, got '%s'", oldIP)
	}
	if newIP != "" {
		t.Errorf("Expected new IP to be empty when desired IP is empty, got '%s'", newIP)
	}
}

func TestCompareIPsWithRouterState_NoCurrentRules(t *testing.T) {
	// Test case: No current rules (new service)
	desiredIP := "192.168.1.100"
	var currentRules []*unifi.PortForward // No rules

	ipChanged, oldIP, newIP := compareIPsWithRouterState(desiredIP, currentRules)

	if ipChanged {
		t.Errorf("Expected IP changed to be false for new service, got true")
	}
	if oldIP != "" {
		t.Errorf("Expected old IP to be empty for new service, got '%s'", oldIP)
	}
	if newIP != "" {
		t.Errorf("Expected new IP to be empty for new service, got '%s'", newIP)
	}
}

func TestCompareIPsWithRouterState_MultipleRulesSomeMatch(t *testing.T) {
	// Test case: Multiple rules, some match, some don't
	desiredIP := "192.168.1.100"
	currentRules := []*unifi.PortForward{
		{
			DstPort: "80",
			Fwd:     "192.168.1.100", // Match
			Name:    "default/test-service:http",
		},
		{
			DstPort: "443",
			Fwd:     "192.168.1.50", // Different IP - should trigger change
			Name:    "default/test-service:https",
		},
	}

	ipChanged, oldIP, newIP := compareIPsWithRouterState(desiredIP, currentRules)

	if !ipChanged {
		t.Errorf("Expected IP changed to be true when any rule has different IP, got false")
	}
	if oldIP != "192.168.1.50" {
		t.Errorf("Expected old IP to be '192.168.1.50' (first mismatched rule), got '%s'", oldIP)
	}
	if newIP != "192.168.1.100" {
		t.Errorf("Expected new IP to be '192.168.1.100', got '%s'", newIP)
	}
}
