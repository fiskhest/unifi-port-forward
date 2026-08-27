package controller

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"unifi-port-forward/pkg/config"
	"unifi-port-forward/pkg/ports"
	"unifi-port-forward/pkg/routers"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	GatewayFinalizerLabel        = "unifi-port-forward.fiskhe.st/gateway-protection"
	GatewayPortForwardNamePrefix = ""
	DstIPAnnotation              = "unifi-port-forward.fiskhe.st/dst-ip"
)

type GatewayReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Router   routers.Router
	Config   *config.Config
	Recorder record.EventRecorder

	activeReconciliations sync.Map
}

func (r *GatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx).WithValues("gateway", req.NamespacedName)

	resourceKey := fmt.Sprintf("%s/%s", req.Namespace, req.Name)
	if _, exists := r.activeReconciliations.LoadOrStore(resourceKey, true); exists {
		logger.Info("Reconciliation already in progress, skipping", "resourceKey", resourceKey)
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}
	defer r.activeReconciliations.Delete(resourceKey)

	gateway := &gatewayv1.Gateway{}
	if err := r.Get(ctx, req.NamespacedName, gateway); err != nil {
		if errors.IsNotFound(err) {
			return r.handleGatewayDeletion(ctx, req.NamespacedName)
		}
		logger.Error(err, "Failed to get Gateway")
		return ctrl.Result{}, err
	}

	hasMapping := hasPortForwardMappingAnnotation(gateway)
	if !controllerutil.ContainsFinalizer(gateway, GatewayFinalizerLabel) {
		if hasMapping {
			controllerutil.AddFinalizer(gateway, GatewayFinalizerLabel)
			if err := r.Update(ctx, gateway); err != nil {
				logger.Error(err, "Failed to add finalizer")
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, nil
	}

	if !gateway.DeletionTimestamp.IsZero() {
		return r.handleGatewayDeletion(ctx, req.NamespacedName)
	}

	if !hasMapping {
		logger.V(1).Info("Gateway has no port forward mapping annotation, skipping")
		return ctrl.Result{}, nil
	}

	dstIP, err := r.resolveGatewayAddress(ctx, gateway)
	if err != nil {
		logger.Error(err, "Failed to resolve Gateway address")
		r.Recorder.Event(gateway, corev1.EventTypeWarning, "AddressNotReady",
			fmt.Sprintf("Gateway address not available: %v", err))
		return ctrl.Result{RequeueAfter: time.Minute * 2}, nil
	}

	if err := r.reconcileGatewayPortForwards(ctx, gateway, dstIP); err != nil {
		logger.Error(err, "Failed to reconcile port forwards")
		return ctrl.Result{RequeueAfter: time.Minute * 1}, nil
	}

	r.Recorder.Event(gateway, corev1.EventTypeNormal, "PortForwardsReconciled",
		"Port forwarding rules reconciled for gateway")

	logger.V(1).Info("Successfully reconciled Gateway port forwards")
	return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
}

func hasPortForwardMappingAnnotation(gateway *gatewayv1.Gateway) bool {
	if gateway == nil {
		return false
	}
	_, ok := gateway.Annotations[config.FilterAnnotation]
	return ok
}

func (r *GatewayReconciler) resolveGatewayAddress(ctx context.Context, gateway *gatewayv1.Gateway) (string, error) {
	logger := ctrllog.FromContext(ctx)

	if dstIP, ok := gateway.Annotations[DstIPAnnotation]; ok {
		logger.V(1).Info("Using destination IP from annotation", "dstIP", dstIP)
		return dstIP, nil
	}

	if len(gateway.Status.Addresses) == 0 {
		return "", fmt.Errorf("no addresses in Gateway status")
	}

	for _, addr := range gateway.Status.Addresses {
		if addr.Type != nil && *addr.Type == gatewayv1.IPAddressType {
			logger.V(1).Info("Resolved IP address from Gateway status",
				"type", *addr.Type, "value", addr.Value)
			return addr.Value, nil
		}
	}

	for _, addr := range gateway.Status.Addresses {
		if addr.Type != nil && *addr.Type == gatewayv1.HostnameAddressType {
			logger.V(1).Info("Using hostname from Gateway status (requires DNS resolution)",
				"type", *addr.Type, "value", addr.Value)
			return addr.Value, nil
		}
	}

	return "", fmt.Errorf("no IPAddress type found in Gateway status")
}

type portMapping struct {
	Protocol string
	Ports    ports.Spec
}

// parseGatewayMappingAnnotation parses "TCP:80,UDP:53" style annotations. The
// port side may also be an inclusive range ("TCP:8000-8100"). Commas already
// separate mappings, so a port list is written as repeated entries -
// "TCP:80,TCP:443" - which yields one rule per entry.
func parseGatewayMappingAnnotation(mapping string) ([]portMapping, error) {
	if mapping == "" {
		return nil, fmt.Errorf("empty mapping")
	}

	var mappings []portMapping

	for part := range strings.SplitSeq(mapping, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		kv := strings.Split(part, ":")
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid mapping format: %s (expected Protocol:Port)", part)
		}

		protocol := strings.ToUpper(strings.TrimSpace(kv[0]))

		portSpec, err := ports.Parse(strings.TrimSpace(kv[1]))
		if err != nil {
			return nil, fmt.Errorf("invalid port in mapping %q: %w", part, err)
		}

		mappings = append(mappings, portMapping{
			Protocol: protocol,
			Ports:    portSpec,
		})
	}

	if len(mappings) == 0 {
		return nil, fmt.Errorf("no valid port mappings found")
	}

	return mappings, nil
}

func (r *GatewayReconciler) reconcileGatewayPortForwards(ctx context.Context, gateway *gatewayv1.Gateway, dstIP string) error {
	logger := ctrllog.FromContext(ctx)

	mappingVal, ok := gateway.Annotations[config.FilterAnnotation]
	if !ok {
		return nil
	}

	mappings, err := parseGatewayMappingAnnotation(mappingVal)
	if err != nil {
		logger.Error(err, "Failed to parse mapping annotation")
		return err
	}

	usedPorts := make(map[string]bool)
	for _, m := range mappings {
		key := m.Ports.String()
		if usedPorts[key] {
			logger.V(1).Info("Port already processed, skipping duplicate", "port", m.Ports)
			continue
		}
		usedPorts[key] = true

		protocol := "tcp"
		if m.Protocol == "UDP" {
			protocol = "udp"
		}

		routerRule := routers.PortConfig{
			Name:      fmt.Sprintf("%s/%s-%s", gateway.Name, strings.ToLower(m.Protocol), m.Ports),
			Enabled:   true,
			Interface: "wan",
			DstPort:   m.Ports,
			FwdPort:   m.Ports,
			SrcIP:     "any",
			DstIP:     dstIP,
			Protocol:  protocol,
		}

		existingRule, exists, checkErr := r.Router.CheckPort(ctx, m.Ports, protocol)
		if checkErr != nil {
			logger.Error(checkErr, "Failed to check existing router rule")
			continue
		}

		if exists && existingRule != nil {
			needsUpdate := false

			if existingRule.Name != routerRule.Name {
				needsUpdate = true
			} else if existingRule.Fwd != routerRule.DstIP {
				needsUpdate = true
			} else if !existingRule.Enabled {
				needsUpdate = true
			}

			if needsUpdate {
				logger.Info("Updating existing port forward rule",
					"port", m.Ports, "protocol", protocol,
					"existingName", existingRule.Name, "newName", routerRule.Name)
				if err := r.Router.UpdatePort(ctx, m.Ports, routerRule); err != nil {
					logger.Error(err, "Failed to update router rule")
					continue
				}
			}
		} else {
			logger.Info("Creating new port forward rule",
				"port", m.Ports, "protocol", protocol, "dstIP", dstIP)
			if err := r.Router.AddPort(ctx, routerRule); err != nil {
				logger.Error(err, "Failed to create router rule")
				continue
			}
		}
	}

	return nil
}

func (r *GatewayReconciler) deleteGatewayPortForwards(ctx context.Context, gateway *gatewayv1.Gateway) error {
	logger := ctrllog.FromContext(ctx)

	mappingVal, ok := gateway.Annotations[config.FilterAnnotation]
	if !ok {
		return nil
	}

	mappings, err := parseGatewayMappingAnnotation(mappingVal)
	if err != nil {
		logger.Error(err, "Failed to parse mapping annotation")
		return err
	}

	for _, m := range mappings {
		protocol := "tcp"
		if m.Protocol == "UDP" {
			protocol = "udp"
		}

		ruleName := fmt.Sprintf("%s/%s-%d", gateway.Name, strings.ToLower(m.Protocol), m.Ports)

		existingRule, exists, checkErr := r.Router.CheckPort(ctx, m.Ports, protocol)
		if checkErr != nil {
			logger.Error(checkErr, "Failed to check router rule for deletion")
			continue
		}

		if !exists || existingRule == nil {
			logger.V(1).Info("Router rule not found for deletion", "ruleName", ruleName)
			continue
		}

		if existingRule.Name != ruleName {
			logger.V(1).Info("Router rule name mismatch, skipping deletion",
				"expected", ruleName, "found", existingRule.Name)
			continue
		}

		logger.Info("Deleting port forward rule",
			"port", m.Ports, "protocol", protocol, "ruleID", existingRule.ID)
		if err := r.Router.DeletePortForwardByID(ctx, existingRule.ID); err != nil {
			logger.Error(err, "Failed to delete router rule")
			return err
		}
	}

	return nil
}

func (r *GatewayReconciler) handleGatewayDeletion(ctx context.Context, namespacedName client.ObjectKey) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx)

	gateway := &gatewayv1.Gateway{}
	err := r.Get(ctx, namespacedName, gateway)

	if err == nil {
		if err := r.deleteGatewayPortForwards(ctx, gateway); err != nil {
			logger.Error(err, "Failed to delete port forwards")
			return ctrl.Result{}, err
		}

		controllerutil.RemoveFinalizer(gateway, GatewayFinalizerLabel)
		if err := r.Update(ctx, gateway); err != nil {
			logger.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
	} else if errors.IsNotFound(err) {
		logger.V(1).Info("Gateway already deleted")
	} else {
		logger.Error(err, "Failed to get Gateway during deletion")
		return ctrl.Result{}, err
	}

	logger.V(1).Info("Successfully handled Gateway deletion")
	return ctrl.Result{}, nil
}

func (r *GatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.Gateway{}).
		Complete(r)
}
