package controller

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"unifi-port-forward/pkg/config"
	"unifi-port-forward/pkg/helpers"
	"unifi-port-forward/pkg/routers"

	"github.com/filipowm/go-unifi/unifi"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	MaxCleanupRetries         = 5
	CleanupRetryStartInterval = 30 * time.Second
	CleanupRetryMaxInterval   = 10 * time.Minute
	CleanupDeadline           = 2 * time.Hour
)

// PortForwardReconciler reconciles Service resources
type PortForwardReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Router         routers.Router
	Config         *config.Config
	EventPublisher *EventPublisher
	Recorder       record.EventRecorder

	PeriodicReconciler *PeriodicReconciler

	// Duplicate event detection
	recentCleanups map[string]time.Time // serviceKey -> cleanup timestamp
	cleanupMutex   sync.RWMutex         // protects recentCleanups
	cleanupWindow  time.Duration        // how long to consider a cleanup "recent"

	// Cleanup retry tracking
	cleanupRetryCount map[string]int // serviceKey -> retry count
}

// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile implements the reconciliation logic for Service resources
func (r *PortForwardReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx)
	serviceKey := fmt.Sprintf("%s/%s", req.Namespace, req.Name)

	if r.isRecentlyCleaned(serviceKey) {
		logger.V(1).Info("Skipping duplicate reconcile",
			"service_key", serviceKey,
			"cleanup_window", r.cleanupWindow)
		return ctrl.Result{}, nil
	}

	service := &corev1.Service{}
	if err := r.Get(ctx, req.NamespacedName, service); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Service not found - checking if cleanup needed",
				"namespace", req.Namespace, "name", req.Name)

			// CRITICAL FIX: Attempt cleanup even when service is not found
			// This handles the race condition where service is deleted before reconciliation runs
			if r.shouldAttemptCleanupForMissingService(ctx, req.NamespacedName) {
				logger.Info("attempting cleanup for missing service (race condition)")
				return r.handleMissingServiceCleanup(ctx, req.NamespacedName)
			}

			logger.Info("No cleanup needed for missing service")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get service")
		return ctrl.Result{}, err
	}

	logger.V(1).Info("Checking deletion status",
		"namespace", service.Namespace,
		"name", service.Name,
		"deletion_timestamp", service.DeletionTimestamp,
		"deletion_timestamp_is_zero", service.DeletionTimestamp.IsZero(),
		"has_finalizer", controllerutil.ContainsFinalizer(service, config.FinalizerLabel),
		"finalizers", service.Finalizers)

	if !service.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(service, config.FinalizerLabel) {
			result, err := r.handleFinalizerCleanup(ctx, service)
			return result, err
		}
		logger.Info("NO FINALIZER - allowing deletion")
		return ctrl.Result{}, nil
	}

	lbIP := helpers.GetLBIP(service)

	if lbIP == "" {
		return ctrl.Result{}, nil
	}

	// Check if service needs port forwarding and add finalizer if needed
	shouldManage := r.shouldProcessService(ctx, service, lbIP)

	// Add finalizer if service needs management and doesn't have it
	if shouldManage && !controllerutil.ContainsFinalizer(service, config.FinalizerLabel) {
		logger.V(1).Info("Adding finalizer to managed service", "has_finalizer", false, "should_manage", shouldManage, "ip", lbIP)

		// Manual retry on conflict to handle concurrent reconciles
		maxRetries := 3
		retryDelay := time.Millisecond * 10

		for attempt := range maxRetries {
			if attempt > 0 {
				// Re-fetch service to get latest state
				if err := r.Get(ctx, req.NamespacedName, service); err != nil {
					logger.Error(err, "Failed to re-fetch service during retry")
					return ctrl.Result{}, err
				}

				// Check if finalizer was added by another reconcile
				if controllerutil.ContainsFinalizer(service, config.FinalizerLabel) {
					logger.V(1).Info("Finalizer already added by another reconcile")
					break
				}

				logger.V(1).Info("Retrying finalizer addition", "attempt", attempt+1)
				time.Sleep(retryDelay * time.Duration(attempt))
			}

			controllerutil.AddFinalizer(service, config.FinalizerLabel)
			if err := r.Update(ctx, service); err != nil {
				if errors.IsConflict(err) && attempt < maxRetries-1 {
					logger.V(1).Info("Conflict during finalizer addition, will retry", "attempt", attempt+1)
					continue
				}
				logger.Error(err, "Failed to add finalizer")
				return ctrl.Result{}, err
			}
			break // Success
		}

		// Re-fetch service to get latest state after finalizer addition
		if err := r.Get(ctx, req.NamespacedName, service); err != nil {
			logger.Error(err, "Failed to re-fetch service after finalizer addition")
			return ctrl.Result{}, err
		}

		return ctrl.Result{Requeue: true}, nil // Requeue to continue processing
	}

	// Remove finalizer if service no longer needs management
	if !shouldManage && controllerutil.ContainsFinalizer(service, config.FinalizerLabel) {
		logger.Info("Removing finalizer from non-managed service", "should_manage", shouldManage, "has_finalizer", controllerutil.ContainsFinalizer(service, config.FinalizerLabel))

		// Clean up port forward rules before removing finalizer
		if err := r.finalizeService(ctx, service); err != nil {
			logger.Error(err, "Failed to cleanup port forward rules during finalizer removal")
			// Continue with finalizer removal even if cleanup fails
		}

		controllerutil.RemoveFinalizer(service, config.FinalizerLabel)
		if err := r.Update(ctx, service); err != nil {
			logger.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Early return for services that don't need management and don't have finalizers
	if !shouldManage && !controllerutil.ContainsFinalizer(service, config.FinalizerLabel) {
		logger.V(1).Info("Service does not meet processing criteria", "should_manage", shouldManage, "has_finalizer", controllerutil.ContainsFinalizer(service, config.FinalizerLabel))
		return ctrl.Result{}, nil
	}

	// Get current router state once per reconcile to ensure data consistency
	allCurrentRules, err := r.Router.ListAllPortForwards(ctx)
	if err != nil {
		logger.Error(err, "Failed to list current port forwards")
		return ctrl.Result{}, err
	}

	// Create change context for this reconciliation using fresh router state
	changeContext := r.detectChanges(ctx, service, serviceKey, allCurrentRules)

	// Filter rules for this specific service for logging
	var currentRules []*unifi.PortForward
	for _, rule := range allCurrentRules {
		// Extract service key from rule name (format: "namespace/service-name:port-name")
		parts := strings.SplitN(rule.Name, ":", 3)
		if len(parts) >= 2 {
			ruleServiceKey := parts[0] // parts[0] is "namespace/service-name"
			if ruleServiceKey == serviceKey {
				currentRules = append(currentRules, rule)
			}
		}
	}

	// Log service vs router state differences for debugging (using filtered service-specific rules)
	logServiceVsRouterStateDifferences(lbIP, currentRules, service.Name, service.Namespace)

	// Skip change processing during initial sync
	if changeContext.IsInitialSync {
		logger.Info("Skipping change processing during initial state synchronization")
		// changeContext.IsInitialSync = false
		return ctrl.Result{}, nil
	}

	if changeContext.HasRelevantChanges() {
		logger.V(1).Info("Processing service changes",
			"has_relevant", changeContext.HasRelevantChanges(),
			"ip_changed", changeContext.IPChanged,
			"annotation_changed", changeContext.AnnotationChanged,
			"spec_changed", changeContext.SpecChanged)

		// Use unified change processing with shared currentRules
		operations, result, err := r.processAllChanges(ctx, service, changeContext, currentRules)
		if err != nil {
			return result, err
		}

		// Publish ownership-taking events
		if r.EventPublisher != nil {
			for _, op := range operations {
				if op.Reason == "port_conflict_take_ownership" {
					oldRuleName := op.ExistingRule.Name
					newRuleName := op.Config.Name
					r.EventPublisher.PublishPortForwardTakenOwnershipEvent(ctx, service,
						oldRuleName, newRuleName, op.Config.DstPort, op.Config.Protocol)
				}
			}
		}
	}

	logger.V(1).Info("No relevant changes detected")
	return ctrl.Result{}, nil
}

// shouldProcessService checks if a service needs port forwarding processing
func (r *PortForwardReconciler) shouldProcessService(ctx context.Context, service *corev1.Service, lbIP string) bool {
	annotations := service.GetAnnotations()
	if annotations == nil {
		return false
	}

	// Check for required annotations
	_, hasPortAnnotation := annotations[config.FilterAnnotation]
	if !hasPortAnnotation {
		// Note: Using Info with V(1) instead of Debug since logr doesn't have Debug
		ctrllog.FromContext(ctx).V(1).Info(
			fmt.Sprintf("Service %s/%s does not contain FilterAnnotation %s", service.Namespace, service.Name, config.FilterAnnotation),
		)
		return false
	}

	if lbIP == "" {
		ctrllog.FromContext(ctx).V(1).Info(
			fmt.Sprintf("Service %s/%s has no LoadBalancer IP assigned", service.Namespace, service.Name),
		)
		return false
	}

	// Service should be managed if it has annotations and IP, regardless of finalizer state
	// Finalizer addition/removal is handled in the main reconcile logic
	return true
}

// processAllChanges handles the unified processing of all service changes
func (r *PortForwardReconciler) processAllChanges(ctx context.Context, service *corev1.Service, changeContext *ChangeContext, currentRules []*unifi.PortForward) ([]PortOperation, ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx)
	// Step 1: Determine desired end state
	desiredConfigs, err := r.calculateDesiredState(service)
	if err != nil {
		logger.Error(err, "calculating desired state while processing all changes")

		return nil, ctrl.Result{}, err
	}

	// Step 2: Calculate delta using unified algorithm with provided currentRules
	operations := r.calculateDelta(currentRules, desiredConfigs, changeContext, service)

	logger.V(1).Info("Calculated port operations",
		"total_operations", len(operations))

	// Step 4: Execute operations atomically
	result, err := r.executeOperations(ctx, operations)
	if err != nil {
		logger.Error(err, "Failed to execute operations",
			"failed_count", len(result.Failed))

		// Publish failure events
		if r.EventPublisher != nil {
			for _, failedErr := range result.Failed {
				r.EventPublisher.PublishPortForwardFailedEvent(ctx, service,
					"", "", "", 0, "", "OperationFailed", failedErr)
			}
		}

		return operations, ctrl.Result{}, err
	}

	logger.V(1).Info("Successfully processed service changes",
		"created_count", len(result.Created),
		"updated_count", len(result.Updated),
		"deleted_count", len(result.Deleted))

	// After successful operations, collect final rules for change context
	successfulCount := len(result.Created) + len(result.Updated)
	if successfulCount > 0 {
		// Convert desiredConfigs to string representation for PortForwardRules
		var ruleNames []string
		for _, config := range desiredConfigs {
			ruleNames = append(ruleNames, fmt.Sprintf("%d:%d", config.DstPort, config.FwdPort))
		}
		changeContext.PortForwardRules = ruleNames
	}

	// Publish events for successful operations
	if r.EventPublisher != nil && len(result.Created) > 0 {
		for _, created := range result.Created {
			lbIP := helpers.GetLBIP(service)
			portName := helpers.GetPortNameByNumber(service, created.FwdPort)
			r.EventPublisher.PublishPortForwardCreatedEvent(ctx, service,
				portName, fmt.Sprintf("%d:%d", created.DstPort, created.FwdPort),
				lbIP, created.DstIP, created.FwdPort, created.DstPort, created.Protocol, "RulesCreatedSuccessfully")
		}

		// Publish update events
		for _, updated := range result.Updated {
			lbIP := helpers.GetLBIP(service)
			// portName := helpers.GetPortNameByNumber(service, updated.FwdPort)
			r.EventPublisher.PublishPortForwardUpdatedEvent(ctx, service, updated.Name,
				fmt.Sprintf("%d:%d", updated.DstPort, updated.FwdPort),
				lbIP, updated.DstIP, updated.DstPort, updated.Protocol, "RulesUpdatedSuccessfully")
		}

		// Publish deletion events
		for _, deleted := range result.Deleted {
			portName := helpers.GetPortNameByNumber(service, deleted.FwdPort)
			r.EventPublisher.PublishPortForwardDeletedEvent(ctx, service,
				portName, fmt.Sprintf("%d:%d", deleted.DstPort, deleted.FwdPort),
				deleted.DstPort, deleted.Protocol, "RulesDeletedSuccessfully",
			)
		}
	}
	return operations, ctrl.Result{}, nil
}

// handleFinalizerCleanup performs cleanup when service is being deleted with finalizer
// New behavior: Only remove finalizer on successful cleanup, implement retry logic
func (r *PortForwardReconciler) handleFinalizerCleanup(ctx context.Context, service *corev1.Service) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx)
	serviceKey := fmt.Sprintf("%s/%s", service.Namespace, service.Name)

	// Check if we should give up on cleanup (5 retries + 2-hour deadline)
	if r.shouldGiveUpOnCleanup(serviceKey) {
		logger.Error(nil, "CLEANUP FAILED PERMANENTLY - manual intervention required",
			"service", serviceKey,
			"retry_count", r.getCleanupRetryCount(serviceKey),
			"finalizer_status", "WILL_REMAIN_FOREVER",
			"action", "service deletion is blocked until manual cleanup")
		return ctrl.Result{}, nil // Don't requeue, but don't remove finalizer
	}

	cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := r.finalizeService(cleanupCtx, service); err != nil {
		r.recordCleanupRetry(serviceKey)
		interval := r.calculateRetryInterval(serviceKey)

		logger.Error(err, "CLEANUP FAILED, will retry",
			"service", serviceKey,
			"retry_count", r.getCleanupRetryCount(serviceKey),
			"retry_interval", interval,
			"max_retries", MaxCleanupRetries,
			"total_deadline", CleanupDeadline)

		return ctrl.Result{RequeueAfter: interval}, err
	}

	// Only remove finalizer on successful cleanup
	r.clearCleanupRetries(serviceKey)

	// Mark service as recently cleaned up to prevent duplicate processing
	r.markServiceCleanup(serviceKey)

	controllerutil.RemoveFinalizer(service, config.FinalizerLabel)

	if err := r.Update(ctx, service); err != nil {
		logger.Error(err, "removing finalizer after successful cleanup",
			"service", serviceKey)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// finalizeService handles cleanup logic when a service with our finalizer is deleted
func (r *PortForwardReconciler) finalizeService(ctx context.Context, service *corev1.Service) error {
	logger := ctrllog.FromContext(ctx)

	// Get current port forward rules with timeout protection
	// TODO: withAuthRetry on all ListAllPortForwards?
	currentRules, err := r.Router.ListAllPortForwards(ctx)
	if err != nil {
		logger.Error(err, "listing port forwards during cleanup")
		return err // Return error to block finalizer removal
	}

	// Generate DELETE operations for rules belonging to this service
	var operations []PortOperation
	for _, rule := range currentRules {
		if helpers.RuleBelongsToService(rule.Name, service.Namespace, service.Name) {
			// Convert string ports to int for PortConfig
			dstPort := 0
			if rule.DstPort != "" {
				if p, err := strconv.Atoi(rule.DstPort); err == nil {
					dstPort = p
				}
			}
			fwdPort := 0
			if rule.FwdPort != "" {
				if p, err := strconv.Atoi(rule.FwdPort); err == nil {
					fwdPort = p
				}
			}

			config := routers.PortConfig{
				Name:      rule.Name,
				DstPort:   dstPort,
				FwdPort:   fwdPort,
				DstIP:     rule.Fwd,
				Protocol:  rule.Proto,
				Enabled:   rule.Enabled,
				Interface: rule.PfwdInterface,
				SrcIP:     rule.Src,
			}

			operations = append(operations, PortOperation{
				Type:         OpDelete,
				Config:       config,
				ExistingRule: rule,
				Reason:       "service_deletion_finalizer",
			})
		}
	}

	// Execute cleanup operations with proper logging and rollback
	result, err := r.executeCleanupOperations(ctx, operations, "ServiceCleanup")
	if err != nil {
		logger.Error(err, "cleanup operations failed",
			"service", fmt.Sprintf("%s/%s", service.Namespace, service.Name),
			"operations_planned", len(operations))
		return err // Return error to block finalizer removal
	}

	// Publish deletion events for successfully removed ports
	if r.EventPublisher != nil {
		for _, deletedConfig := range result.Deleted {
			portName := helpers.GetPortNameByNumber(service, deletedConfig.FwdPort)
			r.EventPublisher.PublishPortForwardDeletedEvent(ctx, service,
				portName, fmt.Sprintf("%d:%d", deletedConfig.DstPort, deletedConfig.FwdPort),
				deletedConfig.DstPort, deletedConfig.Protocol, "ServiceCleanup")
		}
	}

	logger.V(1).Info("service cleanup completed successfully",
		"service", fmt.Sprintf("%s/%s", service.Namespace, service.Name),
		"rules_removed", len(result.Deleted))

	return nil
}

// detectChanges determines what changes are needed using fresh router state
func (r *PortForwardReconciler) detectChanges(ctx context.Context, service *corev1.Service, serviceKey string, allCurrentRules []*unifi.PortForward) *ChangeContext {
	lbIP := helpers.GetLBIP(service)

	// Filter current rules for this specific service from fresh router data
	var currentRules []*unifi.PortForward
	expectedServiceKey := fmt.Sprintf("%s/%s", service.Namespace, service.Name)

	ctrllog.FromContext(ctx).V(1).Info("Detecting changes for service",
		"expected_service_key", expectedServiceKey,
		"total_router_rules", len(allCurrentRules))

	for _, rule := range allCurrentRules {
		// Extract service key from rule name (format: "namespace/service-name:port-name")
		// Use same parsing logic as initial sync for consistency
		parts := strings.SplitN(rule.Name, ":", 3)
		if len(parts) >= 2 {
			ruleServiceKey := parts[0] // parts[0] is "namespace/service-name"
			ctrllog.FromContext(ctx).V(1).Info("Checking rule for service match",
				"rule_name", rule.Name,
				"rule_service_key", ruleServiceKey,
				"expected_service_key", expectedServiceKey,
				"matches", ruleServiceKey == expectedServiceKey)
			if ruleServiceKey == expectedServiceKey {
				currentRules = append(currentRules, rule)
			}
		}
	}

	ctrllog.FromContext(ctx).V(1).Info("Service rule filtering completed",
		"service", service.Name,
		"namespace", service.Namespace,
		"matched_rules", len(currentRules),
		"total_rules", len(allCurrentRules))

	changeContext := &ChangeContext{
		ServiceKey:       serviceKey,
		ServiceNamespace: service.Namespace,
		ServiceName:      service.Name,
	}

	// Calculate desired state for optimization comparison
	desiredConfigs, err := r.calculateDesiredState(service)
	if err != nil {
		// Log error but don't fail - fall back to IP-based detection
		ctrllog.FromContext(ctx).Error(err, "Failed to calculate desired state for optimization")
		return r.fallbackToIPChangeDetection(currentRules, lbIP, changeContext)
	}

	// Perform comprehensive comparison for optimization
	if r.portConfigsMatch(ctx, currentRules, desiredConfigs, lbIP) {
		// No changes detected - skip processing
		changeContext.IsInitialSync = false
		return changeContext
	}

	// Changes detected - set appropriate flags
	// For new services (no current rules), this is a spec change
	if len(currentRules) == 0 && len(desiredConfigs) > 0 {
		changeContext.SpecChanged = true
	} else {
		// For existing services, check what changed
		return r.analyzeDetailedChanges(currentRules, desiredConfigs, lbIP, changeContext)
	}

	return changeContext
}

// PerformInitialReconciliationSync verifies router connectivity on startup
// Legacy map synchronization removed - now using fresh router state on every reconciliation
func (r *PortForwardReconciler) PerformInitialReconciliationSync(ctx context.Context) error {
	logger := ctrllog.FromContext(ctx).WithValues("operation", "initial_reconciliation_sync")

	// Verify router connectivity and get initial state
	_, err := r.Router.ListAllPortForwards(ctx)
	if err != nil {
		return fmt.Errorf("failed to verify router connectivity: %w", err)
	}

	logger.Info("Initial reconciliation sync completed - router connectivity verified")
	return nil
}

// shouldAttemptCleanupForMissingService determines if we should attempt cleanup for a missing service
// Uses fresh router state instead of stale internal maps
func (r *PortForwardReconciler) shouldAttemptCleanupForMissingService(ctx context.Context, namespacedName client.ObjectKey) bool {
	serviceKey := namespacedName.String()
	logger := ctrllog.FromContext(ctx).WithValues("service_key", serviceKey)

	// Check if recently cleaned up (new logic)
	if r.isRecentlyCleaned(serviceKey) {
		logger.Info("DECISION: No cleanup needed - service recently cleaned up",
			"cleanup_age_seconds", time.Since(r.recentCleanups[serviceKey]).Seconds())
		return false
	}

	// Primary check: fresh router state query
	currentRules, err := r.Router.ListAllPortForwards(ctx)
	if err != nil {
		logger.Error(err, "Failed to query router state for cleanup decision")
		return false // Conservative approach on error
	}

	// Check if this service has rules on the router using fresh data
	serviceRuleCount := 0
	for _, rule := range currentRules {
		if helpers.RuleBelongsToService(rule.Name, namespacedName.Namespace, namespacedName.Name) {
			serviceRuleCount++
		}
	}

	if serviceRuleCount > 0 {
		logger.Info("Cleanup needed - service has rules on router",
			"rule_count", serviceRuleCount)
		return true
	}

	// Secondary check: port conflict tracking
	usedPorts := helpers.GetUsedExternalPorts()
	for port, svc := range usedPorts {
		if svc == serviceKey {
			logger.Info("DECISION: Cleanup needed - service has marked ports",
				"port", port)
			return true
		}
	}

	logger.V(1).Info("No cleanup needed - no rules found on router")
	return false
}

// handleMissingServiceCleanup handles cleanup when service object is already deleted from Kubernetes
// This is critical race condition recovery path
func (r *PortForwardReconciler) handleMissingServiceCleanup(ctx context.Context, namespacedName client.ObjectKey) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx)

	currentRules, err := r.Router.ListAllPortForwards(ctx)
	if err != nil {
		logger.Error(err, "failed to list router rules for missing service cleanup")
		return ctrl.Result{}, err
	}

	// Generate DELETE operations for rules belonging to this missing service
	var operations []PortOperation
	for _, rule := range currentRules {
		if helpers.RuleBelongsToService(rule.Name, namespacedName.Namespace, namespacedName.Name) {
			// Convert string ports to int for PortConfig
			dstPort := 0
			if rule.DstPort != "" {
				if p, err := strconv.Atoi(rule.DstPort); err == nil {
					dstPort = p
				}
			}
			fwdPort := 0
			if rule.FwdPort != "" {
				if p, err := strconv.Atoi(rule.FwdPort); err == nil {
					fwdPort = p
				}
			}

			config := routers.PortConfig{
				Name:      rule.Name,
				DstPort:   dstPort,
				FwdPort:   fwdPort,
				DstIP:     rule.Fwd,
				Protocol:  rule.Proto,
				Enabled:   rule.Enabled,
				Interface: rule.PfwdInterface,
				SrcIP:     rule.Src,
			}

			operations = append(operations, PortOperation{
				Type:         OpDelete,
				Config:       config,
				ExistingRule: rule,
				Reason:       "missing_service_race_condition",
			})
		}
	}

	if len(operations) == 0 {
		logger.Info("no cleanup needed for missing service",
			"service_key", namespacedName.String(),
			"total_rules", len(currentRules))
		return ctrl.Result{}, nil
	}

	// Execute cleanup operations with proper logging and rollback
	result, err := r.executeCleanupOperations(ctx, operations, "MissingServiceCleanup")
	if err != nil {
		logger.Error(err, "missing service cleanup operations failed",
			"service_key", namespacedName.String(),
			"operations_planned", len(operations))
		// For missing services, return error without RequeueAfter
		// since service is already deleted from Kubernetes
		return ctrl.Result{}, err
	}

	// Mark service as recently cleaned up if any operations were performed
	r.markServiceCleanup(namespacedName.String())

	logger.Info("missing service cleanup completed successfully",
		"service_key", namespacedName.String(),
		"total_rules", len(currentRules),
		"rules_removed", len(result.Deleted))

	// Return success - service is already deleted, no need to requeue
	return ctrl.Result{}, nil
}

// markServiceCleanup records that a service was recently cleaned up
func (r *PortForwardReconciler) markServiceCleanup(serviceKey string) {
	r.cleanupMutex.Lock()
	defer r.cleanupMutex.Unlock()
	r.recentCleanups[serviceKey] = time.Now()

	// Clean up old entries periodically
	r.cleanupOldEntries()
}

// isRecentlyCleaned checks if service was cleaned up within cleanup window
func (r *PortForwardReconciler) isRecentlyCleaned(serviceKey string) bool {
	r.cleanupMutex.RLock()
	defer r.cleanupMutex.RUnlock()

	if cleanupTime, exists := r.recentCleanups[serviceKey]; exists {
		return time.Since(cleanupTime) < r.cleanupWindow
	}
	return false
}

// cleanupOldEntries removes cleanup entries older than window
func (r *PortForwardReconciler) cleanupOldEntries() {
	cutoff := time.Now().Add(-r.cleanupWindow)
	for serviceKey, cleanupTime := range r.recentCleanups {
		if cleanupTime.Before(cutoff) {
			delete(r.recentCleanups, serviceKey)
		}
	}
}

// recordCleanupRetry increments the retry count for a service
func (r *PortForwardReconciler) recordCleanupRetry(serviceKey string) {
	r.cleanupMutex.Lock()
	defer r.cleanupMutex.Unlock()
	r.cleanupRetryCount[serviceKey]++
}

// getCleanupRetryCount gets the current retry count for a service
func (r *PortForwardReconciler) getCleanupRetryCount(serviceKey string) int {
	r.cleanupMutex.RLock()
	defer r.cleanupMutex.RUnlock()
	return r.cleanupRetryCount[serviceKey]
}

// clearCleanupRetries removes retry tracking for a service
func (r *PortForwardReconciler) clearCleanupRetries(serviceKey string) {
	r.cleanupMutex.Lock()
	defer r.cleanupMutex.Unlock()
	delete(r.cleanupRetryCount, serviceKey)
}

// calculateRetryInterval calculates exponential backoff interval
func (r *PortForwardReconciler) calculateRetryInterval(serviceKey string) time.Duration {
	retryCount := r.getCleanupRetryCount(serviceKey)
	// Exponential backoff: 30s * 2^retryCount, capped at 10 minutes
	backoff := time.Duration(float64(CleanupRetryStartInterval) * math.Pow(2, float64(retryCount)))
	if backoff > CleanupRetryMaxInterval {
		backoff = CleanupRetryMaxInterval
	}
	return backoff
}

// shouldGiveUpOnCleanup checks if we should give up on cleanup due to retry limits and deadline
func (r *PortForwardReconciler) shouldGiveUpOnCleanup(serviceKey string) bool {
	retryCount := r.getCleanupRetryCount(serviceKey)

	// Give up if we've exceeded max retries
	if retryCount >= MaxCleanupRetries {
		return true
	}

	// Check if total retry time exceeds deadline (approximate calculation)
	var totalRetryTime time.Duration
	for i := 0; i <= retryCount; i++ {
		interval := time.Duration(float64(CleanupRetryStartInterval) * math.Pow(2, float64(i)))
		if interval > CleanupRetryMaxInterval {
			interval = CleanupRetryMaxInterval
		}
		totalRetryTime += interval
	}

	return totalRetryTime > CleanupDeadline
}

// SetupWithManager sets up the controller with a manager
func (r *PortForwardReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("unifi-port-forward")
	r.EventPublisher = NewEventPublisher(r.Client, r.Recorder, r.Scheme)

	// Legacy map initialization removed - now using fresh router state on every reconciliation

	// Initialize duplicate event detection
	r.recentCleanups = make(map[string]time.Time)
	r.cleanupWindow = 2 * time.Second

	// Initialize cleanup retry tracking
	r.cleanupRetryCount = make(map[string]int)

	eventFilter := ServiceChangePredicate{}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		WithEventFilter(eventFilter).
		Named("port-forward-controller").
		Complete(r)
}

// portConfigsMach returns true if the states are identical, false if changes are needed
func (r *PortForwardReconciler) portConfigsMatch(ctx context.Context, currentRules []*unifi.PortForward, desiredConfigs []routers.PortConfig, expectedIP string) bool {
	if len(currentRules) != len(desiredConfigs) {
		return false
	}

	// Build maps for efficient comparison
	currentMap := make(map[string]*unifi.PortForward)
	for _, rule := range currentRules {
		key := fmt.Sprintf("%d-%d-%s", helpers.ParseIntField(rule.DstPort),
			helpers.ParseIntField(rule.FwdPort), rule.Proto)
		currentMap[key] = rule
	}

	// Check each desired config against current state
	for _, desired := range desiredConfigs {
		key := fmt.Sprintf("%d-%d-%s", desired.DstPort, desired.FwdPort, desired.Protocol)

		current, exists := currentMap[key]
		if !exists {
			return false // Rule doesn't exist
		}

		if current.Fwd != expectedIP {
			ctrllog.FromContext(ctx).Info("IP mismatch detected in portConfigsMatch",
				"rule_name", current.Name,
				"current_fwd_ip", current.Fwd,
				"desired_fwd_ip", desired.DstIP,
				"expected_service_ip", expectedIP)
			return false // IP changed
		}

		if desired.Enabled != current.Enabled {
			return false // Enabled status changed
		}
	}

	return true // All configurations match
}

// fallbackToIPChangeDetection provides fallback when desired state calculation fails
func (r *PortForwardReconciler) fallbackToIPChangeDetection(currentRules []*unifi.PortForward, lbIP string, changeContext *ChangeContext) *ChangeContext {
	ipChanged, oldIP, newIP := compareIPsWithRouterState(lbIP, currentRules)
	if ipChanged {
		changeContext.IPChanged = true
		changeContext.OldIP = oldIP
		changeContext.NewIP = newIP
	}
	return changeContext
}

// analyzeDetailedChanges performs detailed change analysis when optimization indicates changes needed
func (r *PortForwardReconciler) analyzeDetailedChanges(currentRules []*unifi.PortForward, desiredConfigs []routers.PortConfig, lbIP string, changeContext *ChangeContext) *ChangeContext {
	// Set IsInitialSync to false for processing
	changeContext.IsInitialSync = false

	changeContext = r.fallbackToIPChangeDetection(currentRules, lbIP, changeContext)

	if len(currentRules) == 0 && len(desiredConfigs) > 0 {
		// New service - this counts as a spec change (adding ports)
		changeContext.SpecChanged = true
	} else if len(currentRules) > 0 && len(desiredConfigs) == 0 {
		// Service removed - this counts as a spec change (removing ports)
		changeContext.SpecChanged = true
	} else if len(currentRules) > 0 && len(desiredConfigs) > 0 {
		// Complex change - mark as spec changed for now
		changeContext.SpecChanged = true
	}

	// TODO: HAHA YOU FORGOT ABOUT THIS MOTHERFUCKING PIECE OF SHIT AI????
	// This is where we can add granular change tracking in the future
	return changeContext
}
