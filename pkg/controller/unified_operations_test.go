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
	// Test delta calculation for port deletion
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

func TestDetectPortConflicts(t *testing.T) {
	controller := &PortForwardReconciler{Config: &config.Config{Debug: false}}

	// Create desired configs that should conflict with existing manual rules
	desiredConfigs := []routers.PortConfig{
		{
			Name:      "qbittorrent/qbittorrent-bittorrent:tcp",
			DstPort:   6881,
			FwdPort:   6881,
			DstIP:     "192.168.72.3",
			Protocol:  "tcp",
			Enabled:   true,
			Interface: "wan",
			SrcIP:     "any",
		},
	}

	// Create existing manual rules that conflict
	currentRules := []*unifi.PortForward{
		{
			ID:            "rule6881",    // Add required ID for validation
			Name:          "qbittorrent", // Manual rule name
			DstPort:       "6881",
			FwdPort:       "6881",
			Fwd:           "192.168.1.50", // Different IP
			Proto:         "tcp",
			Enabled:       true,
			PfwdInterface: "wan",
			Src:           "any",
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "qbittorrent-bittorrent",
			Namespace: "qbittorrent",
		},
	}

	operations := controller.detectPortConflicts(currentRules, desiredConfigs, service)

	// Should detect exactly one conflict
	if len(operations) != 1 {
		t.Errorf("Expected 1 conflict operation, got %d", len(operations))
	}

	if operations[0].Type != OpUpdate {
		t.Errorf("Expected UPDATE operation for conflict, got %s", operations[0].Type)
	}

	if operations[0].Reason != "ownership_takeover" {
		t.Errorf("Expected 'ownership_takeover' reason, got %s", operations[0].Reason)
	}

	// Verify the old rule details
	if operations[0].ExistingRule.Name != "qbittorrent" {
		t.Errorf("Expected old rule name 'qbittorrent', got %s", operations[0].ExistingRule.Name)
	}

	// Verify the new config details
	if operations[0].Config.DstPort != 6881 {
		t.Errorf("Expected new config port 6881, got %d", operations[0].Config.DstPort)
	}

	if operations[0].Config.Name != "qbittorrent/qbittorrent-bittorrent:tcp" {
		t.Errorf("Expected new config name 'qbittorrent/qbittorrent-bittorrent:tcp', got %s", operations[0].Config.Name)
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
		t.Errorf("Expected 0 conflict operations, got %d", len(operations))
	}
}

func TestDetectPortConflicts_AlreadyOwned(t *testing.T) {
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

	// Existing rule already owned by this service (correct naming pattern)
	currentRules := []*unifi.PortForward{
		{
			Name:          "default/test-service:http", // Already follows controller naming
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
			Name:      "test-service",
			Namespace: "default",
		},
	}

	operations := controller.detectPortConflicts(currentRules, desiredConfigs, service)

	// Should not detect conflicts with already-owned rules
	if len(operations) != 0 {
		t.Errorf("Expected 0 conflict operations for already-owned rule, got %d", len(operations))
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

func TestDetectPortConflicts_TrueConflict(t *testing.T) {
	controller := &PortForwardReconciler{Config: &config.Config{Debug: false}}

	desiredConfigs := []routers.PortConfig{
		{
			Name:      "qbittorrent/qbittorrent-bittorrent:tcp",
			DstPort:   6881,
			FwdPort:   6881,
			DstIP:     "192.168.72.3",
			Protocol:  "tcp",
			Enabled:   true,
			Interface: "wan",
			SrcIP:     "any",
		},
	}

	// Existing rule with identical port configuration (external+internal+protocol)
	currentRules := []*unifi.PortForward{
		{
			ID:            "rule6881", // Add required ID for validation
			Name:          "qbittorrent",
			DstPort:       "6881",
			FwdPort:       "6881",
			Fwd:           "192.168.1.50",
			Proto:         "tcp",
			Enabled:       true,
			PfwdInterface: "wan",
			Src:           "any",
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "qbittorrent-bittorrent",
			Namespace: "qbittorrent",
		},
	}

	operations := controller.detectPortConflicts(currentRules, desiredConfigs, service)

	// Should detect exactly one conflict (both ports + protocol match)
	if len(operations) != 1 {
		t.Errorf("Expected 1 conflict operation, got %d", len(operations))
	}

	if operations[0].Type != OpUpdate {
		t.Errorf("Expected UPDATE operation for conflict, got %s", operations[0].Type)
	}

	if operations[0].Reason != "ownership_takeover" {
		t.Errorf("Expected 'ownership_takeover' reason, got %s", operations[0].Reason)
	}

	// Verify the old rule details
	if operations[0].ExistingRule.Name != "qbittorrent" {
		t.Errorf("Expected old rule name 'qbittorrent', got %s", operations[0].ExistingRule.Name)
	}

	// Verify the new config details
	if operations[0].Config.DstPort != 6881 {
		t.Errorf("Expected new config DstPort 6881, got %d", operations[0].Config.DstPort)
	}

	if operations[0].Config.FwdPort != 6881 {
		t.Errorf("Expected new config FwdPort 6881, got %d", operations[0].Config.FwdPort)
	}
}

func TestDetectPortConflicts_ExternalPortOnly(t *testing.T) {
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

func TestDetectPortConflicts_InternalPortOnly(t *testing.T) {
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

func TestDetectPortConflicts_ProtocolMismatch(t *testing.T) {
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
			DstPort:       "8080", // Same external port
			FwdPort:       "80",   // Same internal port
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

func TestDetectPortConflicts_MultipleConflicts(t *testing.T) {
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
		conflictPorts[op.Config.DstPort] = true
	}

	if !conflictPorts[8080] {
		t.Error("Expected conflict for port 8080 not detected")
	}

	if !conflictPorts[8443] {
		t.Error("Expected conflict for port 8443 not detected")
	}

	if conflictPorts[9090] {
		t.Error("Unexpected conflict detected for port 9090 (should not conflict)")
	}
}

// TestConflictDetectionOwnershipTakeoverBug tests the specific bug where
// deleting "91:https" from annotation "89:http,91:https" incorrectly generates
// an UPDATE operation for port 89 that fails with "not found"
func TestConflictDetectionOwnershipTakeoverBug(t *testing.T) {
	// Clear port tracking first to avoid conflicts
	helpers.ClearPortConflictTracking()

	// Setup - create service with annotation "89:http,91:https"
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-service",
			Namespace: "test-namespace",
			Annotations: map[string]string{
				"unifi-port-forward.fiskhe.st/mapping": "89:http,91:https",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "web"},
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Port:     8080,
					Protocol: corev1.ProtocolTCP,
				},
				{
					Name:     "https",
					Port:     8181,
					Protocol: corev1.ProtocolTCP,
				},
			},
		},
	}

	// Parse port configurations from annotation
	configs, err := helpers.GetPortConfigs(service, "192.168.1.100", "unifi-port-forward.fiskhe.st/mapping")
	if err != nil {
		t.Fatalf("Failed to get port configs: %v", err)
	}
	if len(configs) != 2 {
		t.Fatalf("Expected 2 configs, got %d", len(configs))
	}

	// Verify initial configs
	if configs[0].DstPort != 89 {
		t.Errorf("Expected DstPort 89, got %d", configs[0].DstPort)
	}
	if configs[0].FwdPort != 8080 {
		t.Errorf("Expected FwdPort 8080, got %d", configs[0].FwdPort)
	}
	if configs[1].DstPort != 91 {
		t.Errorf("Expected DstPort 91, got %d", configs[1].DstPort)
	}
	if configs[1].FwdPort != 8181 {
		t.Errorf("Expected FwdPort 8181, got %d", configs[1].FwdPort)
	}

	// Simulate existing router state:
	// - Port 89 rule exists but with different forward port (owned by another service)
	// - Port 91 rule exists (owned by this service)
	currentRules := []*unifi.PortForward{
		{
			ID:      "rule89",
			Name:    "other-service:http",
			DstPort: "89",
			FwdPort: "9090", // Different forward port - this is the key issue
			Fwd:     "192.168.1.100",
			Proto:   "tcp",
			Enabled: true,
		},
		{
			ID:      "rule91",
			Name:    "test-namespace/web-service:https",
			DstPort: "91",
			FwdPort: "8181",
			Fwd:     "192.168.1.100",
			Proto:   "tcp",
			Enabled: true,
		},
	}

	// Create reconciler
	reconciler := &PortForwardReconciler{}
	changeContext := &ChangeContext{IPChanged: false}

	// Test the scenario: user deletes 91:https, keeping only 89:http
	// This should generate DELETE for port 91 and no UPDATE for port 89
	updatedConfigs := []routers.PortConfig{configs[0]} // Only keep port 89 config

	operations := reconciler.calculateDelta(currentRules, updatedConfigs, changeContext, service)

	// Verify operations - should only have DELETE for port 91, no UPDATE for port 89
	if len(operations) != 2 {
		t.Errorf("Expected exactly 2 operations, got %d", len(operations))
	}

	// Find DELETE operation
	var deleteOp *PortOperation
	var updateOp *PortOperation

	for i := range operations {
		switch operations[i].Type {
		case OpDelete:
			deleteOp = &operations[i]
		case OpUpdate:
			updateOp = &operations[i]
		}
	}

	// Should have DELETE for port 91
	if deleteOp == nil {
		t.Fatal("Should have DELETE operation")
		return
	}

	if deleteOp.Config.DstPort != 91 {
		t.Errorf("DELETE should be for port 91, got %d", deleteOp.Config.DstPort)
	}
	if deleteOp.Reason != "port_no_longer_desired" {
		t.Errorf("Expected reason 'port_no_longer_desired', got %s", deleteOp.Reason)
	}

	// Should NOT have UPDATE for port 89 (this was the bug)
	if updateOp != nil {
		// If there's an UPDATE, it should be a validated conflict operation
		// but in this bug scenario, there should be no UPDATE at all
		t.Logf("Found UPDATE operation (this might indicate the bug still exists): %+v", updateOp)
		t.Errorf("UPDATE operation should not exist for port 89 in this scenario")
	}
}

// TestValidateConflictOperations tests the new validation function
func TestValidateConflictOperations(t *testing.T) {
	reconciler := &PortForwardReconciler{}

	// Create test operations including an invalid conflict operation
	operations := []PortOperation{
		{
			Type: OpUpdate,
			Config: routers.PortConfig{
				Name:     "test/service:port",
				DstPort:  89,
				FwdPort:  8080,
				Protocol: "tcp",
			},
			ExistingRule: &unifi.PortForward{
				ID:      "missing-rule", // This ID doesn't exist in currentRules
				DstPort: "89",
				FwdPort: "8080",
				Proto:   "tcp",
			},
			Reason: "ownership_takeover",
		},
		{
			Type: OpDelete,
			Config: routers.PortConfig{
				Name:     "test/service:port2",
				DstPort:  91,
				FwdPort:  8181,
				Protocol: "tcp",
			},
			Reason: "port_no_longer_desired",
		},
	}

	// Current rules - the missing rule ID is not present
	currentRules := []*unifi.PortForward{
		{
			ID:      "different-rule",
			DstPort: "80",
			FwdPort: "8080",
			Proto:   "tcp",
		},
	}

	// Validate operations
	validated := reconciler.validateConflictOperations(operations, currentRules)

	// Should only have the DELETE operation (UPDATE should be filtered out)
	if len(validated) != 1 {
		t.Errorf("Expected 1 validated operation, got %d", len(validated))
	}
	if validated[0].Type != OpDelete {
		t.Errorf("Expected OpDelete, got %s", validated[0].Type)
	}
	if validated[0].Config.DstPort != 91 {
		t.Errorf("Expected DstPort 91, got %d", validated[0].Config.DstPort)
	}
}
