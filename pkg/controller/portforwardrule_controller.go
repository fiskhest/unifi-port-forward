package controller

import (
	"context"
	"fmt"
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
}

// +kubebuilder:rbac:groups=unifi-port-forward.fiskhe.st,resources=portforwardrules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=unifi-port-forward.fiskhe.st,resources=portforwardrules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=unifi-port-forward.fiskhe.st,resources=portforwardrules/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile implements the reconciliation logic for PortForwardRule resources
func (r *PortForwardRuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx).WithValues("portforwardrule", req.NamespacedName)

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
		r.updateRuleStatus(ctx, rule, v1alpha1.PhaseFailed, err.Error())
		return ctrl.Result{}, err
	}

	if err := r.reconcilePortForwardRule(ctx, rule); err != nil {
		logger.Error(err, "Failed to reconcile port forwarding rule")
		r.updateRuleStatus(ctx, rule, v1alpha1.PhaseFailed, err.Error())
		return ctrl.Result{}, err
	}

	r.updateRuleStatus(ctx, rule, v1alpha1.PhaseActive, "")

	logger.Info("Successfully reconciled PortForwardRule")
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
		Name:      fmt.Sprintf("portforward-%s-%s-%d", rule.Namespace, rule.Name, rule.Spec.ExternalPort),
		Enabled:   rule.Spec.Enabled,
		Interface: rule.Spec.Interface,
		DstPort:   rule.Spec.ExternalPort, // External port (what users connect to)
		FwdPort:   destPort,               // Internal port (what service listens on)
		SrcIP:     srcIP,
		DstIP:     destIP,
		Protocol:  rule.Spec.Protocol,
	}

	existingRule, exists, err := r.Router.CheckPort(ctx, rule.Spec.ExternalPort, rule.Spec.Protocol)
	if err != nil {
		return fmt.Errorf("failed to check existing router rule: %w", err)
	}

	if exists && existingRule != nil {
		if err := r.Router.UpdatePort(ctx, rule.Spec.ExternalPort, routerRule); err != nil {
			return fmt.Errorf("failed to update router rule: %w", err)
		}
	} else {
		if err := r.Router.AddPort(ctx, routerRule); err != nil {
			return fmt.Errorf("failed to create router rule: %w", err)
		}
	}

	ruleID := fmt.Sprintf("port-%d", rule.Spec.ExternalPort)

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

	logger.Info("Successfully applied port forwarding rule", "routerRuleID", ruleID)
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

// updateRuleStatus updates the status of the PortForwardRule
func (r *PortForwardRuleReconciler) updateRuleStatus(ctx context.Context, rule *v1alpha1.PortForwardRule, phase, errorMsg string) {
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
	for i, condition := range conditions {
		if condition.Type == conditionType {
			conditions[i].Status = status
			conditions[i].Reason = reason
			conditions[i].Message = message
			conditions[i].LastTransitionTime = metav1.Now()
			break
		}
	}

	// If condition not found, add it
	found := false
	for _, condition := range conditions {
		if condition.Type == conditionType {
			found = true
			break
		}
	}

	if !found {
		rule.Status.Conditions = append(conditions, metav1.Condition{
			Type:               conditionType,
			Status:             status,
			LastTransitionTime: metav1.Now(),
			Reason:             reason,
			Message:            message,
		})
	}

	// Update error info if failed
	if phase == v1alpha1.PhaseFailed {
		rule.Status.ErrorInfo = &v1alpha1.ErrorInfo{
			Code:            "ReconciliationError",
			Message:         errorMsg,
			LastFailureTime: &metav1.Time{Time: time.Now()},
			RetryCount:      rule.Status.ErrorInfo.RetryCount + 1,
		}
	} else {
		rule.Status.ErrorInfo = nil
	}

	if err := r.Status().Update(ctx, rule); err != nil {
		ctrllog.FromContext(ctx).Error(err, "Failed to update rule status")
	}
}

// handleRuleDeletion handles the deletion of a PortForwardRule
func (r *PortForwardRuleReconciler) handleRuleDeletion(ctx context.Context, namespacedName client.ObjectKey) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx)

	// If we have a router rule ID, try to delete it from router
	rule := &v1alpha1.PortForwardRule{}
	if err := r.Get(ctx, namespacedName, rule); err == nil {
		if rule.Status.RouterRuleID != "" {
			// Extract port number from rule ID for router deletion
			routerRule := routers.PortConfig{
				DstPort: rule.Spec.ExternalPort,
			}
			if err := r.Router.RemovePort(ctx, routerRule); err != nil {
				logger.Error(err, "Failed to delete router rule", "routerRuleID", rule.Status.RouterRuleID)
				// no `return err` here so finalizer can be removed
			}
		}

		controllerutil.RemoveFinalizer(rule, "unifi-port-forward.fiskhe.st/router-rule-protection")
		if err := r.Update(ctx, rule); err != nil {
			logger.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
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
