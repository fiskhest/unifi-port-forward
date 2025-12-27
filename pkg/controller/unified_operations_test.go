package controller

import (
	"testing"

	"kube-router-port-forward/pkg/routers"

	"github.com/filipowm/go-unifi/unifi"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
