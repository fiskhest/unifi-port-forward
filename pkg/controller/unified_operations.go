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

/*
Port Key Format Documentation:

Two different key formats are used in this file:

1. For desired configurations: "dstPort-fwdPort-protocol" (e.g., "8080-8081-tcp")
   - Maps desired service port configurations
   - Used to track what should exist in router

2. For current router state: "dstPort-fwdPort-protocol" (same format)
   - Maps existing UniFi port forward rules
   - Used to compare against desired configurations

Both formats ensure uniqueness by including all three components:
- dstPort: External port number
- fwdPort: Internal port number
- protocol: TCP/UDP

This allows accurate comparison between desired state and current router state.
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
	servicePrefix := fmt.Sprintf("%s/%s:", service.Namespace, service.Name)

	// Detect conflicts with existing manual rules first
	conflictOperations := r.detectPortConflicts(currentRules, desiredConfigs, service)
	operations = append(operations, conflictOperations...)

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
		if strings.HasPrefix(rule.Name, servicePrefix) {
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
					DstIP:     rule.DestinationIP,
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
			// Check if update needed (IP change or other differences)
			needsUpdate := changeContext.IPChanged ||
				existingRule.Fwd != desiredConfig.DstIP ||
				existingRule.Name != desiredConfig.Name ||
				existingRule.Enabled != desiredConfig.Enabled

			if needsUpdate {
				operations = append(operations, PortOperation{
					Type:         OpUpdate,
					Config:       desiredConfig,
					ExistingRule: existingRule,
					Reason:       "configuration_mismatch",
				})
			}
		}
	}

	return operations
}

// executeOperations executes port operations with proper error handling and rollback
func (r *PortForwardReconciler) executeOperations(ctx context.Context, operations []PortOperation) (*OperationResult, error) {
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
			}
		}

		if err != nil {
			result.Failed = append(result.Failed, err)

			// Attempt rollback of completed operations
			logger := ctrllog.FromContext(ctx)
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
			logger := ctrllog.FromContext(ctx)
			logger.Info("Operation completed successfully",
				"operation", op.String())
		}
	}

	// logger := ctrllog.FromContext(ctx)
	// logger.Info("All operations completed successfully",
	// 	"created_count", len(result.Created),
	// 	"updated_count", len(result.Updated),
	// 	"deleted_count", len(result.Deleted))

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
			}
		case OpDelete:
			// Deleted operation -> rollback by creating
			err = r.Router.AddPort(ctx, op.Config)
		}

		if err != nil {
			logger := ctrllog.FromContext(ctx)
			logger.Error(err, "Rollback operation failed",
				"operation", op.String())
			// Continue with other rollback operations
		}
	}

	return nil
}

// calculateDesiredState generates the desired port configurations for a service
func (r *PortForwardReconciler) calculateDesiredState(service *corev1.Service) ([]routers.PortConfig, error) {
	// Extract LoadBalancer IP
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
	servicePrefix := fmt.Sprintf("%s/%s:", service.Namespace, service.Name)
	logger := ctrllog.FromContext(context.Background()).WithValues("service", service.Name, "namespace", service.Namespace)

	// Build map of desired port configurations using dstPort-fwdPort-protocol as key
	// This ensures we only detect true conflicts where both external and internal ports match
	desiredMap := make(map[string]routers.PortConfig)
	for _, config := range desiredConfigs {
		portKey := fmt.Sprintf("%d-%d-%s", config.DstPort, config.FwdPort, config.Protocol)
		desiredMap[portKey] = config
	}

	// Check each current rule for conflicts
	for _, rule := range currentRules {
		dstPort := strToInt(rule.DstPort)
		fwdPort := strToInt(rule.FwdPort)

		// Skip if this rule is already owned by this service
		if strings.HasPrefix(rule.Name, servicePrefix) {
			continue
		}

		// Check if this exact port configuration conflicts with our desired rules
		// Use same key format as calculateDelta for consistency: dstPort-fwdPort-protocol
		portKey := fmt.Sprintf("%d-%d-%s", dstPort, fwdPort, rule.Proto)
		if desiredConfig, conflict := desiredMap[portKey]; conflict {
			// Found a true conflict - both external and internal ports match
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
