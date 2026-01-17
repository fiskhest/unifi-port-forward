package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"unifi-port-forwarder/pkg/config"
	"unifi-port-forwarder/pkg/helpers"
	"unifi-port-forwarder/pkg/routers"

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

// PortForwardReconciler reconciles Service resources
type PortForwardReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Router         routers.Router
	Config         *config.Config
	EventPublisher *EventPublisher
	Recorder       record.EventRecorder

	// Internal state synchronization maps
	ruleOwnerMap   map[string]string               // port -> serviceKey
	serviceRuleMap map[string][]*unifi.PortForward // serviceKey -> rules

	// Periodic refresh optimization fields
	mapVersion    int64        // Unix timestamp of last full refresh
	refreshTicker *time.Ticker // Periodic refresh trigger

	// Always-on periodic reconciler
	PeriodicReconciler *PeriodicReconciler
}

// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile implements the reconciliation logic for Service resources
func (r *PortForwardReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx).WithValues("namespace", req.Namespace, "name", req.Name)

	service := &corev1.Service{}
	if err := r.Get(ctx, req.NamespacedName, service); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Service not found - checking if cleanup needed",
				"namespace", req.Namespace, "name", req.Name)

			// CRITICAL FIX: Attempt cleanup even when service is not found
			// This handles the race condition where service is deleted before reconciliation runs
			if r.shouldAttemptCleanupForMissingService(req.NamespacedName) {
				logger.Info("ATTEMPTING CLEANUP FOR MISSING SERVICE (RACE CONDITION)")
				return r.handleMissingServiceCleanup(ctx, req.NamespacedName)
			}

			logger.Info("No cleanup needed for missing service")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get service")
		return ctrl.Result{}, err
	}

	logger.V(1).Info("Service fetched successfully",
		"namespace", service.Namespace,
		"name", service.Name,
		"deletion_timestamp", service.DeletionTimestamp,
		"deletion_timestamp_is_zero", service.DeletionTimestamp.IsZero(),
		"has_finalizer", controllerutil.ContainsFinalizer(service, config.FinalizerLabel),
		"finalizers", service.Finalizers)

	logger.V(1).Info("Checking deletion status",
		"deletion_timestamp_is_zero", service.DeletionTimestamp.IsZero(),
		"has_finalizer", controllerutil.ContainsFinalizer(service, config.FinalizerLabel))

	if !service.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(service, config.FinalizerLabel) {
			result, err := r.handleFinalizerCleanup(ctx, service)
			return result, err
		}
		logger.Info("NO FINALIZER - allowing deletion")
		return ctrl.Result{}, nil
	}

	// Extract LoadBalancer IP once for the entire reconciliation
	lbIP := helpers.GetLBIP(service)

	if lbIP == "" {
		logger.Info("NO LOADBALANCER IP - skipping")
		return ctrl.Result{}, nil
	}

	// Check if service needs port forwarding and add finalizer if needed
	shouldManage := r.shouldProcessService(ctx, service, lbIP)

	// Add finalizer if service needs management and doesn't have it
	if shouldManage && !controllerutil.ContainsFinalizer(service, config.FinalizerLabel) {
		logger.Info("Adding finalizer to managed service", "has_finalizer", false, "should_manage", shouldManage, "ip", lbIP)
		controllerutil.AddFinalizer(service, config.FinalizerLabel)
		if err := r.Update(ctx, service); err != nil {
			logger.Error(err, "Failed to add finalizer")
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
	currentRules, err := r.Router.ListAllPortForwards(ctx)
	if err != nil {
		logger.Error(err, "Failed to list current port forwards")
		return ctrl.Result{}, err
	}

	// Create change context for this reconciliation using fresh router state
	serviceKey := fmt.Sprintf("%s/%s", service.Namespace, service.Name)
	changeContext := r.detectChanges(ctx, service, serviceKey, currentRules)

	// Skip change processing during initial sync
	if changeContext.IsInitialSync {
		logger.Info("Skipping change processing during initial state synchronization")
		// changeContext.IsInitialSync = false
		return ctrl.Result{}, nil
	}

	if changeContext.HasRelevantChanges() {
		logger.Info("Processing service changes",
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
	logger := ctrllog.FromContext(ctx).WithValues(
		"namespace", service.Namespace,
		"name", service.Name,
	)
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
			r.EventPublisher.PublishPortForwardCreatedEvent(ctx, service,
				fmt.Sprintf("%d:%d", created.DstPort, created.FwdPort),
				lbIP, created.DstIP, created.DstPort, created.Protocol, "RulesCreatedSuccessfully")
		}

		// Publish update events
		for _, updated := range result.Updated {
			lbIP := helpers.GetLBIP(service)
			r.EventPublisher.PublishPortForwardUpdatedEvent(ctx, service,
				fmt.Sprintf("%d:%d", updated.DstPort, updated.FwdPort),
				lbIP, updated.DstIP, updated.DstPort, updated.Protocol, "RulesUpdatedSuccessfully")
		}

		// Publish deletion events
		for _, deleted := range result.Deleted {
			r.EventPublisher.PublishPortForwardDeletedEvent(ctx, service,
				fmt.Sprintf("%d:%d", deleted.DstPort, deleted.FwdPort),
				deleted.DstPort, deleted.Protocol, "RulesDeletedSuccessfully")
		}
	}
	return operations, ctrl.Result{}, nil
}

// handleFinalizerCleanup performs cleanup when service is being deleted with finalizer
// Simplified pattern - cleanup first, then always remove finalizer (never hangs)
func (r *PortForwardReconciler) handleFinalizerCleanup(ctx context.Context, service *corev1.Service) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx).WithValues("namespace", service.Namespace, "name", service.Name)

	cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := r.finalizeService(cleanupCtx, service); err != nil {
		// TODO: err no?
		logger.Error(err, "CLEANUP FAILED, but removing finalizer anyway")
	} else {
		logger.Info("CLEANUP SUCCEEDED")
	}

	// Always remove finalizer - never blocked by cleanup failures
	// TODO: err no?
	controllerutil.RemoveFinalizer(service, config.FinalizerLabel)

	if err := r.Update(ctx, service); err != nil {
		logger.Error(err, "removing finalizer")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// finalizeService handles cleanup logic when a service with our finalizer is deleted
func (r *PortForwardReconciler) finalizeService(ctx context.Context, service *corev1.Service) error {
	logger := ctrllog.FromContext(ctx).WithValues("namespace", service.Namespace, "name", service.Name)

	// Get current port forward rules with timeout protection
	currentRules, err := r.Router.ListAllPortForwards(ctx)
	if err != nil {
		logger.Error(err, "listing port forwards during cleanup")
		return err // Return error but don't block finalizer removal
	}

	removedCount := 0
	var cleanupErrors []string

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

			if err := r.Router.RemovePort(ctx, config); err != nil {
				logger.Error(err, "removing port forward rule during cleanup",
					"port", config.DstPort,
					"rule_name", rule.Name)

				cleanupErrors = append(cleanupErrors, fmt.Sprintf("port %d: %v", config.DstPort, err))
			} else {
				removedCount++

				// Add port conflict tracking cleanup
				helpers.UnmarkPortUsed(config.DstPort)
			}
		}
	}

	// Return error summary if there were cleanup failures (but don't block finalizer removal)
	if len(cleanupErrors) > 0 {
		return fmt.Errorf("cleanup completed with %d failures: %s", len(cleanupErrors), strings.Join(cleanupErrors, "; "))
	}

	return nil
}

// detectChanges determines what changes are needed using fresh router state
func (r *PortForwardReconciler) detectChanges(ctx context.Context, service *corev1.Service, serviceKey string, allCurrentRules []*unifi.PortForward) *ChangeContext {
	lbIP := helpers.GetLBIP(service)

	// Filter current rules for this specific service from fresh router data
	var currentRules []*unifi.PortForward
	expectedServiceKey := fmt.Sprintf("%s/%s", service.Namespace, service.Name)

	ctrllog.FromContext(ctx).Info("Detecting changes for service",
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

	ctrllog.FromContext(ctx).Info("Service rule filtering completed",
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

// PerformInitialReconciliationSync performs a comprehensive one-time sync of router state
// This replaces per-reconciliation syncs with a single startup scan
func (r *PortForwardReconciler) PerformInitialReconciliationSync(ctx context.Context) error {
	logger := ctrllog.FromContext(ctx).WithValues("operation", "initial_reconciliation_sync")

	// Get current router rules
	currentRules, err := r.Router.ListAllPortForwards(ctx)
	if err != nil {
		return fmt.Errorf("failed to list port forwards for initial sync: %w", err)
	}

	// Clear existing maps
	r.ruleOwnerMap = make(map[string]string)
	r.serviceRuleMap = make(map[string][]*unifi.PortForward)

	// Populate maps with current router state
	for _, rule := range currentRules {
		// Extract service key from rule name (format: "namespace/service-name:port-name")
		parts := strings.SplitN(rule.Name, ":", 3)
		if len(parts) >= 2 {
			serviceKey := parts[0] // parts[0] is "namespace/service-name"
			r.ruleOwnerMap[rule.DstPort] = serviceKey
			r.serviceRuleMap[serviceKey] = append(r.serviceRuleMap[serviceKey], rule)
		}
	}

	// Set version timestamp for refresh timing
	r.mapVersion = time.Now().Unix()

	logger.Info("Initial reconciliation sync completed",
		"total_rules", len(currentRules),
		"service_mappings", len(r.serviceRuleMap),
		"port_mappings", len(r.ruleOwnerMap))

	return nil
}

// refreshMaps performs incremental updates to internal state maps
func (r *PortForwardReconciler) refreshMaps(ctx context.Context) error {
	// Create a clean logger context with controller and component fields
	// This prevents inheriting Service-specific fields when called from Reconcile()
	baseLogger := ctrllog.FromContext(ctx)
	logger := baseLogger.WithValues(
		"controller", "port-forward-controller",
		"component", "map-refresh",
		"operation", "refresh_maps",
	)

	// Get current router state
	currentRules, err := r.Router.ListAllPortForwards(ctx)
	if err != nil {
		return fmt.Errorf("failed to list port forwards for refresh: %w", err)
	}

	// Update existing maps incrementally
	updatedCount := r.updateMapsIncrementally(currentRules)

	// Update version timestamp
	r.mapVersion = time.Now().Unix()

	logger.Info("Periodic map refresh completed",
		"updated_mappings", updatedCount,
		"total_rules", len(currentRules))

	return nil
}

// updateMapsIncrementally updates internal maps with current router state
func (r *PortForwardReconciler) updateMapsIncrementally(currentRules []*unifi.PortForward) int {
	// Create temporary maps for the new state
	newRuleOwnerMap := make(map[string]string)
	newServiceRuleMap := make(map[string][]*unifi.PortForward)

	// Populate new maps with current router state
	for _, rule := range currentRules {
		parts := strings.SplitN(rule.Name, ":", 3)
		if len(parts) >= 2 {
			serviceKey := fmt.Sprintf("%s/%s", parts[0], parts[1])
			newRuleOwnerMap[rule.DstPort] = serviceKey
			newServiceRuleMap[serviceKey] = append(newServiceRuleMap[serviceKey], rule)
		}
	}

	// Calculate update count (simple count of differences)
	updateCount := 0
	if len(newRuleOwnerMap) != len(r.ruleOwnerMap) {
		updateCount += len(newRuleOwnerMap) - len(r.ruleOwnerMap)
	}
	if len(newServiceRuleMap) != len(r.serviceRuleMap) {
		updateCount += len(newServiceRuleMap) - len(r.serviceRuleMap)
	}

	// Replace existing maps atomically
	r.ruleOwnerMap = newRuleOwnerMap
	r.serviceRuleMap = newServiceRuleMap

	return updateCount
}

// StartPeriodicRefresh starts a background goroutine for periodic map refresh
func (r *PortForwardReconciler) StartPeriodicRefresh(interval time.Duration) {
	r.refreshTicker = time.NewTicker(interval)
	go func() {
		for range r.refreshTicker.C {
			ctx := context.Background()
			logger := ctrllog.FromContext(ctx).WithValues(
				"controller", "port-forward-controller",
				"component", "map-refresh",
				"operation", "periodic_refresh",
			)
			if err := r.refreshMaps(ctx); err != nil {
				logger.Error(err, "Periodic refresh failed")
			}
		}
	}()
}

// parseIntField safely parses a string field to int
func (r *PortForwardReconciler) parseIntField(field string) int {
	if field == "" {
		return 0
	}
	if result, err := strconv.Atoi(field); err == nil {
		return result
	}
	return 0
}

// shouldAttemptCleanupForMissingService determines if we should attempt cleanup for a missing service
// Uses a simple heuristic: always attempt cleanup for services that could be ours
func (r *PortForwardReconciler) shouldAttemptCleanupForMissingService(namespacedName client.ObjectKey) bool {
	logger := ctrllog.FromContext(context.Background()).WithValues(
		"namespace", namespacedName.Namespace,
		"name", namespacedName.Name)

	logger.Info("🔍 DECISION: Always attempting cleanup for missing service",
		"service_key", namespacedName.String())

	// For now, always attempt cleanup for missing services to prevent race conditions
	// This is safer than potentially missing cleanup due to race conditions
	return true
}

// handleMissingServiceCleanup handles cleanup when service object is already deleted from Kubernetes
// This is the critical race condition recovery path
func (r *PortForwardReconciler) handleMissingServiceCleanup(ctx context.Context, namespacedName client.ObjectKey) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx).WithValues("namespace", namespacedName.Namespace, "name", namespacedName.Name)
	// Get current router rules and delete any that match this service
	currentRules, err := r.Router.ListAllPortForwards(ctx)
	if err != nil {
		logger.Error(err, "failed to list router rules for missing service cleanup")
		return ctrl.Result{}, err
	}

	removedCount := 0
	for _, rule := range currentRules {
		if helpers.RuleBelongsToService(rule.Name, namespacedName.Namespace, namespacedName.Name) {
			logger.Info("deleting rule for missing service", "rule_name", rule.Name)

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

			if err := r.Router.RemovePort(ctx, config); err != nil {
				logger.Error(err, "failed to remove port forward rule for missing service",
					"rule_name", rule.Name,
					"dst_port", config.DstPort,
					"error", err)
			} else {
				removedCount++

				helpers.UnmarkPortUsed(config.DstPort)
			}
		}
	}

	logger.Info("missing service cleanup completed",
		"service_key", namespacedName.String(),
		"total_rules", len(currentRules),
		"rules_removed", removedCount)

	// Return success - service is already deleted, no need to requeue
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with a manager
func (r *PortForwardReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Initialize internal state maps
	r.ruleOwnerMap = make(map[string]string)
	r.serviceRuleMap = make(map[string][]*unifi.PortForward)

	// Initialize recorder
	r.Recorder = mgr.GetEventRecorderFor("unifi-port-forwarder")

	// 🆕 Initialize event publisher
	r.EventPublisher = NewEventPublisher(r.Client, r.Recorder, r.Scheme)

	// Use enhanced predicate for unified change detection
	eventFilter := ServiceChangePredicate{}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		WithEventFilter(eventFilter).
		Named("port-forward-controller").
		Complete(r)
}

// Returns true if the states are identical, false if changes are needed
func (r *PortForwardReconciler) portConfigsMatch(ctx context.Context, currentRules []*unifi.PortForward, desiredConfigs []routers.PortConfig, expectedIP string) bool {
	// Quick length check
	if len(currentRules) != len(desiredConfigs) {
		return false
	}

	// Build maps for efficient comparison
	currentMap := make(map[string]*unifi.PortForward)
	for _, rule := range currentRules {
		key := fmt.Sprintf("%d-%d-%s", r.parseIntField(rule.DstPort),
			r.parseIntField(rule.FwdPort), rule.Proto)
		currentMap[key] = rule
	}

	// Check each desired config against current state
	for _, desired := range desiredConfigs {
		key := fmt.Sprintf("%d-%d-%s", desired.DstPort, desired.FwdPort, desired.Protocol)

		current, exists := currentMap[key]
		if !exists {
			return false // Rule doesn't exist
		}

		// Compare critical fields - use Fwd (actual forward IP) not DestinationIP (source IP, always "any")
		if current.Fwd != expectedIP {
			ctrllog.FromContext(ctx).Info("IP mismatch detected in portConfigsMatch",
				"rule_name", current.Name,
				"current_fwd_ip", current.Fwd,
				"current_dst_ip", current.DestinationIP,
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
	for _, rule := range currentRules {
		if rule.DestinationIP != lbIP {
			changeContext.IPChanged = true
			changeContext.OldIP = rule.DestinationIP
			changeContext.NewIP = lbIP
			break
		}
	}
	return changeContext
}

// analyzeDetailedChanges performs detailed change analysis when optimization indicates changes needed
func (r *PortForwardReconciler) analyzeDetailedChanges(currentRules []*unifi.PortForward, desiredConfigs []routers.PortConfig, lbIP string, changeContext *ChangeContext) *ChangeContext {
	// Set IsInitialSync to false for processing
	changeContext.IsInitialSync = false

	// Check for IP changes in existing rules
	if len(currentRules) > 0 && len(desiredConfigs) > 0 {
		for _, rule := range currentRules {
			if rule.DestinationIP != lbIP {
				changeContext.IPChanged = true
				changeContext.OldIP = rule.DestinationIP
				changeContext.NewIP = lbIP
				break
			}
		}
	}

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

	// Perform detailed analysis (existing logic)
	// This is where we can add granular change tracking in the future
	return changeContext
}
