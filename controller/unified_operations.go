package controller

import (
	"context"
	"fmt"
	"strings"

	"github.com/filipowm/go-unifi/unifi"
	corev1 "k8s.io/api/core/v1"
	"kube-router-port-forward/config"
	"kube-router-port-forward/helpers"
	"kube-router-port-forward/routers"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

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
		return fmt.Sprintf("CREATE port %d → %s:%d (%s)", op.Config.DstPort, op.Config.DstIP, op.Config.FwdPort, op.Config.Protocol)
	case OpUpdate:
		return fmt.Sprintf("UPDATE port %d → %s:%d (%s)", op.Config.DstPort, op.Config.DstIP, op.Config.FwdPort, op.Config.Protocol)
	case OpDelete:
		return fmt.Sprintf("DELETE port %d (%s)", op.Config.DstPort, op.Config.Protocol)
	default:
		return fmt.Sprintf("UNKNOWN operation: %s", op.Type)
	}
}

// calculateDelta determines what operations are needed to reach desired state
func (r *PortForwardReconciler) calculateDelta(currentRules []*unifi.PortForward, desiredConfigs []routers.PortConfig, changeContext *ChangeContext, service *corev1.Service) []PortOperation {
	var operations []PortOperation
	servicePrefix := fmt.Sprintf("%s/%s:", service.Namespace, service.Name)

	// Build maps for efficient lookup
	// Create map of desired port configurations using dstPort-fwdPort-protocol as key
	// This key format ensures uniqueness for port forward rules and matches router state format
	desiredMap := make(map[string]routers.PortConfig)
	for _, config := range desiredConfigs {
		portKey := fmt.Sprintf("%d-%d-%s", config.DstPort, config.FwdPort, config.Protocol)
		desiredMap[portKey] = config
	}

	currentMap := make(map[string]*unifi.PortForward) // portKey -> existing rule
	for _, rule := range currentRules {
		if strings.HasPrefix(rule.Name, servicePrefix) {
			dstPort := r.parseIntField(rule.DstPort)
			fwdPort := r.parseIntField(rule.FwdPort)
			// Use same key format as desiredMap for proper comparison
			// This ensures we can accurately compare desired vs current router state
			portKey := fmt.Sprintf("%d-%d-%s", dstPort, fwdPort, rule.Proto)
			currentMap[portKey] = rule
		}
	}

	// Find deletions (exist in current but not desired)
	for portKey, rule := range currentMap {
		if _, desired := desiredMap[portKey]; !desired {
			dstPort := r.parseIntField(rule.DstPort)
			operations = append(operations, PortOperation{
				Type: OpDelete,
				Config: routers.PortConfig{
					Name:      rule.Name,
					DstPort:   dstPort,
					FwdPort:   r.parseIntField(rule.FwdPort),
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
		if existingRule, exists := currentMap[portKey]; !exists {
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

	ctrllog.FromContext(ctx).Info("Executing port operations",
		"total_operations", len(operations))

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

	logger := ctrllog.FromContext(ctx)
	logger.Info("All operations completed successfully",
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
					DstPort:   r.parseIntField(op.ExistingRule.DstPort),
					FwdPort:   r.parseIntField(op.ExistingRule.FwdPort),
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
