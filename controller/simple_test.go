package controller

import (
	"testing"

	"github.com/filipowm/go-unifi/unifi"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"kube-router-port-forward/routers"
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

func TestCalculateDelta_CreationScenario(t *testing.T) {
	// Test delta calculation for new port creation
	controller := &PortForwardReconciler{}

	desiredConfigs := []routers.PortConfig{
		{
			Name:      "default/test-service:http",
			DstPort:   8080,
			FwdPort:   80,
			DstIP:     "192.168.1.100",
			Protocol:  "tcp",
			Enabled:   true,
			Interface: "wan",
			SrcIP:     "any",
		},
	}

	changeContext := &ChangeContext{
		ServiceKey:       "default/test-service",
		ServiceNamespace: "default",
		ServiceName:      "test-service",
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
	}

	operations := controller.calculateDelta([]*unifi.PortForward{}, desiredConfigs, changeContext, service)

	if len(operations) != 1 {
		t.Errorf("Expected 1 operation, got %d", len(operations))
	}

	if operations[0].Type != OpCreate {
		t.Errorf("Expected CREATE operation, got %s", operations[0].Type)
	}

	if operations[0].Reason != "port_not_yet_exists" {
		t.Errorf("Expected 'port_not_yet_exists' reason, got %s", operations[0].Reason)
	}
}

func TestCalculateDelta_UpdateScenario(t *testing.T) {
	// Test delta calculation for existing rule update
	controller := &PortForwardReconciler{}

	existingRules := []*unifi.PortForward{
		{
			ID:      "abc123",
			Name:    "default/test-service:http",
			DstPort: "8080",
			FwdPort: "80",
			Fwd:     "192.168.1.100", // Old IP
			Proto:   "tcp",
			Enabled: true,
		},
	}

	desiredConfigs := []routers.PortConfig{
		{
			Name:      "default/test-service:http",
			DstPort:   8080,
			FwdPort:   80,
			DstIP:     "192.168.1.101", // New IP
			Protocol:  "tcp",
			Enabled:   true,
			Interface: "wan",
			SrcIP:     "any",
		},
	}

	changeContext := &ChangeContext{
		IPChanged:        true,
		ServiceKey:       "default/test-service",
		ServiceNamespace: "default",
		ServiceName:      "test-service",
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
	}

	operations := controller.calculateDelta(existingRules, desiredConfigs, changeContext, service)

	if len(operations) != 1 {
		t.Errorf("Expected 1 operation, got %d", len(operations))
	}

	if operations[0].Type != OpUpdate {
		t.Errorf("Expected UPDATE operation, got %s", operations[0].Type)
	}

	if operations[0].Reason != "configuration_mismatch" {
		t.Errorf("Expected 'configuration_mismatch' reason, got %s", operations[0].Reason)
	}
}

func TestCalculateDelta_DeletionScenario(t *testing.T) {
	// Test delta calculation for port deletion
	controller := &PortForwardReconciler{}

	existingRules := []*unifi.PortForward{
		{
			ID:      "abc123",
			Name:    "default/test-service:http",
			DstPort: "8080",
			FwdPort: "80",
			Fwd:     "192.168.1.100",
			Proto:   "tcp",
			Enabled: true,
		},
	}

	var desiredConfigs []routers.PortConfig

	changeContext := &ChangeContext{
		AnnotationChanged: true,
		ServiceKey:        "default/test-service",
		ServiceNamespace:  "default",
		ServiceName:       "test-service",
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
	}

	operations := controller.calculateDelta(existingRules, desiredConfigs, changeContext, service)

	if len(operations) != 1 {
		t.Errorf("Expected 1 operation, got %d", len(operations))
	}

	if operations[0].Type != OpDelete {
		t.Errorf("Expected DELETE operation, got %s", operations[0].Type)
	}

	if operations[0].Reason != "port_no_longer_desired" {
		t.Errorf("Expected 'port_no_longer_desired' reason, got %s", operations[0].Reason)
	}
}
