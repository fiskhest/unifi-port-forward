package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"unifi-port-forwarder/pkg/config"
	"unifi-port-forwarder/pkg/helpers"
	"unifi-port-forwarder/pkg/routers"

	"github.com/filipowm/go-unifi/unifi"
	corev1 "k8s.io/api/core/v1"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// strToInt converts string to int with fallback to 0
func strToInt(s string) int {
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}
	return 0
}

// determineMismatchType identifies the type of mismatch between existing and desired rules
func determineMismatchType(existingRule *unifi.PortForward, desiredConfig routers.PortConfig, changeContext *ChangeContext) string {
	// Check for risky changes first (delete-then-recreate)
	if existingRule.FwdPort != strconv.Itoa(desiredConfig.FwdPort) {
		return "fwdport"
	}
	if existingRule.DstPort != strconv.Itoa(desiredConfig.DstPort) {
		return "port"
	}
	if existingRule.Proto != desiredConfig.Protocol {
		return "protocol"
	}

	// Check for safe changes (direct update)
	// Detect IP change directly in addition to context flag
	if changeContext.IPChanged || existingRule.Fwd != desiredConfig.DstIP {
		return "ip"
	}
	if existingRule.Name != desiredConfig.Name {
		return "name"
	}
	if existingRule.Enabled != desiredConfig.Enabled {
		return "enabled"
	}

	return "" // No mismatch
}

// isSafeUpdate determines if a mismatch type can be safely updated in-place
func isSafeUpdate(mismatchType string) bool {
	safeTypes := map[string]bool{
		"name":      true,
		"ip":        true,
		"enabled":   true,
		"ownership": true,
	}
	return safeTypes[mismatchType]
}

/*
Port Key Format Documentation:

This file uses a consistent key format: "dstPort-fwdPort-protocol" (e.g., "8080-8081-tcp")

Key format ensures uniqueness by including all three components:
- dstPort: External port number
- fwdPort: Internal port number
- protocol: TCP/UDP

UniFi port forward rules are uniquely identified by the combination of
DstPort + FwdPort + Protocol. This is critical because:
- Multiple rules can have the same external port (dstPort)
- Multiple rules can have the same internal port (fwdPort)
- Only the full combination makes a rule unique

This allows accurate comparison between desired state and current router state,
and ensures proper detection of drift including FwdPort changes.
*/

// OperationType represents the type of port operation
type OperationType string

const (
	OpCreate OperationType = "create"
	OpUpdate OperationType = "update"
	OpDelete OperationType = "delete"
)

// PortOperation represents a single port management operation
type PortOperation struct {
	Type         OperationType
	Config       routers.PortConfig
	ExistingRule *unifi.PortForward // for updates/deletes
	Reason       string             // "ip_change", "annotation_add", "port_remove", etc.
}

// OperationResult represents the result of executing operations
type OperationResult struct {
	Created []routers.PortConfig
	Updated []routers.PortConfig
	Deleted []routers.PortConfig
	Failed  []error
}

// String returns a string representation of the operation
func (op PortOperation) String() string {
	switch op.Type {
	case OpCreate:
		return fmt.Sprintf("CREATE rule port %d → %s:%d (%s)", op.Config.DstPort, op.Config.DstIP, op.Config.FwdPort, op.Config.Protocol)
	case OpUpdate:
		return fmt.Sprintf("UPDATE rule port %d → %s:%d (%s)", op.Config.DstPort, op.Config.DstIP, op.Config.FwdPort, op.Config.Protocol)
	case OpDelete:
		return fmt.Sprintf("DELETE rule port %d (%s)", op.Config.DstPort, op.Config.Protocol)
	default:
		return fmt.Sprintf("UNKNOWN operation: %s", op.Type)
	}
}

// calculateDelta determines what operations are needed to reach desired state
//
// IMPORTANT: UniFi API UpdatePort limitations:
// - Can update: rule name, destination IP, enabled status
// - CANNOT update: external port (DstPort), internal port (FwdPort), protocol
// - Port changes require CREATE + DELETE operations
func (r *PortForwardReconciler) calculateDelta(currentRules []*unifi.PortForward, desiredConfigs []routers.PortConfig, changeContext *ChangeContext, service *corev1.Service) []PortOperation {
	var operations []PortOperation

	// Detect conflicts with existing manual rules first
	conflictOperations := r.detectPortConflicts(currentRules, desiredConfigs, service)

	// Validate conflict operations before adding them - remove any that might fail
	validConflictOperations := r.validateConflictOperations(conflictOperations, currentRules)
	operations = append(operations, validConflictOperations...)

	// Track ports that are already being handled by conflict operations
	// This prevents generating duplicate CREATE operations for ports that will be updated
	conflictPorts := make(map[string]bool)
	for _, op := range conflictOperations {
		portKey := fmt.Sprintf("%d-%d-%s", op.Config.DstPort, op.Config.FwdPort, op.Config.Protocol)
		conflictPorts[portKey] = true
	}

	// Build maps for efficient lookup
	// Create map of desired port configurations using dstPort-fwdPort-protocol as key
	// This key format ensures uniqueness for port forward rules and matches router state format
	desiredMap := make(map[string]routers.PortConfig)
	for _, config := range desiredConfigs {
		portKey := fmt.Sprintf("%d-%d-%s", config.DstPort, config.FwdPort, config.Protocol)
		desiredMap[portKey] = config
	}

	// Build map of current rules using same key format
	// If port keys don't match, it means port numbers changed = CREATE + DELETE
	// If port keys match but properties differ, it's UPDATE
	currentMap := make(map[string]*unifi.PortForward) // portKey -> existing rule
	for _, rule := range currentRules {
		if helpers.RuleBelongsToService(rule.Name, service.Namespace, service.Name) {
			// Use same key format as desiredMap for proper comparison
			// This ensures we can accurately compare desired vs current router state
			portKey := fmt.Sprintf("%s-%s-%s", rule.DstPort, rule.FwdPort, rule.Proto)
			currentMap[portKey] = rule
		}
	}

	// Find deletions (exist in current but not desired)
	for portKey, rule := range currentMap {
		if _, desired := desiredMap[portKey]; !desired {
			dstPort := strToInt(rule.DstPort)
			operations = append(operations, PortOperation{
				Type: OpDelete,
				Config: routers.PortConfig{
					Name:      rule.Name,
					DstPort:   dstPort,
					FwdPort:   strToInt(rule.FwdPort),
					DstIP:     rule.Fwd,
					Protocol:  rule.Proto,
					Enabled:   rule.Enabled,
					Interface: rule.PfwdInterface,
					SrcIP:     rule.Src,
				},
				ExistingRule: rule,
				Reason:       "port_no_longer_desired",
			})
		}
	}

	// Find creations and updates
	for portKey, desiredConfig := range desiredMap {
		// Skip ports that are already being handled by conflict operations
		// This prevents duplicate CREATE operations for ports that will be updated via conflict resolution
		if conflictPorts[portKey] {
			continue
		}

		if existingRule, exists := currentMap[portKey]; !exists {
			// Port configuration (dstPort/fwdPort/protocol) doesn't match any existing rule
			// This requires CREATE operation because UniFi UpdatePort API cannot change port numbers
			operations = append(operations, PortOperation{
				Type:   OpCreate,
				Config: desiredConfig,
				Reason: "port_not_yet_exists",
			})
		} else {
			// Use smart operation generation to handle mismatches safely
			// This detects risky changes (FwdPort, DstPort, Protocol) and uses delete-then-recreate
			mismatchType := determineMismatchType(existingRule, desiredConfig, changeContext)

			if mismatchType != "" {
				if isSafeUpdate(mismatchType) {
					// Safe change: direct update
					operations = append(operations, PortOperation{
						Type:         OpUpdate,
						Config:       desiredConfig,
						ExistingRule: existingRule,
						Reason:       "configuration_mismatch_safe",
					})
				} else {
					// Risky change: delete then recreate
					operations = append(operations, PortOperation{
						Type: OpDelete,
						Config: routers.PortConfig{
							// Copy from current rule for deletion
							Name:      existingRule.Name,
							DstPort:   helpers.ParseIntField(existingRule.DstPort),
							FwdPort:   helpers.ParseIntField(existingRule.FwdPort),
							DstIP:     existingRule.Fwd,
							Protocol:  existingRule.Proto,
							Enabled:   existingRule.Enabled,
							Interface: existingRule.PfwdInterface,
							SrcIP:     existingRule.Src,
						},
						ExistingRule: existingRule,
						Reason:       "configuration_mismatch_delete",
					})

					operations = append(operations, PortOperation{
						Type:   OpCreate,
						Config: desiredConfig,
						Reason: "configuration_mismatch_create",
					})
				}
			}
		}
	}

	return operations
}

// executeOperations executes port operations with proper error handling and rollback
func (r *PortForwardReconciler) executeOperations(ctx context.Context, operations []PortOperation) (*OperationResult, error) {
	logger := ctrllog.FromContext(ctx)
	result := &OperationResult{}
	var completedOperations []PortOperation

	if r.Config.Debug {
		ctrllog.FromContext(ctx).V(1).Info("Executing port operations",
			"total_operations", len(operations),
			"ownership_takeovers", countOwnershipTakeovers(operations))
	}

	for _, op := range operations {
		var err error

		switch op.Type {
		case OpCreate:
			err = r.Router.AddPort(ctx, op.Config)
			if err == nil {
				result.Created = append(result.Created, op.Config)
			}
		case OpUpdate:
			err = r.Router.UpdatePort(ctx, op.Config.DstPort, op.Config)
			if err == nil {
				result.Updated = append(result.Updated, op.Config)
			}
		case OpDelete:
			err = r.Router.RemovePort(ctx, op.Config)
			if err == nil {
				result.Deleted = append(result.Deleted, op.Config)
				// Clean up port tracking to free the port for reuse
				helpers.UnmarkPortUsed(op.Config.DstPort)
			}
		}

		if err != nil {
			result.Failed = append(result.Failed, err)

			// Attempt rollback of completed operations
			logger.Info("Operation failed, attempting rollback of completed operations",
				"operation", op.String(),
				"error", err)

			rollbackErr := r.rollbackOperations(ctx, completedOperations)
			if rollbackErr != nil {
				logger.Error(rollbackErr, "Rollback also failed",
					"completed_count", len(completedOperations))
				return result, fmt.Errorf("operation failed: %v, rollback also failed: %v", err, rollbackErr)
			}

			return result, fmt.Errorf("operation failed: %v", err)
		} else {
			completedOperations = append(completedOperations, op)
			logger.Info("Operation completed successfully",
				"operation", op.String())
		}
	}

	logger.V(1).Info("All operations completed successfully",
		"created_count", len(result.Created),
		"updated_count", len(result.Updated),
		"deleted_count", len(result.Deleted))

	return result, nil
}

// rollbackOperations attempts to rollback completed operations
func (r *PortForwardReconciler) rollbackOperations(ctx context.Context, operations []PortOperation) error {
	ctrllog.FromContext(ctx).Info("Rolling back completed operations",
		"operation_count", len(operations))

	// Rollback in reverse order
	for i := len(operations) - 1; i >= 0; i-- {
		op := operations[i]

		var err error
		switch op.Type {
		case OpCreate:
			// Created operation -> rollback by deleting
			err = r.Router.RemovePort(ctx, op.Config)
		case OpUpdate:
			// Updated operation -> rollback by updating back
			if op.ExistingRule != nil {
				rollbackConfig := routers.PortConfig{
					Name:      op.ExistingRule.Name,
					DstPort:   strToInt(op.ExistingRule.DstPort),
					FwdPort:   strToInt(op.ExistingRule.FwdPort),
					DstIP:     op.ExistingRule.Fwd,
					Protocol:  op.ExistingRule.Proto,
					Enabled:   op.ExistingRule.Enabled,
					Interface: op.ExistingRule.PfwdInterface,
					SrcIP:     op.ExistingRule.Src,
				}
				err = r.Router.UpdatePort(ctx, op.Config.DstPort, rollbackConfig)
				// If UpdatePort fails with "not found", convert to CREATE instead
				if err != nil && strings.Contains(err.Error(), "not found") {
					// Try to create the rule instead of updating
					createConfig := routers.PortConfig{
						Name:      op.Config.Name,
						DstPort:   op.Config.DstPort,
						FwdPort:   op.Config.FwdPort,
						DstIP:     op.Config.DstIP,
						Protocol:  op.Config.Protocol,
						Enabled:   op.Config.Enabled,
						Interface: "wan", // Use default interface
						SrcIP:     "any", // Use default source
					}
					err = r.Router.AddPort(ctx, createConfig)
				}
			}
		case OpDelete:
			// Deleted operation -> rollback by creating
			err = r.Router.AddPort(ctx, op.Config)
		}

		if err != nil {
			logger := ctrllog.FromContext(ctx)
			logger.Error(err, "Rollback operation failed",
				"operation", op.String())
		}
	}

	return nil
}

// executeCleanupOperations executes cleanup operations with proper error handling and rollback
func (r *PortForwardReconciler) executeCleanupOperations(ctx context.Context, operations []PortOperation, cleanupReason string) (*OperationResult, error) {
	logger := ctrllog.FromContext(ctx)
	result := &OperationResult{}
	var completedOperations []PortOperation

	if r.Config.Debug {
		ctrllog.FromContext(ctx).V(1).Info("Executing cleanup operations",
			"total_operations", len(operations),
			"cleanup_reason", cleanupReason)
	}

	// Validate all operations are DELETE operations
	for _, op := range operations {
		if op.Type != OpDelete {
			return result, fmt.Errorf("executeCleanupOperations only supports DELETE operations, got %s", op.Type)
		}
	}

	for _, op := range operations {
		var err error

		switch op.Type {
		case OpDelete:
			err = r.Router.RemovePort(ctx, op.Config)
			if err == nil {
				result.Deleted = append(result.Deleted, op.Config)
				// Clean up port tracking to free the port for reuse
				helpers.UnmarkPortUsed(op.Config.DstPort)
			}
		}

		if err != nil {
			result.Failed = append(result.Failed, err)

			// Attempt rollback of completed operations
			logger.Info("Cleanup operation failed, attempting rollback of completed operations",
				"operation", op.String(),
				"error", err)

			rollbackErr := r.rollbackCleanupOperations(ctx, completedOperations)
			if rollbackErr != nil {
				logger.Error(rollbackErr, "Cleanup rollback also failed",
					"completed_count", len(completedOperations))
				return result, fmt.Errorf("cleanup operation failed: %v, cleanup rollback also failed: %v", err, rollbackErr)
			}
			return result, fmt.Errorf("cleanup operation failed: %v", err)
		} else {
			completedOperations = append(completedOperations, op)
			logger.Info("Operation completed successfully", "operation", op)
		}
	}

	logger.V(1).Info("All cleanup operations completed successfully",
		"deleted_count", len(result.Deleted),
		"cleanup_reason", cleanupReason)

	return result, nil
}

// rollbackCleanupOperations attempts to rollback completed cleanup operations
func (r *PortForwardReconciler) rollbackCleanupOperations(ctx context.Context, operations []PortOperation) error {
	ctrllog.FromContext(ctx).Info("Rolling back completed cleanup operations",
		"operation_count", len(operations))

	// Rollback in reverse order
	for i := len(operations) - 1; i >= 0; i-- {
		op := operations[i]

		// Cleanup operations are DELETE operations, so rollback is CREATE
		err := r.Router.AddPort(ctx, op.Config)

		if err != nil {
			logger := ctrllog.FromContext(ctx)
			logger.Error(err, "Cleanup rollback operation failed",
				"operation", op.String())
		}
	}

	return nil
}

// calculateDesiredState generates the desired port configurations for a service
func (r *PortForwardReconciler) calculateDesiredState(service *corev1.Service) ([]routers.PortConfig, error) {
	lbIP := helpers.GetLBIP(service)
	if lbIP == "" {
		return nil, fmt.Errorf("service has no LoadBalancer IP")
	}

	// Get port configurations from annotations
	portConfigs, err := helpers.GetPortConfigs(service, lbIP, config.FilterAnnotation)
	if err != nil {
		return nil, fmt.Errorf("failed to get port configurations: %w", err)
	}

	return portConfigs, nil
}

// detectPortConflicts finds existing rules that conflict with desired ports but have different names
func (r *PortForwardReconciler) detectPortConflicts(currentRules []*unifi.PortForward, desiredConfigs []routers.PortConfig, service *corev1.Service) []PortOperation {
	var operations []PortOperation
	logger := ctrllog.FromContext(context.Background())

	// Build map of desired port configurations using dstPort-fwdPort-protocol as key
	// This ensures we only detect true conflicts where both external and internal ports match
	desiredMap := make(map[string]routers.PortConfig)
	for _, config := range desiredConfigs {
		portKey := fmt.Sprintf("%d-%d-%s", config.DstPort, config.FwdPort, config.Protocol)
		desiredMap[portKey] = config
	}

	// If no desired configs, no conflicts can exist (deletion-only scenario)
	if len(desiredConfigs) == 0 {
		return operations
	}

	// Check each current rule for conflicts
	for _, rule := range currentRules {
		dstPort := strToInt(rule.DstPort)
		fwdPort := strToInt(rule.FwdPort)

		// Skip if this rule is already owned by this service
		if helpers.RuleBelongsToService(rule.Name, service.Namespace, service.Name) {
			continue
		}

		// Check if this exact port configuration conflicts with our desired rules
		// Use same key format as calculateDelta for consistency: dstPort-fwdPort-protocol
		portKey := fmt.Sprintf("%d-%d-%s", dstPort, fwdPort, rule.Proto)
		if desiredConfig, conflict := desiredMap[portKey]; conflict {
			// Found a true conflict - both external and internal ports match
			// But only generate UPDATE if the existing rule actually exists and can be updated
			if rule.ID != "" {
				logger.Info("Port conflict detected - will take ownership",
					"existing_rule", rule.Name,
					"existing_dst_port", dstPort,
					"existing_fwd_port", fwdPort,
					"existing_protocol", rule.Proto,
					"new_rule_name", desiredConfig.Name,
					"service", service.Name,
					"namespace", service.Namespace)

				operations = append(operations, PortOperation{
					Type:         OpUpdate, // Update to take ownership
					Config:       desiredConfig,
					ExistingRule: rule,
					Reason:       "ownership_takeover",
				})
			} else {
				logger.Info("Skipping conflict resolution for rule without valid ID",
					"existing_rule", rule.Name,
					"dst_port", dstPort,
					"fwd_port", fwdPort,
					"protocol", rule.Proto)
			}
		}
	}

	if len(operations) > 0 {
		logger.Info("Detected port conflicts with existing manual rules",
			"conflict_count", len(operations),
			"service", service.Name,
			"namespace", service.Namespace)

		for _, op := range operations {
			logger.Info("Port conflict detected - will take ownership",
				"existing_rule", op.ExistingRule.Name,
				"existing_dst_port", op.ExistingRule.DstPort,
				"existing_fwd_port", op.ExistingRule.FwdPort,
				"existing_protocol", op.ExistingRule.Proto,
				"new_rule_name", op.Config.Name,
				"external_port", op.Config.DstPort,
				"fwd_port", op.Config.FwdPort,
				"protocol", op.Config.Protocol)
		}
	}

	return operations
}

// validateConflictOperations checks if conflict operations are viable before execution
func (r *PortForwardReconciler) validateConflictOperations(operations []PortOperation, currentRules []*unifi.PortForward) []PortOperation {
	var validOperations []PortOperation
	logger := ctrllog.FromContext(context.Background())

	for _, op := range operations {
		if op.Type == OpUpdate && op.Reason == "ownership_takeover" {
			// For ownership takeovers, verify the rule actually exists on the router
			ruleExists := false
			for _, rule := range currentRules {
				if rule.ID == op.ExistingRule.ID {
					ruleExists = true
					break
				}
			}

			if !ruleExists {
				logger.Info("Skipping invalid conflict operation - rule not found on router",
					"operation", op.String(),
					"rule_id", op.ExistingRule.ID,
					"dst_port", op.Config.DstPort,
					"protocol", op.Config.Protocol)
				continue
			}
		}
		validOperations = append(validOperations, op)
	}

	return validOperations
}

// countOwnershipTakeovers counts operations that are ownership takeovers
func countOwnershipTakeovers(operations []PortOperation) int {
	count := 0
	for _, op := range operations {
		if op.Reason == "ownership_takeover" {
			count++
		}
	}
	return count
}
