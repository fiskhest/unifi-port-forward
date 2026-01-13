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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	// Always-on periodic reconciler
	PeriodicReconciler *PeriodicReconciler
}

// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile implements the reconciliation logic for Service resources
func (r *PortForwardReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx).WithValues("namespace", req.Namespace, "name", req.Name)

	// 🆕 Initial state synchronization - sync router rules to internal maps
	if err := r.syncRouterState(ctx); err != nil {
		logger.Error(err, "Failed to sync router state on startup")
		return ctrl.Result{}, err
	}

	// Fetch the Service instance
	service := &corev1.Service{}
	if err := r.Get(ctx, req.NamespacedName, service); err != nil {
		if errors.IsNotFound(err) {
			// Service deleted - clean up port forwards
			return r.handleServiceDeletion(ctx, req.NamespacedName)
		}
		logger.Error(err, "Failed to get service")
		return ctrl.Result{}, err
	}

	// Handle deletion with finalizer blocking
	if !service.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(service, config.FinalizerLabel) {
			logger.Info("Service marked for deletion, performing finalizer cleanup")
			return r.handleFinalizerCleanup(ctx, service)
		}
		logger.V(1).Info("Service being deleted but no finalizer present")
		return ctrl.Result{}, nil
	}

	// Extract LoadBalancer IP once for the entire reconciliation
	lbIP := helpers.GetLBIP(service)
	logger.Info("Extracted LoadBalancer IP", "ip", lbIP, "len_ingress", len(service.Status.LoadBalancer.Ingress))
	if lbIP == "" {
		logger.Info("Service has no LoadBalancer IP, skipping gracefully")
		return ctrl.Result{}, nil
	}

	// Check if service needs port forwarding and add finalizer if needed
	shouldManage := r.shouldProcessService(ctx, service, lbIP)
	if shouldManage && !controllerutil.ContainsFinalizer(service, config.FinalizerLabel) {
		logger.Info("Adding finalizer to managed service")
		controllerutil.AddFinalizer(service, config.FinalizerLabel)
		if err := r.Update(ctx, service); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// If service doesn't need management but has finalizer, remove it
	if !shouldManage && controllerutil.ContainsFinalizer(service, config.FinalizerLabel) {
		logger.Info("Removing finalizer from non-managed service")
		controllerutil.RemoveFinalizer(service, config.FinalizerLabel)
		if err := r.Update(ctx, service); err != nil {
			logger.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if !shouldManage {
		logger.V(1).Info("Service does not meet processing criteria")
		return ctrl.Result{}, nil
	}

	// Create change context for this reconciliation using synchronized state
	serviceKey := fmt.Sprintf("%s/%s", service.Namespace, service.Name)
	changeContext := r.detectChanges(ctx, service, serviceKey)

	// 🆕 Skip change detection during initial sync
	if changeContext.IsInitialSync {
		logger.Info("Skipping change processing during initial state synchronization")
		changeContext.IsInitialSync = false
	}

	if changeContext.HasRelevantChanges() {
		logger.Info("Processing service changes",
			"ip_changed", changeContext.IPChanged,
			"annotation_changed", changeContext.AnnotationChanged,
			"spec_changed", changeContext.SpecChanged)

		// Use unified change processing
		operations, result, err := r.processAllChanges(ctx, service, changeContext)
		if err != nil {
			return result, err
		}

		// Publish ownership-taking events
		if r.EventPublisher != nil {
			for _, op := range operations {
				if op.Reason == "port_conflict_take_ownership" {
					oldRuleName := op.ExistingRule.Name
					newRuleName := op.Config.Name
					r.EventPublisher.PublishPortForwardTakenOwnershipEvent(ctx, service, changeContext,
						oldRuleName, newRuleName, op.Config.DstPort, op.Config.Protocol)
				}
			}
		}

		return result, nil
	}

	logger.V(1).Info("No relevant changes detected")
	return ctrl.Result{}, nil
}

// handleServiceDeletion handles service deletion cleanup for services without finalizers
func (r *PortForwardReconciler) handleServiceDeletion(ctx context.Context, namespacedName client.ObjectKey) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx).WithValues("namespace", namespacedName.Namespace, "name", namespacedName.Name)
	logger.Info("Handling service deletion without finalizer - best effort cleanup")

	// Get current port forward rules
	currentRules, err := r.Router.ListAllPortForwards(ctx)
	if err != nil {
		logger.Error(err, "Failed to list current port forwards for cleanup")
		// Don't fail the reconciliation, just log the error
		return ctrl.Result{}, nil
	}

	// Remove all rules that belong to this service
	servicePrefix := fmt.Sprintf("%s/%s:", namespacedName.Namespace, namespacedName.Name)
	removedCount := 0
	var cleanupErrors []string

	for _, rule := range currentRules {
		if strings.HasPrefix(rule.Name, servicePrefix) {
			config := routers.PortConfig{
				Name:      rule.Name,
				DstPort:   r.parseIntField(rule.DstPort),
				FwdPort:   r.parseIntField(rule.FwdPort),
				DstIP:     rule.DestinationIP,
				Protocol:  rule.Proto,
				Enabled:   rule.Enabled,
				Interface: rule.PfwdInterface,
				SrcIP:     rule.Src,
			}

			if err := r.Router.RemovePort(ctx, config); err != nil {
				logger.Error(err, "Failed to remove port forward rule during service deletion",
					"port", config.DstPort, "rule_name", rule.Name)
				cleanupErrors = append(cleanupErrors, fmt.Sprintf("port %d: %v", config.DstPort, err))
			} else {
				removedCount++
				logger.Info("Successfully removed port forward rule during service deletion",
					"port", config.DstPort, "rule_name", rule.Name)
				// Add port conflict tracking cleanup
				helpers.UnmarkPortUsed(config.DstPort)
			}
		}
	}

	logger.Info("Service deletion cleanup completed",
		"removed_count", removedCount, "errors_count", len(cleanupErrors))

	// Always return success - this is cleanup for already-deleted services
	// We don't want to retry and block other operations
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
func (r *PortForwardReconciler) processAllChanges(ctx context.Context, service *corev1.Service, changeContext *ChangeContext) ([]PortOperation, ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx).WithValues(
		"namespace", service.Namespace,
		"name", service.Name,
	)
	logger.V(1).Info("processAllChanges called", "namespace", service.Namespace, "service", service.Name)

	// Step 1: Determine desired end state
	desiredConfigs, err := r.calculateDesiredState(service)
	if err != nil {
		logger.Error(err, "Failed to calculate desired state")

		return nil, ctrl.Result{}, err
	}

	// Step 2: Get current state from router
	currentRules, err := r.Router.ListAllPortForwards(ctx)
	if err != nil {
		logger.Error(err, "Failed to list current port forwards")
		return nil, ctrl.Result{}, err
	}

	// Step 3: Calculate delta using unified algorithm
	operations := r.calculateDelta(currentRules, desiredConfigs, changeContext, service)

	logger.Info("Calculated port operations",
		"total_operations", len(operations))

	// Step 4: Execute operations atomically
	result, err := r.executeOperations(ctx, operations)
	if err != nil {
		logger.Error(err, "Failed to execute operations",
			"failed_count", len(result.Failed))

		// Publish failure events
		if r.EventPublisher != nil {
			for _, failedErr := range result.Failed {
				r.EventPublisher.PublishPortForwardFailedEvent(ctx, service, changeContext,
					"", "", "", 0, "", "OperationFailed", failedErr)
			}
		}

		return nil, ctrl.Result{}, err
	}

	logger.Info("Successfully processed service changes",
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
			r.EventPublisher.PublishPortForwardCreatedEvent(ctx, service, changeContext,
				fmt.Sprintf("%d:%d", created.DstPort, created.FwdPort),
				lbIP, created.DstIP, created.DstPort, created.Protocol, "RulesCreatedSuccessfully")
		}

		// Publish update events
		for _, updated := range result.Updated {
			lbIP := helpers.GetLBIP(service)
			r.EventPublisher.PublishPortForwardUpdatedEvent(ctx, service, changeContext,
				fmt.Sprintf("%d:%d", updated.DstPort, updated.FwdPort),
				lbIP, updated.DstIP, updated.DstPort, updated.Protocol, "RulesUpdatedSuccessfully")
		}

		// Publish deletion events
		for _, deleted := range result.Deleted {
			r.EventPublisher.PublishPortForwardDeletedEvent(ctx, service, changeContext,
				fmt.Sprintf("%d:%d", deleted.DstPort, deleted.FwdPort),
				deleted.DstPort, deleted.Protocol, "RulesDeletedSuccessfully")
		}
	}
	return operations, ctrl.Result{}, nil
}

// handleFinalizerCleanup performs cleanup when service is being deleted with finalizer
func (r *PortForwardReconciler) handleFinalizerCleanup(ctx context.Context, service *corev1.Service) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx).WithValues("namespace", service.Namespace, "name", service.Name)
	logger.Info("Starting finalizer cleanup for service deletion")

	// Perform best-effort cleanup without annotation tracking (annotations not available during deletion)
	cleanupErr := r.performBestEffortCleanup(ctx, service)

	// Always remove finalizer after cleanup attempt, even if some rules failed to delete
	// This prevents service deletion from hanging indefinitely
	logger.Info("Removing finalizer after cleanup attempt")
	controllerutil.RemoveFinalizer(service, config.FinalizerLabel)

	if err := r.Update(ctx, service); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	// Create event for visibility
	eventMessage := fmt.Sprintf("Finalizer cleanup completed for service: %s", service.Name)
	if cleanupErr != nil {
		eventMessage = fmt.Sprintf("Finalizer cleanup completed with errors for service: %s - some rules may need manual cleanup", service.Name)
		logger.Error(cleanupErr, "Cleanup completed with partial failures")
	}
	r.createEvent(ctx, service, "FinalizerCleanupCompleted", eventMessage)

	logger.Info("Finalizer cleanup completed, service can now be deleted")
	return ctrl.Result{}, nil
}

// performBestEffortCleanup performs best-effort cleanup without retry logic during finalizer cleanup
func (r *PortForwardReconciler) performBestEffortCleanup(ctx context.Context, service *corev1.Service) error {
	logger := ctrllog.FromContext(ctx).WithValues("namespace", service.Namespace, "name", service.Name)
	namespacedName := client.ObjectKey{Namespace: service.Namespace, Name: service.Name}

	// Get current port forward rules
	currentRules, err := r.Router.ListAllPortForwards(ctx)
	if err != nil {
		logger.Error(err, "Failed to list current port forwards for cleanup")
		return err
	}

	// Remove all rules that belong to this service
	servicePrefix := fmt.Sprintf("%s/%s:", namespacedName.Namespace, namespacedName.Name)
	removedCount := 0
	var cleanupErrors []string

	for _, rule := range currentRules {
		if strings.HasPrefix(rule.Name, servicePrefix) {
			config := routers.PortConfig{
				Name:      rule.Name,
				DstPort:   r.parseIntField(rule.DstPort),
				FwdPort:   r.parseIntField(rule.FwdPort),
				DstIP:     rule.DestinationIP,
				Protocol:  rule.Proto,
				Enabled:   rule.Enabled,
				Interface: rule.PfwdInterface,
				SrcIP:     rule.Src,
			}

			if err := r.Router.RemovePort(ctx, config); err != nil {
				logger.Error(err, "Failed to remove port forward rule during finalizer cleanup",
					"port", config.DstPort, "rule_name", rule.Name)
				cleanupErrors = append(cleanupErrors, fmt.Sprintf("port %d: %v", config.DstPort, err))
			} else {
				removedCount++
				logger.Info("Successfully removed port forward rule during finalizer cleanup",
					"port", config.DstPort, "rule_name", rule.Name)
				// Add port conflict tracking cleanup
				helpers.UnmarkPortUsed(config.DstPort)
			}
		}
	}

	logger.Info("Finalizer cleanup completed",
		"removed_count", removedCount, "errors_count", len(cleanupErrors))

	// Return error summary if there were cleanup failures
	if len(cleanupErrors) > 0 {
		return fmt.Errorf("cleanup completed with %d failures: %s", len(cleanupErrors), strings.Join(cleanupErrors, "; "))
	}

	return nil
}

// 🆕 syncRouterState synchronizes router rules to internal maps for state tracking
func (r *PortForwardReconciler) syncRouterState(ctx context.Context) error {
	logger := ctrllog.FromContext(ctx).WithValues("operation", "sync_router_state")

	// Get current router rules
	currentRules, err := r.Router.ListAllPortForwards(ctx)
	if err != nil {
		return fmt.Errorf("failed to list port forwards for state sync: %w", err)
	}

	// Clear existing maps
	r.ruleOwnerMap = make(map[string]string)
	r.serviceRuleMap = make(map[string][]*unifi.PortForward)

	// Populate maps with current router state
	for _, rule := range currentRules {
		// Extract service key from rule name (format: "namespace/service-name:port-name")
		parts := strings.SplitN(rule.Name, ":", 3)
		if len(parts) >= 2 {
			serviceKey := fmt.Sprintf("%s/%s", parts[0], parts[1])
			r.ruleOwnerMap[rule.DstPort] = serviceKey
			r.serviceRuleMap[serviceKey] = append(r.serviceRuleMap[serviceKey], rule)
		}
	}

	logger.Info("Synchronized router state",
		"total_rules", len(currentRules),
		"service_mappings", len(r.serviceRuleMap),
		"port_mappings", len(r.ruleOwnerMap))

	return nil
}

// 🆕 detectChanges determines what changes are needed using synchronized state
func (r *PortForwardReconciler) detectChanges(ctx context.Context, service *corev1.Service, serviceKey string) *ChangeContext {
	lbIP := helpers.GetLBIP(service)

	// Get current rules for this service from internal map
	currentRules := r.serviceRuleMap[serviceKey]

	changeContext := &ChangeContext{
		ServiceKey:       serviceKey,
		ServiceNamespace: service.Namespace,
		ServiceName:      service.Name,
		IsInitialSync:    true, // Mark as initial sync
	}

	// Calculate desired state for optimization comparison
	desiredConfigs, err := r.calculateDesiredState(service)
	if err != nil {
		// Log error but don't fail - fall back to IP-based detection
		ctrllog.FromContext(ctx).Error(err, "Failed to calculate desired state for optimization")
		return r.fallbackToIPChangeDetection(currentRules, lbIP, changeContext)
	}

	// Perform comprehensive comparison for optimization
	if r.portConfigsMatch(currentRules, desiredConfigs, lbIP) {
		// No changes detected - skip processing
		changeContext.IsInitialSync = false
		return changeContext
	}

	// Fall back to detailed change analysis
	changeContext = r.analyzeDetailedChanges(currentRules, desiredConfigs, lbIP, changeContext)
	return changeContext
}

// createEvent creates a Kubernetes event for the service
func (r *PortForwardReconciler) createEvent(ctx context.Context, service *corev1.Service, eventType, message string) {
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: service.Name + "-",
			Namespace:    service.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:            service.Kind,
			Namespace:       service.Namespace,
			Name:            service.Name,
			UID:             service.UID,
			ResourceVersion: service.ResourceVersion,
		},
		Reason:  eventType,
		Message: message,
		Source: corev1.EventSource{
			Component: "port-forward-controller",
		},
		Type:          "Normal",
		LastTimestamp: metav1.Now(),
	}

	if err := r.Create(ctx, event); err != nil {
		ctrllog.FromContext(ctx).Error(err, "Failed to create event", "event_type", eventType)
	}
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

// portConfigsMatch compares current router rules with desired configurations
// Returns true if the states are identical, false if changes are needed
func (r *PortForwardReconciler) portConfigsMatch(currentRules []*unifi.PortForward, desiredConfigs []routers.PortConfig, expectedIP string) bool {
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

		// Compare critical fields
		if current.DestinationIP != expectedIP {
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
	if currentRules != nil {
		for _, rule := range currentRules {
			if rule.DestinationIP != lbIP {
				changeContext.IPChanged = true
				changeContext.OldIP = rule.DestinationIP
				changeContext.NewIP = lbIP
				break
			}
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
