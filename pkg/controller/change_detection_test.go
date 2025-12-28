package controller

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestChangeDetection_IPChange(t *testing.T) {
	// Test IP change detection logic
	changeContext := &ChangeContext{
		IPChanged:        true,
		OldIP:            "192.168.1.100",
		NewIP:            "192.168.1.101",
		ServiceKey:       "default/test-service",
		ServiceNamespace: "default",
		ServiceName:      "test-service",
	}

	if !changeContext.HasRelevantChanges() {
		t.Error("Expected IP change to be detected")
	}

	if !changeContext.IPChanged {
		t.Error("Expected IPChanged to be true")
	}

	if changeContext.AnnotationChanged || changeContext.SpecChanged {
		t.Error("Expected only IP change")
	}
}

func TestChangeDetection_AnnotationChange(t *testing.T) {
	// Test annotation change detection logic
	changeContext := &ChangeContext{
		AnnotationChanged: true,
		OldAnnotation:     "http:8080",
		NewAnnotation:     "http:8080,https:8443",
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

	if changeContext.IPChanged || changeContext.SpecChanged {
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

	if changeContext.IPChanged || changeContext.AnnotationChanged {
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
		IPChanged:         true,
		OldIP:             "192.168.1.100",
		NewIP:             "192.168.1.101",
		AnnotationChanged: false,
		OldAnnotation:     "http:80",
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
	if !strings.Contains(serialized, `"ip_changed": true`) {
		t.Error("Missing ip_changed field")
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

	// Simulate a service with the annotation
	mockService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "test-namespace",
			Annotations: map[string]string{
				ChangeContextAnnotationKey: serialized,
			},
		},
	}

	extracted, err := extractChangeContext(mockService)
	if err != nil {
		t.Fatalf("Failed to extract from mock service: %v", err)
	}

	// Verify extracted context has all fields populated
	if extracted.ServiceKey != "test-namespace/test-service" {
		t.Errorf("Expected service_key 'test-namespace/test-service', got '%s'", extracted.ServiceKey)
	}
	if extracted.ServiceNamespace != "test-namespace" {
		t.Errorf("Expected service_namespace 'test-namespace', got '%s'", extracted.ServiceNamespace)
	}
	if extracted.ServiceName != "test-service" {
		t.Errorf("Expected service_name 'test-service', got '%s'", extracted.ServiceName)
	}

	t.Logf("New format output:\n%s", serialized)
}
