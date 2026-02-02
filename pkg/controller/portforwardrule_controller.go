package controller

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"unifi-port-forward/pkg/api/v1alpha1"
	"unifi-port-forward/pkg/config"
	"unifi-port-forward/pkg/routers"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// PortForwardRuleReconciler reconciles PortForwardRule resources
type PortForwardRuleReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Router   routers.Router
	Config   *config.Config
	Recorder record.EventRecorder

	// activeReconciliations tracks ongoing reconciliations per resource
	activeReconciliations sync.Map
}

// Reconcile implements the reconciliation logic for PortForwardRule resources
func (r *PortForwardRuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx).WithValues("portforwardrule", req.NamespacedName)

	// Check if reconciliation is already in progress for this resource
	resourceKey := fmt.Sprintf("%s/%s", req.Namespace, req.Name)
	if _, exists := r.activeReconciliations.LoadOrStore(resourceKey, true); exists {
		logger.Info("Reconciliation already in progress, skipping", "resourceKey", resourceKey)
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}
	defer r.activeReconciliations.Delete(resourceKey)

	rule := &v1alpha1.PortForwardRule{}
	if err := r.Get(ctx, req.NamespacedName, rule); err != nil {
		if errors.IsNotFound(err) {
			// PortForwardRule deleted - clean up port forwards
			return r.handleRuleDeletion(ctx, req.NamespacedName)
		}
		logger.Error(err, "Failed to get PortForwardRule")
		return ctrl.Result{}, err
	}

	if !controllerutil.ContainsFinalizer(rule, config.FinalizerLabel) {
		controllerutil.AddFinalizer(rule, config.FinalizerLabel)
		if err := r.Update(ctx, rule); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if !rule.DeletionTimestamp.IsZero() {
		return r.handleRuleDeletion(ctx, req.NamespacedName)
	}

	if err := r.validateRule(ctx, rule); err != nil {
		logger.Error(err, "Rule validation failed")
		r.updateRuleStatusWithRetry(ctx, rule, v1alpha1.PhaseFailed, err.Error())
		return ctrl.Result{}, err
	}

	if err := r.reconcilePortForwardRule(ctx, rule); err != nil {
		// Check for special overlap error that needs backoff
		if err.Error() == "PortForwardOverlaps: requires backoff" {
			logger.Info("Port forward overlap detected, applying exponential backoff",
				"rule", rule.Name,
				"namespace", rule.Namespace)
			r.updateRuleStatusWithRetry(ctx, rule, v1alpha1.PhaseFailed, "Port forward overlap conflict")
			return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
		} else {
			// For any other error, apply a shorter backoff to prevent spam
			logger.Info("Port forward reconciliation failed, applying backoff",
				"rule", rule.Name,
				"namespace", rule.Namespace,
				"error", err.Error())
			r.updateRuleStatusWithRetry(ctx, rule, v1alpha1.PhaseFailed, err.Error())
			return ctrl.Result{RequeueAfter: time.Minute * 1}, nil
		}
	}

	r.updateRuleStatusWithRetry(ctx, rule, v1alpha1.PhaseActive, "")

	logger.V(1).Info("Successfully reconciled PortForwardRule")
	return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
}

// validateRule validates the PortForwardRule
func (r *PortForwardRuleReconciler) validateRule(ctx context.Context, rule *v1alpha1.PortForwardRule) error {
	if err := rule.ValidateCreate(); len(err) > 0 {
		return fmt.Errorf("validation failed: %v", err)
	}

	if rule.Spec.ServiceRef != nil {
		if validationErrs := rule.ValidateServiceExists(ctx, r.Client); len(validationErrs) > 0 {
			return fmt.Errorf("service validation failed: %v", validationErrs)
		}
	}

	if conflictErrs := rule.ValidateCrossNamespacePortConflict(ctx, r.Client); len(conflictErrs) > 0 {
		// For cross-namespace conflicts, we update status with warnings
		// but don't fail reconciliation unless it's same-namespace conflict
		for _, err := range conflictErrs {
			if err.Type == field.ErrorTypeForbidden {
				return fmt.Errorf("port conflict: %s", err.Detail)
			}
		}
	}

	return nil
}

// reconcilePortForwardRule creates/updates the port forwarding rule on the router
func (r *PortForwardRuleReconciler) reconcilePortForwardRule(ctx context.Context, rule *v1alpha1.PortForwardRule) error {
	logger := ctrllog.FromContext(ctx)

	// Create special error type for overlap scenario
	ErrPortForwardOverlaps := fmt.Errorf("PortForwardOverlaps: requires backoff")

	var destIP string
	var destPort int
	var err error

	if rule.Spec.ServiceRef != nil {
		destIP, destPort, err = r.getServiceDestination(ctx, rule)
	} else if rule.Spec.DestinationIP != nil && rule.Spec.DestinationPort != nil {
		destIP = *rule.Spec.DestinationIP
		destPort = *rule.Spec.DestinationPort
	} else {
		return fmt.Errorf("invalid rule: neither serviceRef nor destinationIP specified")
	}

	if err != nil {
		return fmt.Errorf("failed to get destination: %w", err)
	}

	srcIP := ""
	if rule.Spec.SourceIPRestriction != nil {
		srcIP = *rule.Spec.SourceIPRestriction
	}

	routerRule := routers.PortConfig{
		Name:      fmt.Sprintf("%s/%s:%d", rule.Namespace, rule.Name, rule.Spec.ExternalPort),
		Enabled:   rule.Spec.Enabled,
		Interface: rule.Spec.Interface,
		DstPort:   rule.Spec.ExternalPort, // External port (what users connect to)
		FwdPort:   destPort,               // Internal port (what service listens on)
		SrcIP:     srcIP,
		DstIP:     destIP,
		Protocol:  rule.Spec.Protocol,
	}

	// Property-based discovery: find rule by port+protocol (annotation controller pattern)
	existingRule, exists, err := r.Router.CheckPort(ctx, rule.Spec.ExternalPort, rule.Spec.Protocol)
	if err != nil {
		return fmt.Errorf("failed to check existing router rule: %w", err)
	}

	if exists && existingRule != nil {
		// Adopt annotation controller's aggressive ownership strategy
		needsOwnership := false
		reason := ""

		// Check if we need to take ownership or update existing rule
		if !strings.HasPrefix(existingRule.Name, fmt.Sprintf("%s/%s:", rule.Namespace, rule.Name)) {
			needsOwnership = true
			reason = "ownership_takeover"
		} else if existingRule.Name != routerRule.Name {
			needsOwnership = true
			reason = "name_mismatch"
		} else if existingRule.Fwd != routerRule.DstIP {
			needsOwnership = true
			reason = "ip_mismatch"
		} else if existingRule.Enabled != routerRule.Enabled {
			needsOwnership = true
			reason = "enabled_mismatch"
		}

		if needsOwnership {
			logger.Info("Taking ownership of existing port forward rule",
				"port", rule.Spec.ExternalPort,
				"protocol", rule.Spec.Protocol,
				"existing_rule_id", existingRule.ID,
				"existing_rule_name", existingRule.Name,
				"new_rule_name", routerRule.Name,
				"reason", reason)

			// Update the rule to take ownership and fix configuration
			if err := r.Router.UpdatePort(ctx, rule.Spec.ExternalPort, routerRule); err != nil {
				if strings.Contains(err.Error(), "PortForwardOverlaps") {
					logger.Info("Port forward overlap detected during ownership takeover, applying exponential backoff",
						"port", rule.Spec.ExternalPort,
						"protocol", rule.Spec.Protocol,
						"rule_name", routerRule.Name)
					return ErrPortForwardOverlaps
				}
				return fmt.Errorf("failed to update router rule during ownership takeover: %w", err)
			}
			logger.Info("Successfully took ownership of port forward rule",
				"port", rule.Spec.ExternalPort,
				"protocol", rule.Spec.Protocol,
				"rule_id", existingRule.ID)
		} else {
			logger.V(1).Info("Port forward rule exists and matches desired configuration",
				"port", rule.Spec.ExternalPort,
				"protocol", rule.Spec.Protocol,
				"rule_id", existingRule.ID)
		}
	} else {
		// No existing rule found - create new one
		if err := r.Router.AddPort(ctx, routerRule); err != nil {
			if strings.Contains(err.Error(), "PortForwardOverlaps") {
				logger.Info("Port forward overlap detected during creation, applying exponential backoff",
					"port", rule.Spec.ExternalPort,
					"protocol", rule.Spec.Protocol,
					"rule_name", routerRule.Name)
				return ErrPortForwardOverlaps
			}
			return fmt.Errorf("failed to create router rule: %w", err)
		}
		logger.Info("Successfully created new port forward rule",
			"port", rule.Spec.ExternalPort,
			"protocol", rule.Spec.Protocol,
			"rule_name", routerRule.Name)
	}

	ruleID := fmt.Sprintf("%s/%s:%d", rule.Namespace, rule.Name, rule.Spec.ExternalPort)

	now := metav1.Now()
	rule.Status.RouterRuleID = ruleID
	rule.Status.LastAppliedTime = &now
	rule.Status.ObservedGeneration = rule.Generation

	if rule.Spec.ServiceRef != nil {
		namespace := rule.Namespace
		if rule.Spec.ServiceRef.Namespace != nil {
			namespace = *rule.Spec.ServiceRef.Namespace
		}

		rule.Status.ServiceStatus = &v1alpha1.ServiceStatus{
			Name:           rule.Spec.ServiceRef.Name,
			Namespace:      namespace,
			LoadBalancerIP: destIP,
			ServicePort:    int32(destPort),
		}
	}

	r.Recorder.Event(rule, corev1.EventTypeNormal, "RuleApplied",
		fmt.Sprintf("Port forwarding rule applied to router (ID: %s)", ruleID))

	logger.V(1).Info("Successfully applied port forwarding rule", "routerRuleID", ruleID)
	return nil
}

// getServiceDestination gets the destination IP and port from a service reference
func (r *PortForwardRuleReconciler) getServiceDestination(ctx context.Context, rule *v1alpha1.PortForwardRule) (string, int, error) {
	namespace := rule.Namespace
	if rule.Spec.ServiceRef.Namespace != nil {
		namespace = *rule.Spec.ServiceRef.Namespace
	}

	var service corev1.Service
	if err := r.Get(ctx, client.ObjectKey{Name: rule.Spec.ServiceRef.Name, Namespace: namespace}, &service); err != nil {
		return "", 0, fmt.Errorf("failed to get service: %w", err)
	}

	var destIP string
	if service.Spec.Type == corev1.ServiceTypeLoadBalancer {
		for _, ingress := range service.Status.LoadBalancer.Ingress {
			if ingress.IP != "" {
				destIP = ingress.IP
				break
			}
		}
	}

	if destIP == "" {
		return "", 0, fmt.Errorf("service %s/%s has no LoadBalancer IP", namespace, rule.Spec.ServiceRef.Name)
	}

	// Find the service port
	var destPort int
	for _, port := range service.Spec.Ports {
		if port.Name == rule.Spec.ServiceRef.Port || fmt.Sprintf("%d", port.Port) == rule.Spec.ServiceRef.Port {
			destPort = int(port.Port)
			break
		}
	}

	if destPort == 0 {
		return "", 0, fmt.Errorf("port %s not found in service %s/%s", rule.Spec.ServiceRef.Port, namespace, rule.Spec.ServiceRef.Name)
	}

	return destIP, destPort, nil
}

// updateRuleStatusWithRetry updates status of PortForwardRule with retry logic for conflicts
func (r *PortForwardRuleReconciler) updateRuleStatusWithRetry(ctx context.Context, rule *v1alpha1.PortForwardRule, phase, errorMsg string) {
	logger := ctrllog.FromContext(ctx)

	// Use the existing updateRuleStatus logic but with retry
	backoffDuration := 100 * time.Millisecond
	maxAttempts := 3

	for attempt := range maxAttempts {
		if attempt > 0 {
			logger.Info("Retrying status update attempt",
				"attempt", attempt+1,
				"maxAttempts", maxAttempts,
				"backoff", backoffDuration.String(),
				"rule", rule.Name,
				"namespace", rule.Namespace)
			time.Sleep(backoffDuration)
			backoffDuration *= 2 // Exponential backoff

			// Refresh resource to get latest version
			if getErr := r.Get(ctx, client.ObjectKey{Namespace: rule.Namespace, Name: rule.Name}, rule); getErr != nil {
				logger.Error(getErr, "Failed to refresh resource for retry",
					"attempt", attempt+1,
					"rule", rule.Name,
					"namespace", rule.Namespace)
				return // Can't retry without refreshed resource
			}
		}

		// Apply status updates (this will modify the rule in-place)
		rule.Status.Phase = phase

		conditionType := "RuleReady"
		status := metav1.ConditionFalse
		reason := "Failed"
		message := errorMsg

		if phase == v1alpha1.PhaseActive {
			status = metav1.ConditionTrue
			reason = "RuleApplied"
			message = "Port forwarding rule successfully applied"
		}

		// Update or add condition
		conditions := rule.Status.Conditions
		updatedConditions := make([]metav1.Condition, len(conditions))
		copy(updatedConditions, conditions)

		conditionFound := false
		for i, condition := range updatedConditions {
			if condition.Type == conditionType {
				updatedConditions[i].Status = status
				updatedConditions[i].Reason = reason
				updatedConditions[i].Message = message
				updatedConditions[i].LastTransitionTime = metav1.Now()
				conditionFound = true
				break
			}
		}

		if !conditionFound {
			updatedConditions = append(updatedConditions, metav1.Condition{
				Type:               conditionType,
				Status:             status,
				LastTransitionTime: metav1.Now(),
				Reason:             reason,
				Message:            message,
			})
		}

		rule.Status.Conditions = updatedConditions

		// Update error info if failed
		if phase == v1alpha1.PhaseFailed {
			var retryCount int
			if rule.Status.ErrorInfo != nil {
				retryCount = rule.Status.ErrorInfo.RetryCount + 1
			}
			rule.Status.ErrorInfo = &v1alpha1.ErrorInfo{
				Code:            "ReconciliationError",
				Message:         errorMsg,
				LastFailureTime: &metav1.Time{Time: time.Now()},
				RetryCount:      retryCount,
			}
		} else {
			rule.Status.ErrorInfo = nil
		}

		// Try to update status
		err := r.Status().Update(ctx, rule)
		if err == nil {
			logger.V(1).Info("Successfully updated rule status",
				"attempt", attempt+1,
				"phase", phase,
				"rule", rule.Name,
				"namespace", rule.Namespace)
			return // Success
		}

		if errors.IsConflict(err) {
			logger.Info("Status update conflict detected, will retry",
				"attempt", attempt+1,
				"error", err.Error(),
				"rule", rule.Name,
				"namespace", rule.Namespace)
			// Continue to next attempt with refreshed resource
			continue
		} else {
			// Non-conflict error, don't retry
			logger.Error(err, "Failed to update rule status (non-conflict error)",
				"attempt", attempt+1,
				"rule", rule.Name,
				"namespace", rule.Namespace)
			return
		}
	}

	// Max retries exceeded
	logger.Error(nil, "Failed to update status after maximum retries",
		"maxRetries", maxAttempts,
		"rule", rule.Name,
		"namespace", rule.Namespace)
}

// deleteRouterRuleByID deletes router rule using proper identification
func (r *PortForwardRuleReconciler) deleteRouterRuleByID(ctx context.Context, rule *v1alpha1.PortForwardRule) error {
	logger := ctrllog.FromContext(ctx)

	// Use CheckPort to find the actual UniFi router rule ID
	pf, exists, err := r.Router.CheckPort(ctx, rule.Spec.ExternalPort, rule.Spec.Protocol)
	if err != nil {
		return fmt.Errorf("failed to find router rule for deletion: %w", err)
	}

	if !exists {
		// Rule doesn't exist on router - consider this success
		logger.V(1).Info("Router rule not found during deletion, assuming already cleaned up",
			"port", rule.Spec.ExternalPort,
			"protocol", rule.Spec.Protocol,
			"routerRuleID", rule.Status.RouterRuleID)
		return nil
	}

	// Delete using the actual UniFi router rule ID
	logger.V(1).Info("Deleting router rule by ID",
		"routerRuleID", pf.ID,
		"port", rule.Spec.ExternalPort,
		"protocol", rule.Spec.Protocol)

	return r.Router.DeletePortForwardByID(ctx, pf.ID)
}

// handleRuleDeletion handles the deletion of a PortForwardRule
func (r *PortForwardRuleReconciler) handleRuleDeletion(ctx context.Context, namespacedName client.ObjectKey) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx)

	// Try to get the rule to extract router deletion information
	rule := &v1alpha1.PortForwardRule{}
	err := r.Get(ctx, namespacedName, rule)

	if err == nil {
		// Rule still exists - handle router deletion and finalizer removal
		if rule.Status.RouterRuleID != "" {
			if delErr := r.deleteRouterRuleByID(ctx, rule); delErr != nil {
				logger.Error(delErr, "Failed to delete router rule", "routerRuleID", rule.Status.RouterRuleID)
				// CRITICAL: Don't remove finalizer if router deletion failed
				return ctrl.Result{}, fmt.Errorf("router rule deletion failed: %w", delErr)
			}
			logger.Info("Successfully deleted router rule during rule deletion", "routerRuleID", rule.Status.RouterRuleID)
		}

		// Remove finalizer only after router rule is successfully deleted
		controllerutil.RemoveFinalizer(rule, config.FinalizerLabel)
		if updErr := r.Update(ctx, rule); updErr != nil {
			logger.Error(updErr, "Failed to remove finalizer")
			return ctrl.Result{}, updErr
		}
	} else if errors.IsNotFound(err) {
		// Rule is already deleted from K8s - this can happen if deletion was already processed
		logger.V(1).Info("PortForwardRule not found during deletion handling, likely already processed")
	} else {
		// Some other error occurred
		logger.Error(err, "Failed to get PortForwardRule during deletion")
		return ctrl.Result{}, err
	}

	logger.V(1).Info("Successfully handled PortForwardRule deletion")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *PortForwardRuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.PortForwardRule{}).
		Owns(&corev1.Service{}). // Watch services that are referenced by rules
		Complete(r)
}
