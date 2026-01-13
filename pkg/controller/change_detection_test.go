package controller

import (
	"strings"
	"testing"

	"unifi-port-forwarder/pkg/routers"

	corev1 "k8s.io/api/core/v1"
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

func TestChangeContextSerializationFormat(t *testing.T) {
	// Test that new format excludes redundant fields and is properly formatted
	context := &ChangeContext{
		AnnotationChanged: false,
		OldAnnotation:     "80:http",
		NewAnnotation:     "http:81",
		SpecChanged:       true,
		ServiceKey:        "test-namespace/test-service",
		ServiceNamespace:  "test-namespace",
		ServiceName:       "test-service",
	}

	serialized, err := serializeChangeContext(context)
	if err != nil {
		t.Fatalf("Failed to serialize: %v", err)
	}

	// Verify it contains expected fields
	if !strings.Contains(serialized, `"spec_changed": true`) {
		t.Error("Missing spec_changed field")
	}
	if !strings.Contains(serialized, `"service_key": "test-namespace/test-service"`) {
		t.Error("Missing service_key field")
	}

	// Verify it excludes redundant fields
	if strings.Contains(serialized, "service_namespace") {
		t.Error("Should not contain service_namespace field")
	}
	if strings.Contains(serialized, "service_name") {
		t.Error("Should not contain service_name field")
	}

	// Verify it's properly formatted (contains newlines and indentation)
	if !strings.Contains(serialized, "\n") {
		t.Error("Should be multi-line formatted")
	}
	if !strings.Contains(serialized, "  ") {
		t.Error("Should contain indentation")
	}

	t.Logf("Serialization format output:\n%s", serialized)
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
