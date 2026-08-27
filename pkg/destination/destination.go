// Package destination resolves the address a router rule should forward to for
// a given Service.
//
// Which address is correct depends on how the Service is exposed:
//
//   - LoadBalancer: the ingress IP the LoadBalancer implementation assigned.
//   - NodePort: the LAN address of a node, since traffic enters through a node's
//     nodePort rather than through a dedicated service address.
//
// ClusterIP is deliberately not supported. A ClusterIP is only routable inside
// the cluster unless the service CIDR is advertised to the router, so
// programming a rule pointing at one would usually create a silently dead rule.
// Use a standalone PortForwardRule with an explicit destinationIP for that case.
package destination

import (
	"context"
	"errors"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IPAnnotation pins the destination address, overriding every other source. It
// is the same annotation the Gateway controller honours.
const IPAnnotation = "unifi-port-forward.fiskhe.st/dst-ip"

// Source records how an address was found, for logs and events.
type Source string

const (
	SourceAnnotation   Source = "annotation"
	SourceExternalIP   Source = "externalIP"
	SourceLoadBalancer Source = "loadBalancer"
	SourceNode         Source = "node"
)

// Destination is the resolved forwarding target for a Service.
type Destination struct {
	// IP is the address router rules forward to.
	IP string

	// Source records where IP came from.
	Source Source

	// NodeName is the node IP was taken from, when Source is SourceNode.
	NodeName string
}

// Resolver resolves Service addresses. Client may be nil, in which case node
// discovery is unavailable and NodePort services must carry an explicit address.
type Resolver struct {
	Client client.Client
}

// ErrUnsupportedType reports a Service this operator cannot forward to.
type ErrUnsupportedType struct {
	Type corev1.ServiceType
}

func (e *ErrUnsupportedType) Error() string {
	switch e.Type {
	case corev1.ServiceTypeClusterIP:
		return "ClusterIP services are not supported: a cluster IP is not routable from the router. " +
			"Use a LoadBalancer or NodePort service, or a standalone PortForwardRule with an explicit destinationIP"
	default:
		return fmt.Sprintf("service type %s is not supported: use LoadBalancer or NodePort", e.Type)
	}
}

// ForService resolves the address to forward to for a Service.
func (r *Resolver) ForService(ctx context.Context, service *corev1.Service) (Destination, error) {
	// An explicit annotation wins for every type - it is the escape hatch for
	// VIPs, keepalived addresses and anything else we cannot infer.
	if ip, ok := service.Annotations[IPAnnotation]; ok && ip != "" {
		return Destination{IP: ip, Source: SourceAnnotation}, nil
	}

	// An assigned ingress address is the strongest evidence available and is
	// checked before the declared type: a Service carrying one demonstrably has
	// a routable address, whatever spec.type says.
	for _, ingress := range service.Status.LoadBalancer.Ingress {
		if ingress.IP != "" {
			return Destination{IP: ingress.IP, Source: SourceLoadBalancer}, nil
		}
	}

	switch service.Spec.Type {
	case corev1.ServiceTypeLoadBalancer:
		if ip := firstExternalIP(service); ip != "" {
			return Destination{IP: ip, Source: SourceExternalIP}, nil
		}
		return Destination{}, fmt.Errorf("service %s/%s has no LoadBalancer IP", service.Namespace, service.Name)

	case corev1.ServiceTypeNodePort:
		if ip := firstExternalIP(service); ip != "" {
			return Destination{IP: ip, Source: SourceExternalIP}, nil
		}
		return r.nodeAddress(ctx, service)

	default:
		return Destination{}, &ErrUnsupportedType{Type: service.Spec.Type}
	}
}

// nodeAddress picks the node a NodePort service should be reached through.
//
// The choice is re-made on every reconcile rather than remembered, so a node
// going away heals on the next pass instead of leaving a rule pointing at it.
func (r *Resolver) nodeAddress(ctx context.Context, service *corev1.Service) (Destination, error) {
	if r.Client == nil {
		return Destination{}, fmt.Errorf(
			"cannot resolve a node address for NodePort service %s/%s: no cluster client. Set the %s annotation",
			service.Namespace, service.Name, IPAnnotation)
	}

	var nodes corev1.NodeList
	if err := r.Client.List(ctx, &nodes); err != nil {
		return Destination{}, fmt.Errorf(
			"failed to list nodes for NodePort service %s/%s (the controller needs get/list/watch on nodes, "+
				"or set the %s annotation): %w",
			service.Namespace, service.Name, IPAnnotation, err)
	}

	eligible := make([]corev1.Node, 0, len(nodes.Items))
	for _, node := range nodes.Items {
		if !isNodeReady(&node) {
			continue
		}
		// Honour the standard opt-out that external load balancers respect.
		if _, excluded := node.Labels[corev1.LabelNodeExcludeBalancers]; excluded {
			continue
		}
		if nodeIP(&node) == "" {
			continue
		}
		eligible = append(eligible, node)
	}

	if len(eligible) == 0 {
		return Destination{}, fmt.Errorf(
			"no eligible node found for NodePort service %s/%s: need a Ready node with an address that is not "+
				"labelled %s, or set the %s annotation",
			service.Namespace, service.Name, corev1.LabelNodeExcludeBalancers, IPAnnotation)
	}

	// Sort by name so the same cluster always yields the same node, which keeps
	// the rule stable across reconciles instead of flapping between nodes.
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].Name < eligible[j].Name })

	chosen := eligible[0]
	return Destination{IP: nodeIP(&chosen), Source: SourceNode, NodeName: chosen.Name}, nil
}

// nodeIP prefers the node's internal address, which is the LAN address a router
// on the same network reaches it by. An external address is used only when
// there is no internal one.
func nodeIP(node *corev1.Node) string {
	var external string
	for _, addr := range node.Status.Addresses {
		switch addr.Type {
		case corev1.NodeInternalIP:
			if addr.Address != "" {
				return addr.Address
			}
		case corev1.NodeExternalIP:
			if external == "" {
				external = addr.Address
			}
		}
	}
	return external
}

func isNodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func firstExternalIP(service *corev1.Service) string {
	for _, ip := range service.Spec.ExternalIPs {
		if ip != "" {
			return ip
		}
	}
	return ""
}

// DeclaredIP returns the address the Service object itself declares: the
// annotation, an external IP, or a LoadBalancer ingress IP.
//
// It is a pure lookup with no cluster access, for callers such as event
// predicates that only need to notice when a Service's own address changed. A
// NodePort service that relies on node discovery declares nothing, so this
// returns "" for it and such changes are caught by the periodic drift pass
// instead.
func DeclaredIP(service *corev1.Service) string {
	if ip, ok := service.Annotations[IPAnnotation]; ok && ip != "" {
		return ip
	}
	for _, ingress := range service.Status.LoadBalancer.Ingress {
		if ingress.IP != "" {
			return ingress.IP
		}
	}
	return firstExternalIP(service)
}

// IsSupportedType reports whether this operator can forward to a Service type.
func IsSupportedType(serviceType corev1.ServiceType) bool {
	return serviceType == corev1.ServiceTypeLoadBalancer || serviceType == corev1.ServiceTypeNodePort
}

// IsUnsupportedType reports whether err says the Service type can never be
// forwarded, as opposed to an address that is merely not available yet.
func IsUnsupportedType(err error) bool {
	var unsupported *ErrUnsupportedType
	return errors.As(err, &unsupported)
}
