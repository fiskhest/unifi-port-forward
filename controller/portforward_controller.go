package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"kube-router-port-forward/config"
	"kube-router-port-forward/helpers"
	"kube-router-port-forward/routers"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// PortForwardReconciler reconciles Service resources
type PortForwardReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Router routers.Router
}

// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// handleIPChange handles LoadBalancer IP changes - simplified version without caching
func (r *PortForwardReconciler) handleIPChange(ctx context.Context, service *corev1.Service, lbIP string) error {
	// Always update destination IPs if service has a valid IP
	if lbIP != "" {
		return r.updateDestinationIPs(ctx, service, lbIP)
	}

	return nil
}

// updateDestinationIPs updates all port forward rules with new destination IP
func (r *PortForwardReconciler) updateDestinationIPs(ctx context.Context, service *corev1.Service, newIP string) error {
	// Get current IP from service to generate port configs that actually exist in router
	currentIP := helpers.GetLBIP(service)
	portConfigs, err := helpers.GetPortConfigs(service, currentIP, config.FilterAnnotation)
	if err != nil {
		return err
	}

	for _, config := range portConfigs {
		// Update config with new IP
		config.DstIP = newIP

		portLogger := log.FromContext(ctx).WithValues(
			"dst_port", config.DstPort,
			"new_ip", newIP,
		)

		portLogger.Info("Updating destination IP")

		if err := r.Router.UpdatePort(ctx, config.DstPort, config); err != nil {
			portLogger.Error(err, "Failed to update destination IP")
			return err
		}

		portLogger.Info("Successfully updated destination IP")
	}

	return nil
}

// Reconcile implements the reconciliation logic for Service resources
func (r *PortForwardReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("namespace", req.Namespace, "name", req.Name)

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
	}

	// Extract LoadBalancer IP once for the entire reconciliation
	lbIP := helpers.GetLBIP(service)
	if lbIP == "" {
		logger.V(1).Info("Service has no LoadBalancer IP, skipping gracefully")
		return ctrl.Result{}, nil
	}

	// Check if service needs port forwarding
	if !r.shouldProcessService(ctx, service, lbIP) {
		logger.V(1).Info("Service does not meet processing criteria")
		return ctrl.Result{}, nil
	}

	// Use unified change processing
	return r.processAllChanges(ctx, service, changeContext)
}

// handleServiceDeletion handles service deletion cleanup
func (r *PortForwardReconciler) handleServiceDeletion(ctx context.Context, namespacedName client.ObjectKey) (ctrl.Result, error) {
	log.FromContext(ctx).Info("Handling service deletion - cleaning up all port forward rules")

	// Get current port forward rules
	currentRules, err := r.Router.ListAllPortForwards(ctx)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to list current port forwards for cleanup")
		return ctrl.Result{}, err
	}

	// Remove all rules that belong to this service
	servicePrefix := fmt.Sprintf("%s/%s:", namespacedName.Namespace, namespacedName.Name)
	removedCount := 0

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
				log.FromContext(ctx).Error(err, "Failed to remove port forward rule during service deletion",
					"port", config.DstPort)
			} else {
				removedCount++
				log.FromContext(ctx).Info("Removed port forward rule during service deletion",
					"port", config.DstPort)
			}
		}
	}

	log.FromContext(ctx).Info("Service deletion cleanup completed",
		"removed_count", removedCount)

	return ctrl.Result{}, nil
}

// shouldProcessService checks if a service needs port forwarding processing
func (r *PortForwardReconciler) shouldProcessService(ctx context.Context, service *corev1.Service, lbIP string) bool {
	annotations := service.GetAnnotations()
	if annotations == nil {
		return false
	}

	_, hasPortAnnotation := annotations[config.FilterAnnotation]
	if !hasPortAnnotation {
		// Note: Using Info with V(1) instead of Debug since logr doesn't have Debug
		log.FromContext(ctx).V(1).Info(
			fmt.Sprintf("Service %s/%s does not contain FilterAnnotation %s", service.Namespace, service.Name, config.FilterAnnotation),
		)
		return false
	}

	if lbIP == "" {
		log.FromContext(ctx).V(1).Info(
			fmt.Sprintf("Service %s/%s has no LoadBalancer IP assigned", service.Namespace, service.Name),
		)
		return false
	}

	return true
}

// processAllChanges handles the unified processing of all service changes
func (r *PortForwardReconciler) processAllChanges(ctx context.Context, service *corev1.Service, changeContext *ChangeContext) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues(
		"namespace", service.Namespace,
		"name", service.Name,
	)

	// Step 1: Determine desired end state
	desiredConfigs, err := r.calculateDesiredState(service)
	if err != nil {
		logger.Error(err, "Failed to calculate desired state")
		return ctrl.Result{}, err
	}

	// Step 2: Get current state from router
	currentRules, err := r.Router.ListAllPortForwards(ctx)
	if err != nil {
		logger.Error(err, "Failed to list current port forwards")
		return ctrl.Result{}, err
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
		return ctrl.Result{}, err
	}

	logger.Info("Successfully processed service changes",
		"created_count", len(result.Created),
		"updated_count", len(result.Updated),
		"deleted_count", len(result.Deleted))

	return ctrl.Result{}, nil
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
	// Use enhanced predicate for unified change detection
	eventFilter := EnhancedServiceChangePredicate{}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		WithEventFilter(eventFilter).
		Named("port-forward-controller").
		Complete(r)
}
