package controller

import (
	"testing"

	"unifi-port-forward/pkg/config"
	"unifi-port-forward/pkg/helpers"
	"unifi-port-forward/pkg/routers"

	"github.com/filipowm/go-unifi/unifi"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCalculateDelta_CreationScenario(t *testing.T) {
	// Test delta calculation for new port creation
	controller := &PortForwardReconciler{Config: &config.Config{Debug: false}}

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
	controller := &PortForwardReconciler{Config: &config.Config{Debug: false}}

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

	if operations[0].Reason != "configuration_mismatch_safe" {
		t.Errorf("Expected 'configuration_mismatch_safe' reason, got %s", operations[0].Reason)
	}
}

func TestCalculateDelta_DeletionScenario(t *testing.T) {
	// Test delta calculation for rule deletion
	controller := &PortForwardReconciler{Config: &config.Config{Debug: false}}

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

	// No desired configs means deletion
	var desiredConfigs []routers.PortConfig

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

func TestDetectPortConflicts_NoConflicts(t *testing.T) {
	controller := &PortForwardReconciler{Config: &config.Config{Debug: false}}

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

	// No conflicting existing rules
	currentRules := []*unifi.PortForward{}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
	}

	operations := controller.detectPortConflicts(currentRules, desiredConfigs, service)

	// Should detect no conflicts
	if len(operations) != 0 {
		t.Errorf("Expected 0 conflict operations when no existing rules, got %d", len(operations))
	}
}

func TestCountOwnershipTakeovers(t *testing.T) {
	operations := []PortOperation{
		{Type: OpCreate, Reason: "port_configuration_changed"},
		{Type: OpUpdate, Reason: "ownership_takeover"},
		{Type: OpUpdate, Reason: "configuration_mismatch"},
		{Type: OpUpdate, Reason: "ownership_takeover"},
		{Type: OpDelete, Reason: "port_no_longer_desired"},
	}

	count := countOwnershipTakeovers(operations)

	if count != 2 {
		t.Errorf("Expected 2 ownership takeovers, got %d", count)
	}
}

func TestPortConflicts_CreationScenarios(t *testing.T) {
	testCases := []struct {
		name           string
		desiredConfig  routers.PortConfig
		existingRule   *unifi.PortForward
		expectConflict bool
	}{
		{
			name: "conflict_with_manual_rule",
			desiredConfig: routers.PortConfig{
				Name:      "qbittorrent/qbittorrent-bittorrent:tcp",
				DstPort:   6881,
				FwdPort:   6881,
				DstIP:     "192.168.72.3",
				Protocol:  "tcp",
				Enabled:   true,
				Interface: "wan",
				SrcIP:     "any",
			},
			existingRule: &unifi.PortForward{
				ID:            "rule6881",
				Name:          "qbittorrent",
				DstPort:       "6881",
				FwdPort:       "6881",
				Fwd:           "192.168.1.50",
				Proto:         "tcp",
				Enabled:       true,
				PfwdInterface: "wan",
				Src:           "any",
			},
			expectConflict: true,
		},
		{
			name: "no_conflict_different_ports",
			desiredConfig: routers.PortConfig{
				Name:      "default/service:tcp",
				DstPort:   8080,
				FwdPort:   80,
				DstIP:     "192.168.1.100",
				Protocol:  "tcp",
				Enabled:   true,
				Interface: "wan",
				SrcIP:     "any",
			},
			existingRule: &unifi.PortForward{
				Name:          "manual-rule",
				DstPort:       "9090",
				FwdPort:       "8080",
				Fwd:           "192.168.1.50",
				Proto:         "tcp",
				Enabled:       true,
				PfwdInterface: "wan",
				Src:           "any",
			},
			expectConflict: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			helpers.ClearPortConflictTracking()

			controller := &PortForwardReconciler{Config: &config.Config{Debug: false}}

			var currentRules []*unifi.PortForward
			if tc.existingRule != nil {
				currentRules = []*unifi.PortForward{tc.existingRule}
			}

			desiredConfigs := []routers.PortConfig{tc.desiredConfig}

			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
			}

			operations := controller.detectPortConflicts(currentRules, desiredConfigs, service)

			if tc.expectConflict {
				if len(operations) != 1 {
					t.Errorf("Expected 1 conflict operation, got %d", len(operations))
					return
				}
				if operations[0].Type != OpUpdate {
					t.Errorf("Expected UPDATE operation for conflict, got %s", operations[0].Type)
				}
				if operations[0].Reason != "ownership_takeover" {
					t.Errorf("Expected 'ownership_takeover' reason, got %s", operations[0].Reason)
				}
			} else {
				if len(operations) != 0 {
					t.Errorf("Expected no conflict operations, got %d", len(operations))
				}
			}
		})
	}
}

func TestPortConflicts_AlreadyOwned(t *testing.T) {
	helpers.ClearPortConflictTracking()

	controller := &PortForwardReconciler{Config: &config.Config{Debug: false}}

	desiredConfigs := []routers.PortConfig{
		{
			Name:      "default/web-service:tcp",
			DstPort:   8080,
			FwdPort:   80,
			DstIP:     "192.168.1.100",
			Protocol:  "tcp",
			Enabled:   true,
			Interface: "wan",
			SrcIP:     "any",
		},
	}

	// Existing rule already owned by this service (correct naming pattern)
	currentRules := []*unifi.PortForward{
		{
			Name:          "default/web-service:tcp",
			DstPort:       "8080",
			FwdPort:       "80",
			Fwd:           "192.168.1.50",
			Proto:         "tcp",
			Enabled:       true,
			PfwdInterface: "wan",
			Src:           "any",
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-service",
			Namespace: "default",
		},
	}

	operations := controller.detectPortConflicts(currentRules, desiredConfigs, service)

	// Should not detect conflicts with already-owned rules
	if len(operations) != 0 {
		t.Errorf("Expected 0 conflict operations for already-owned rule, got %d", len(operations))
	}
}

func TestPortConflicts_MultipleConflicts(t *testing.T) {
	helpers.ClearPortConflictTracking()

	controller := &PortForwardReconciler{Config: &config.Config{Debug: false}}

	desiredConfigs := []routers.PortConfig{
		{
			Name:      "test/service:http",
			DstPort:   8080,
			FwdPort:   80,
			DstIP:     "192.168.1.100",
			Protocol:  "tcp",
			Enabled:   true,
			Interface: "wan",
			SrcIP:     "any",
		},
		{
			Name:      "test/service:https",
			DstPort:   8443,
			FwdPort:   443,
			DstIP:     "192.168.1.100",
			Protocol:  "tcp",
			Enabled:   true,
			Interface: "wan",
			SrcIP:     "any",
		},
	}

	// Multiple existing rules with conflicts
	currentRules := []*unifi.PortForward{
		{
			ID:            "rule8080",
			Name:          "manual-http",
			DstPort:       "8080",
			FwdPort:       "80",
			Fwd:           "192.168.1.50",
			Proto:         "tcp",
			Enabled:       true,
			PfwdInterface: "wan",
			Src:           "any",
		},
		{
			ID:            "rule8443",
			Name:          "manual-https",
			DstPort:       "8443",
			FwdPort:       "443",
			Fwd:           "192.168.1.51",
			Proto:         "tcp",
			Enabled:       true,
			PfwdInterface: "wan",
			Src:           "any",
		},
		// Non-conflicting rule (different internal port)
		{
			ID:            "rule8080other",
			Name:          "other-rule",
			DstPort:       "8080",
			FwdPort:       "8081",
			Fwd:           "192.168.1.52",
			Proto:         "tcp",
			Enabled:       true,
			PfwdInterface: "wan",
			Src:           "any",
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service",
			Namespace: "test",
		},
	}

	operations := controller.detectPortConflicts(currentRules, desiredConfigs, service)

	// Should detect exactly 2 conflicts (http and https)
	if len(operations) != 2 {
		t.Errorf("Expected 2 conflict operations, got %d", len(operations))
	}

	// Verify both conflicts are detected
	conflictPorts := make(map[int]bool)
	for _, op := range operations {
		if op.Type != OpUpdate {
			t.Errorf("Expected UPDATE operation for conflict, got %s", op.Type)
		}
		if op.Reason != "ownership_takeover" {
			t.Errorf("Expected 'ownership_takeover' reason, got %s", op.Reason)
		}
		conflictPorts[op.Config.DstPort] = true
	}

	if !conflictPorts[8080] {
		t.Error("Expected conflict for port 8080 not detected")
	}

	if !conflictPorts[8443] {
		t.Error("Expected conflict for port 8443 not detected")
	}

	// Should not conflict with 8080 rule that has different internal port
	if conflictPorts[8080] && len(operations) == 3 {
		t.Error("Unexpected conflict detected for port 8080 with different internal port")
	}
}

func TestPortConflicts_ProtocolMismatch(t *testing.T) {
	helpers.ClearPortConflictTracking()

	controller := &PortForwardReconciler{Config: &config.Config{Debug: false}}

	desiredConfigs := []routers.PortConfig{
		{
			Name:      "test/service:tcp",
			DstPort:   8080,
			FwdPort:   80,
			DstIP:     "192.168.1.100",
			Protocol:  "tcp",
			Enabled:   true,
			Interface: "wan",
			SrcIP:     "any",
		},
	}

	// Existing rule with same ports but different protocol
	currentRules := []*unifi.PortForward{
		{
			Name:          "different-rule",
			DstPort:       "8080",
			FwdPort:       "80",
			Fwd:           "192.168.1.50",
			Proto:         "udp", // Different protocol
			Enabled:       true,
			PfwdInterface: "wan",
			Src:           "any",
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service",
			Namespace: "test",
		},
	}

	operations := controller.detectPortConflicts(currentRules, desiredConfigs, service)

	// Should detect NO conflicts (protocols differ)
	if len(operations) != 0 {
		t.Errorf("Expected 0 conflict operations when protocols differ, got %d", len(operations))
	}
}

func TestPortConflicts_ExternalPortOnly(t *testing.T) {
	helpers.ClearPortConflictTracking()

	controller := &PortForwardReconciler{Config: &config.Config{Debug: false}}

	desiredConfigs := []routers.PortConfig{
		{
			Name:      "test/service:tcp",
			DstPort:   8080,
			FwdPort:   80,
			DstIP:     "192.168.1.100",
			Protocol:  "tcp",
			Enabled:   true,
			Interface: "wan",
			SrcIP:     "any",
		},
	}

	// Existing rule with same external port but different internal port
	currentRules := []*unifi.PortForward{
		{
			Name:          "different-rule",
			DstPort:       "8080", // Same external port
			FwdPort:       "8080", // Different internal port (80 vs 8080)
			Fwd:           "192.168.1.50",
			Proto:         "tcp",
			Enabled:       true,
			PfwdInterface: "wan",
			Src:           "any",
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service",
			Namespace: "test",
		},
	}

	operations := controller.detectPortConflicts(currentRules, desiredConfigs, service)

	// Should detect NO conflicts (internal ports differ)
	if len(operations) != 0 {
		t.Errorf("Expected 0 conflict operations when only external port matches, got %d", len(operations))
	}
}

func TestPortConflicts_InternalPortOnly(t *testing.T) {
	helpers.ClearPortConflictTracking()

	controller := &PortForwardReconciler{Config: &config.Config{Debug: false}}

	desiredConfigs := []routers.PortConfig{
		{
			Name:      "test/service:tcp",
			DstPort:   8080,
			FwdPort:   80,
			DstIP:     "192.168.1.100",
			Protocol:  "tcp",
			Enabled:   true,
			Interface: "wan",
			SrcIP:     "any",
		},
	}

	// Existing rule with same internal port but different external port
	currentRules := []*unifi.PortForward{
		{
			Name:          "different-rule",
			DstPort:       "9090", // Different external port
			FwdPort:       "80",   // Same internal port
			Fwd:           "192.168.1.50",
			Proto:         "tcp",
			Enabled:       true,
			PfwdInterface: "wan",
			Src:           "any",
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service",
			Namespace: "test",
		},
	}

	operations := controller.detectPortConflicts(currentRules, desiredConfigs, service)

	// Should detect NO conflicts (external ports differ)
	if len(operations) != 0 {
		t.Errorf("Expected 0 conflict operations when only internal port matches, got %d", len(operations))
	}
}
