package controller

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"unifi-port-forward/pkg/api/v1alpha1"
	"unifi-port-forward/pkg/config"
	"unifi-port-forward/pkg/ports"
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

// ErrPortForwardOverlaps signals that UniFi rejected a rule because it overlaps
// an existing one. Reconcile matches on the message and backs off rather than
// retrying immediately.
var ErrPortForwardOverlaps = fmt.Errorf("PortForwardOverlaps: requires backoff")

// reconcilePortForwardRule creates/updates the port forwarding rule on the router
func (r *PortForwardRuleReconciler) reconcilePortForwardRule(ctx context.Context, rule *v1alpha1.PortForwardRule) error {
	logger := ctrllog.FromContext(ctx)

	externalPorts, err := rule.Spec.ExternalPortSpec()
	if err != nil {
		return fmt.Errorf("invalid externalPort: %w", err)
	}

	bindings, err := r.destinationBindings(ctx, rule, externalPorts)
	if err != nil {
		return fmt.Errorf("failed to get destination: %w", err)
	}

	srcIP := ""
	if rule.Spec.SourceIPRestriction != nil {
		srcIP = *rule.Spec.SourceIPRestriction
	}

	ruleIDs := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		// UniFi keys rules by name, so a rule that splits across protocols has to
		// put the protocol in the name or its two halves would collide and each
		// reconcile would rewrite the other.
		name := fmt.Sprintf("%s/%s:%s", rule.Namespace, rule.Name, externalPorts)
		if len(bindings) > 1 {
			name = fmt.Sprintf("%s/%s", name, binding.Protocol)
		}

		routerRule := routers.PortConfig{
			Name:      name,
			Enabled:   rule.Spec.Enabled,
			Interface: rule.Spec.Interface,
			DstPort:   externalPorts, // External port(s) (what users connect to)
			FwdPort:   binding.Ports, // Internal port(s) (what the service listens on)
			SrcIP:     srcIP,
			DstIP:     binding.IP,
			Protocol:  binding.Protocol,
		}

		if err := r.applyRouterRule(ctx, rule, externalPorts, routerRule); err != nil {
			return err
		}
		ruleIDs = append(ruleIDs, name)
	}

	now := metav1.Now()
	rule.Status.RouterRuleID = strings.Join(ruleIDs, ",")
	rule.Status.ExternalPorts = externalPorts.String()
	rule.Status.LastAppliedTime = &now
	rule.Status.ObservedGeneration = rule.Generation

	if rule.Spec.ServiceRef != nil {
		namespace := rule.Namespace
		if rule.Spec.ServiceRef.Namespace != nil {
			namespace = *rule.Spec.ServiceRef.Namespace
		}

		internal := make([]string, 0, len(bindings))
		for _, binding := range bindings {
			if len(bindings) > 1 {
				internal = append(internal, fmt.Sprintf("%s/%s", binding.Ports, binding.Protocol))
			} else {
				internal = append(internal, binding.Ports.String())
			}
		}

		rule.Status.ServiceStatus = &v1alpha1.ServiceStatus{
			Name:           rule.Spec.ServiceRef.Name,
			Namespace:      namespace,
			LoadBalancerIP: bindings[0].IP,
			ServicePort:    int32(bindings[0].Ports.Low()),
			ServicePorts:   strings.Join(internal, ","),
		}
	}

	r.Recorder.Event(rule, corev1.EventTypeNormal, "RuleApplied",
		fmt.Sprintf("Port forwarding rule applied to router (ID: %s)", rule.Status.RouterRuleID))

	logger.V(1).Info("Successfully applied port forwarding rule", "routerRuleID", rule.Status.RouterRuleID)
	return nil
}

// applyRouterRule creates or takes ownership of the single router rule described
// by routerRule.
func (r *PortForwardRuleReconciler) applyRouterRule(ctx context.Context, rule *v1alpha1.PortForwardRule, externalPorts ports.Spec, routerRule routers.PortConfig) error {
	logger := ctrllog.FromContext(ctx)

	// Property-based discovery: find rule by port+protocol (annotation controller pattern)
	existingRule, exists, err := r.Router.CheckPort(ctx, externalPorts, routerRule.Protocol)
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
				"port", externalPorts,
				"protocol", routerRule.Protocol,
				"existing_rule_id", existingRule.ID,
				"existing_rule_name", existingRule.Name,
				"new_rule_name", routerRule.Name,
				"reason", reason)

			// Update the rule to take ownership and fix configuration
			if err := r.Router.UpdatePort(ctx, externalPorts, routerRule); err != nil {
				if strings.Contains(err.Error(), "PortForwardOverlaps") {
					logger.Info("Port forward overlap detected during ownership takeover, applying exponential backoff",
						"port", externalPorts,
						"protocol", routerRule.Protocol,
						"rule_name", routerRule.Name)
					return ErrPortForwardOverlaps
				}
				return fmt.Errorf("failed to update router rule during ownership takeover: %w", err)
			}
			logger.Info("Successfully took ownership of port forward rule",
				"port", externalPorts,
				"protocol", routerRule.Protocol,
				"rule_id", existingRule.ID)
		} else {
			logger.V(1).Info("Port forward rule exists and matches desired configuration",
				"port", externalPorts,
				"protocol", routerRule.Protocol,
				"rule_id", existingRule.ID)
		}
	} else {
		// No existing rule found - create new one
		if err := r.Router.AddPort(ctx, routerRule); err != nil {
			if strings.Contains(err.Error(), "PortForwardOverlaps") {
				logger.Info("Port forward overlap detected during creation, applying exponential backoff",
					"port", externalPorts,
					"protocol", routerRule.Protocol,
					"rule_name", routerRule.Name)
				return ErrPortForwardOverlaps
			}
			return fmt.Errorf("failed to create router rule: %w", err)
		}
		logger.Info("Successfully created new port forward rule",
			"port", externalPorts,
			"protocol", routerRule.Protocol,
			"rule_name", routerRule.Name)
	}

	return nil
}

// Protocol values accepted by PortForwardRuleSpec.Protocol.
const (
	protocolTCP  = "tcp"
	protocolUDP  = "udp"
	protocolBoth = "both"
)

// destinationBinding is one router rule's worth of resolved destination: the
// protocol it carries, and where that protocol's traffic is sent.
type destinationBinding struct {
	Protocol string
	IP       string
	Ports    ports.Spec
}

// destinationBindings resolves a rule into the router rules needed to serve it.
//
// Nearly every rule resolves to exactly one binding. The exception is a "both"
// rule pointing at a NodePort Service: Kubernetes allocates the TCP and UDP
// nodePorts of a port pair independently, so the two protocols land on different
// internal ports and no single UniFi rule - which carries one fwd_port - can
// express the pair. Those split into one binding per protocol.
func (r *PortForwardRuleReconciler) destinationBindings(ctx context.Context, rule *v1alpha1.PortForwardRule, externalPorts ports.Spec) ([]destinationBinding, error) {
	if rule.Spec.ServiceRef == nil {
		if rule.Spec.DestinationIP == nil || rule.Spec.DestinationPort == nil {
			return nil, fmt.Errorf("invalid rule: neither serviceRef nor destinationIP specified")
		}
		destPorts, err := rule.Spec.DestinationPortSpec()
		if err != nil {
			return nil, err
		}
		return []destinationBinding{{
			Protocol: rule.Spec.Protocol,
			IP:       *rule.Spec.DestinationIP,
			Ports:    destPorts,
		}}, nil
	}
	return r.getServiceDestinations(ctx, rule, externalPorts)
}

// getServiceDestinations gets the destination IP and port(s) from a service
// reference. Both are derived from the Service, so a rule using serviceRef never
// has to name a destination port itself.
//
// The address depends on how the Service is exposed - a LoadBalancer ingress IP,
// or a node's address for a NodePort service. The internal ports follow:
//
//   - a single external port forwards to the service port (nodePort for NodePort)
//   - a contiguous external range forwards to a same-sized range starting there,
//     preserving the offset of each port
//   - a discontinuous external list forwards all of its ports to the single port
//
// The range offset is skipped for NodePort services, whose nodePorts are
// allocated individually and are not contiguous.
func (r *PortForwardRuleReconciler) getServiceDestinations(ctx context.Context, rule *v1alpha1.PortForwardRule, externalPorts ports.Spec) ([]destinationBinding, error) {
	namespace := rule.Namespace
	if rule.Spec.ServiceRef.Namespace != nil {
		namespace = *rule.Spec.ServiceRef.Namespace
	}

	var service corev1.Service
	if err := r.Get(ctx, client.ObjectKey{Name: rule.Spec.ServiceRef.Name, Namespace: namespace}, &service); err != nil {
		return nil, fmt.Errorf("failed to get service: %w", err)
	}

	dest, err := resolveServiceDestination(ctx, r.Client, &service)
	if err != nil {
		return nil, err
	}

	// Find the referenced service port
	var servicePort *corev1.ServicePort
	for i, port := range service.Spec.Ports {
		if port.Name == rule.Spec.ServiceRef.Port || fmt.Sprintf("%d", port.Port) == rule.Spec.ServiceRef.Port {
			servicePort = &service.Spec.Ports[i]
			break
		}
	}

	if servicePort == nil {
		return nil, fmt.Errorf("port %s not found in service %s/%s", rule.Spec.ServiceRef.Port, namespace, rule.Spec.ServiceRef.Name)
	}

	isNodePort := service.Spec.Type == corev1.ServiceTypeNodePort

	if rule.Spec.Protocol == protocolBoth && isNodePort {
		split, err := r.splitNodePortBindings(&service, servicePort, dest.IP, namespace, rule.Spec.ServiceRef.Name)
		if err != nil {
			return nil, err
		}
		if split != nil {
			return split, nil
		}
	}

	destPort := int(servicePort.Port)
	offsetRanges := true
	if isNodePort {
		if servicePort.NodePort == 0 {
			return nil, fmt.Errorf("port %s of NodePort service %s/%s has no nodePort allocated yet",
				rule.Spec.ServiceRef.Port, namespace, rule.Spec.ServiceRef.Name)
		}
		destPort = int(servicePort.NodePort)
		offsetRanges = false
	}

	if externalPorts.IsSinglePort() || !externalPorts.IsContiguous() || !offsetRanges {
		return []destinationBinding{{Protocol: rule.Spec.Protocol, IP: dest.IP, Ports: ports.FromPort(destPort)}}, nil
	}

	destPorts, err := ports.ContiguousFrom(destPort, externalPorts.Count())
	if err != nil {
		return nil, fmt.Errorf("cannot forward external ports %s to service %s/%s port %d: %w",
			externalPorts, namespace, rule.Spec.ServiceRef.Name, destPort, err)
	}
	return []destinationBinding{{Protocol: rule.Spec.Protocol, IP: dest.IP, Ports: destPorts}}, nil
}

// splitNodePortBindings returns one binding per protocol when a "both" rule
// points at a NodePort Service whose TCP and UDP nodePorts differ.
//
// It returns nil - meaning "do not split, handle this as a single both rule" -
// when the referenced port has no sibling of the other protocol, or when the two
// were pinned to a shared nodePort, which Kubernetes permits only when set
// explicitly. Both cases are what a single "both" rule already handles correctly.
func (r *PortForwardRuleReconciler) splitNodePortBindings(service *corev1.Service, servicePort *corev1.ServicePort, destIP, namespace, serviceName string) ([]destinationBinding, error) {
	// The sibling is the entry exposing the same service port over the other
	// protocol, which is how a TCP+UDP service is spelled in a Service spec.
	var sibling *corev1.ServicePort
	for i := range service.Spec.Ports {
		candidate := &service.Spec.Ports[i]
		if candidate.Port == servicePort.Port && candidate.Protocol != servicePort.Protocol {
			sibling = candidate
			break
		}
	}

	if sibling == nil || sibling.NodePort == servicePort.NodePort {
		return nil, nil
	}

	tcp, udp := servicePort, sibling
	if servicePort.Protocol == corev1.ProtocolUDP {
		tcp, udp = sibling, servicePort
	}

	for _, p := range []*corev1.ServicePort{tcp, udp} {
		if p.NodePort == 0 {
			return nil, fmt.Errorf("port %s/%s of NodePort service %s/%s has no nodePort allocated yet",
				p.Name, strings.ToLower(string(p.Protocol)), namespace, serviceName)
		}
	}

	return []destinationBinding{
		{Protocol: protocolTCP, IP: destIP, Ports: ports.FromPort(int(tcp.NodePort))},
		{Protocol: protocolUDP, IP: destIP, Ports: ports.FromPort(int(udp.NodePort))},
	}, nil
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

	externalPorts, err := rule.Spec.ExternalPortSpec()
	if err != nil {
		return fmt.Errorf("invalid externalPort: %w", err)
	}

	// A "both" rule may have been applied as a single both rule or split into a
	// tcp and a udp rule, depending on the Service it resolved to at the time.
	// Which one it was is not recorded anywhere durable, so deletion sweeps all
	// three rather than trusting the spec to describe what is on the router.
	protocols := []string{rule.Spec.Protocol}
	if rule.Spec.Protocol == protocolBoth {
		protocols = append(protocols, protocolTCP, protocolUDP)
	}

	// Rules this CR owns are named "{namespace}/{name}:...". Matching on that
	// prefix keeps the sweep from deleting a different CR's rule that happens to
	// share an external port and protocol.
	owned := fmt.Sprintf("%s/%s:", rule.Namespace, rule.Name)

	var deleted int
	for _, protocol := range protocols {
		pf, exists, err := r.Router.CheckPort(ctx, externalPorts, protocol)
		if err != nil {
			return fmt.Errorf("failed to find router rule for deletion: %w", err)
		}

		if !exists || pf == nil {
			continue
		}

		if !strings.HasPrefix(pf.Name, owned) {
			logger.V(1).Info("Skipping router rule not owned by this resource during deletion",
				"port", externalPorts,
				"protocol", protocol,
				"existing_rule_name", pf.Name)
			continue
		}

		logger.V(1).Info("Deleting router rule by ID",
			"routerRuleID", pf.ID,
			"port", externalPorts,
			"protocol", protocol)

		if err := r.Router.DeletePortForwardByID(ctx, pf.ID); err != nil {
			return err
		}
		deleted++
	}

	if deleted == 0 {
		logger.V(1).Info("Router rule not found during deletion, assuming already cleaned up",
			"port", externalPorts,
			"protocol", rule.Spec.Protocol,
			"routerRuleID", rule.Status.RouterRuleID)
	}

	return nil
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
