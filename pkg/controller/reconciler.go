package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"unifi-port-forwarder/pkg/config"
	"unifi-port-forwarder/pkg/helpers"
	"unifi-port-forwarder/pkg/routers"

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

	// Always-on periodic reconciler
	PeriodicReconciler *PeriodicReconciler
}

// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile implements the reconciliation logic for Service resources
func (r *PortForwardReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx).WithValues("namespace", req.Namespace, "name", req.Name)

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

	// Extract change context to understand what triggered this reconciliation
	changeContext, err := extractChangeContext(service)
	if err != nil {
		logger.Error(err, "Failed to extract change context, proceeding without it")
		changeContext = &ChangeContext{
			ServiceKey:       fmt.Sprintf("%s/%s", service.Namespace, service.Name),
			ServiceNamespace: service.Namespace,
			ServiceName:      service.Name,
		}
	}

	// Log what changes we're processing
	if changeContext.HasRelevantChanges() {
		logger.Info("Processing service changes",
			"ip_changed", changeContext.IPChanged,
			"annotation_changed", changeContext.AnnotationChanged,
			"spec_changed", changeContext.SpecChanged)

		// Publish change context events
		if r.EventPublisher != nil {
			if changeContext.IPChanged {
				r.EventPublisher.PublishIPChangedEvent(ctx, service, changeContext, changeContext.OldIP, changeContext.NewIP)
			}
		}
	}

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

// handleServiceDeletion handles service deletion cleanup for services without finalizers
func (r *PortForwardReconciler) handleServiceDeletion(ctx context.Context, namespacedName client.ObjectKey) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx).WithValues("namespace", namespacedName.Namespace, "name", namespacedName.Name)
	logger.Info("Handling service deletion without finalizer - best effort cleanup")

	// Get current port forward rules
	currentRules, err := r.Router.ListAllPortForwards(ctx)
	if err != nil {
		logger.Error(err, "Failed to list current port forwards for cleanup")
		return ctrl.Result{}, err
	}

	// Remove all rules that belong to this service
	servicePrefix := fmt.Sprintf("%s/%s:", namespacedName.Namespace, namespacedName.Name)
	removedCount := 0
	var cleanupErr error

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
					"port", config.DstPort)
				cleanupErr = fmt.Errorf("failed to remove port forward rule during service deletion: %w", err)
			} else {
				removedCount++
				logger.Info("Removed port forward rule during service deletion",
					"port", config.DstPort)
				// Add port conflict tracking cleanup
				helpers.UnmarkPortUsed(config.DstPort)
			}
		}
	}

	logger.Info("Service deletion cleanup completed",
		"removed_count", removedCount)

	if cleanupErr != nil {
		return ctrl.Result{}, cleanupErr
	}
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

		// Update error context for validation failures
		errorContext := &ErrorContext{
			Timestamp:        getCurrentTime(),
			LastFailureTime:  getCurrentTime(),
			OverallStatus:    "complete_failure",
			LastErrorCode:    "VALIDATION_ERROR",
			LastErrorMessage: err.Error(),
		}

		if updateErr := updateErrorContextAnnotation(ctx, r.Client, service, errorContext); updateErr != nil {
			logger.Error(updateErr, "Failed to update error context for validation failure")
		}

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

		// Update error context on failure
		errorContext := &ErrorContext{
			Timestamp:            getCurrentTime(),
			LastFailureTime:      getCurrentTime(),
			FailedPortOperations: buildFailedOperations(result.Failed, operations),
			OverallStatus:        determineOverallStatus(len(result.Created)+len(result.Updated), len(result.Failed)),
			LastErrorCode:        "OPERATION_FAILURE",
			LastErrorMessage:     fmt.Sprintf("%d operations failed", len(result.Failed)),
		}

		// Increment retry count if there's existing error context
		if existingErrCtx, extractErr := extractErrorContext(service); extractErr == nil && existingErrCtx != nil {
			errorContext.RetryCount = existingErrCtx.RetryCount + 1
		}

		if err := updateErrorContextAnnotation(ctx, r.Client, service, errorContext); err != nil {
			logger.Error(err, "Failed to update error context annotation")
		}

		// Publish failure events
		if r.EventPublisher != nil {
			for _, failedErr := range result.Failed {
				r.EventPublisher.PublishPortForwardFailedEvent(ctx, service, changeContext,
					"", "", "", 0, "", "OperationFailed", failedErr)
			}
		}

		return nil, ctrl.Result{}, err
	} else {
		// Clear error context on success
		if err := clearErrorContextAnnotation(ctx, r.Client, service); err != nil {
			logger.Error(err, "Failed to clear error context annotation")
		}
	}

	logger.Info("Successfully processed service changes",
		"created_count", len(result.Created),
		"updated_count", len(result.Updated),
		"deleted_count", len(result.Deleted))

	// After successful operations, collect final rules for change context
	successfulCount := len(result.Created) + len(result.Updated)
	if successfulCount > 0 {
		changeContext.PortForwardRules = collectRulesForService(desiredConfigs)

		// Update service annotation with new change context (including rules)
		if err := updateChangeContextAnnotation(ctx, r.Client, service, changeContext); err != nil {
			logger.Error(err, "Failed to update change context with port forward rules")
		} else {
			logger.Info("Updated change context with port forward rules",
				"rules_count", len(changeContext.PortForwardRules))
		}
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

	// Get cleanup status annotations
	annotations := service.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	// Get current attempt count
	attemptsStr := annotations[config.CleanupAttemptsAnnotation]
	attempts := 0
	if attemptsStr != "" {
		if parsed, err := strconv.Atoi(attemptsStr); err == nil {
			attempts = parsed
		}
	}

	// Check if we've exceeded max retries
	if attempts >= r.Config.FinalizerMaxRetries {
		logger.Error(fmt.Errorf("cleanup exceeded max retries"), "Finalizer cleanup failed after maximum attempts",
			"attempts", attempts, "max_retries", r.Config.FinalizerMaxRetries)

		// Create a failure event
		r.createEvent(ctx, service, "CleanupFailed", fmt.Sprintf("Failed cleanup service: %s - exceeded maximum attempts, manual intervention required", service.Name))

		// Remove finalizer to allow deletion (with manual intervention marker)
		controllerutil.RemoveFinalizer(service, config.FinalizerLabel)
		annotations[config.CleanupStatusAnnotation] = "failed_max_retries"
		service.SetAnnotations(annotations)
		if err := r.Update(ctx, service); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Increment attempt count
	attempts++
	annotations[config.CleanupAttemptsAnnotation] = strconv.Itoa(attempts)
	annotations[config.CleanupStatusAnnotation] = "in_progress"
	service.SetAnnotations(annotations)

	// Update annotations first
	if err := r.Update(ctx, service); err != nil {
		logger.Error(err, "Failed to update cleanup annotations")
		return ctrl.Result{}, err
	}

	// Perform cleanup
	logger.Info("Performing finalizer cleanup", "attempt", attempts)
	cleanupSuccessful, err := r.performCleanup(ctx, service)
	if err != nil {
		logger.Error(err, "Cleanup attempt failed", "attempt", attempts)

		// Track cleanup failures in error context
		errorContext := &ErrorContext{
			Timestamp:        getCurrentTime(),
			LastFailureTime:  getCurrentTime(),
			OverallStatus:    "complete_failure",
			LastErrorCode:    "CLEANUP_FAILURE",
			LastErrorMessage: fmt.Sprintf("Cleanup failed: %v", err),
		}

		// Increment retry count from existing context
		if existingErrCtx, extractErr := extractErrorContext(service); extractErr == nil && existingErrCtx != nil {
			errorContext.RetryCount = existingErrCtx.RetryCount + 1
		}

		if updateErr := updateErrorContextAnnotation(ctx, r.Client, service, errorContext); updateErr != nil {
			logger.Error(updateErr, "Failed to update error context for cleanup failure")
		}

		return ctrl.Result{RequeueAfter: r.Config.FinalizerRetryInterval}, nil
	}

	if !cleanupSuccessful {
		logger.Info("Cleanup attempt unsuccessful, retrying", "attempt", attempts)
		return ctrl.Result{RequeueAfter: r.Config.FinalizerRetryInterval}, nil
	}

	// Cleanup successful - remove finalizer
	logger.Info("Cleanup successful, removing finalizer")
	controllerutil.RemoveFinalizer(service, config.FinalizerLabel)

	// Clear cleanup annotations
	annotations[config.CleanupStatusAnnotation] = "completed"
	delete(annotations, config.CleanupAttemptsAnnotation)
	service.SetAnnotations(annotations)

	if err := r.Update(ctx, service); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	r.createEvent(ctx, service, "CleanupCompleted", fmt.Sprintf("Completed cleanup service: %s", service.Name))
	return ctrl.Result{}, nil
}

// performCleanup performs the actual cleanup of port forwarding rules
func (r *PortForwardReconciler) performCleanup(ctx context.Context, service *corev1.Service) (bool, error) {
	namespacedName := client.ObjectKey{Namespace: service.Namespace, Name: service.Name}

	// Get current port forward rules
	currentRules, err := r.Router.ListAllPortForwards(ctx)
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "Failed to list current port forwards for cleanup")
		return false, err
	}

	// Remove all rules that belong to this service
	servicePrefix := fmt.Sprintf("%s/%s:", namespacedName.Namespace, namespacedName.Name)
	removedCount := 0
	var lastErr error

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
				ctrllog.FromContext(ctx).Error(err, "Failed to remove port forward rule during finalizer cleanup",
					"port", config.DstPort)
				lastErr = err
			} else {
				removedCount++
				ctrllog.FromContext(ctx).Info("Removed port forward rule during finalizer cleanup",
					"port", config.DstPort)
				// Add port conflict tracking cleanup
				helpers.UnmarkPortUsed(config.DstPort)
			}
		}
	}

	ctrllog.FromContext(ctx).Info("Finalizer cleanup completed",
		"removed_count", removedCount)

	// Consider cleanup successful if we removed any rules or there were no matching rules to remove
	hasMatching := false
	for _, rule := range currentRules {
		if strings.HasPrefix(rule.Name, servicePrefix) {
			hasMatching = true
			break
		}
	}
	success := removedCount > 0 || !hasMatching
	return success, lastErr
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
