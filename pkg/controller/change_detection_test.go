package controller

import (
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"unifi-port-forwarder/pkg/routers"
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

func TestChangeContextWithPortForwardRules(t *testing.T) {
	// Test that port forward rules are included in change context serialization
	context := &ChangeContext{
		IPChanged:         false,
		OldIP:             "192.168.1.100",
		NewIP:             "192.168.1.101",
		AnnotationChanged: false,
		OldAnnotation:     "80:http",
		NewAnnotation:     "http:81",
		SpecChanged:       true,
		ServiceKey:        "test-namespace/test-service",
		ServiceNamespace:  "test-namespace",
		ServiceName:       "test-service",
		PortForwardRules: []string{
			"test-namespace/test-service:http",
			"test-namespace/test-service:https",
			"test-namespace/test-service:db",
		},
	}

	serialized, err := serializeChangeContext(context)
	if err != nil {
		t.Fatalf("Failed to serialize: %v", err)
	}

	// Verify port forward rules are included
	if !strings.Contains(serialized, `"port_forward_rules"`) {
		t.Error("Missing port_forward_rules field")
	}

	if !strings.Contains(serialized, `"test-namespace/test-service:http"`) {
		t.Error("Missing http rule in port_forward_rules")
	}

	if !strings.Contains(serialized, `"test-namespace/test-service:https"`) {
		t.Error("Missing https rule in port_forward_rules")
	}

	if !strings.Contains(serialized, `"test-namespace/test-service:db"`) {
		t.Error("Missing db rule in port_forward_rules")
	}

	// Verify still excludes redundant fields
	if strings.Contains(serialized, "service_namespace") {
		t.Error("Should not contain service_namespace field")
	}

	if strings.Contains(serialized, "service_name") {
		t.Error("Should not contain service_name field")
	}

	// Verify proper formatting (multi-line)
	if !strings.Contains(serialized, "\n") {
		t.Error("Should be multi-line formatted")
	}

	t.Logf("Change context with port forward rules:\n%s", serialized)
}

func TestErrorContextSerialization(t *testing.T) {
	// Test error context serialization
	errorContext := &ErrorContext{
		Timestamp:       "2023-12-28T10:30:00Z",
		LastFailureTime: "2023-12-28T10:25:00Z",
		FailedPortOperations: []FailedPortOperation{
			{
				PortMapping:  "default/webapp:db",
				ExternalPort: 3306,
				Protocol:     "tcp",
				ErrorType:    "conflict",
				ErrorMessage: "Port 3306 already in use by default/other-service:mysql",
				Timestamp:    "2023-12-28T10:25:00Z",
			},
		},
		OverallStatus:    "partial_failure",
		RetryCount:       2,
		LastErrorCode:    "PORT_CONFLICT",
		LastErrorMessage: "Port 3306 already in use by default/other-service:mysql",
	}

	// Serialize error context
	errorJSON, err := json.MarshalIndent(errorContext, "", "  ")
	if err != nil {
		t.Fatalf("Failed to serialize error context: %v", err)
	}

	errorJSONStr := string(errorJSON)

	// Verify key fields
	if !strings.Contains(errorJSONStr, `"timestamp"`) {
		t.Error("Missing timestamp field")
	}
	if !strings.Contains(errorJSONStr, `"last_failure_time"`) {
		t.Error("Missing last_failure_time field")
	}
	if !strings.Contains(errorJSONStr, `"failed_port_operations"`) {
		t.Error("Missing failed_port_operations field")
	}
	if !strings.Contains(errorJSONStr, `"overall_status"`) {
		t.Error("Missing overall_status field")
	}
	if !strings.Contains(errorJSONStr, `"retry_count"`) {
		t.Error("Missing retry_count field")
	}

	// Verify failed operation details
	if !strings.Contains(errorJSONStr, `"port_mapping"`) {
		t.Error("Missing port_mapping in failed operation")
	}
	if !strings.Contains(errorJSONStr, `"error_type"`) {
		t.Error("Missing error_type in failed operation")
	}
	if !strings.Contains(errorJSONStr, `"conflict"`) {
		t.Error("Missing conflict error type")
	}

	// Verify proper formatting
	if !strings.Contains(errorJSONStr, "\n") {
		t.Error("Should be multi-line formatted")
	}

	t.Logf("Error context output:\n%s", errorJSONStr)
}

func TestExtractErrorContext(t *testing.T) {
	// Test extracting error context from service
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "test-namespace",
			Annotations: map[string]string{
				"unifi-port-forwarder/error-context": `{
  "timestamp": "2023-12-28T10:30:00Z",
  "last_failure_time": "2023-12-28T10:25:00Z",
  "overall_status": "partial_failure",
  "retry_count": 2,
  "last_error_code": "PORT_CONFLICT",
  "last_error_message": "Port 3306 already in use"
}`,
			},
		},
	}

	// Extract error context
	errorContext, err := extractErrorContext(service)
	if err != nil {
		t.Fatalf("Failed to extract error context: %v", err)
	}

	if errorContext == nil {
		t.Fatal("Expected error context to be extracted")
	}

	// Verify extracted fields
	if errorContext.OverallStatus != "partial_failure" {
		t.Errorf("Expected overall_status 'partial_failure', got '%s'", errorContext.OverallStatus)
	}
	if errorContext.RetryCount != 2 {
		t.Errorf("Expected retry_count 2, got %d", errorContext.RetryCount)
	}
	if errorContext.LastErrorCode != "PORT_CONFLICT" {
		t.Errorf("Expected last_error_code 'PORT_CONFLICT', got '%s'", errorContext.LastErrorCode)
	}
}

func TestErrorContextLifecycle(t *testing.T) {
	// Test error context clearing on success
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "test-namespace",
			Annotations: map[string]string{
				"unifi-port-forwarder/error-context": `{"overall_status":"partial_failure"}`,
			},
		},
	}

	// Should extract existing error context
	errorContext, err := extractErrorContext(service)
	if err != nil {
		t.Fatalf("Failed to extract error context: %v", err)
	}

	if errorContext.OverallStatus != "partial_failure" {
		t.Errorf("Expected overall_status 'partial_failure', got '%s'", errorContext.OverallStatus)
	}

	// Clear error context annotation
	delete(service.Annotations, "unifi-port-forwarder/error-context")

	// Should return nil after clearing
	errorContext, err = extractErrorContext(service)
	if err != nil {
		t.Fatalf("Failed to extract error context after clearing: %v", err)
	}

	if errorContext != nil {
		t.Error("Expected nil error context after clearing annotation")
	}
}
