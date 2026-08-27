package controller

import (
	"context"

	"unifi-port-forward/pkg/destination"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// resolveServiceDestination resolves the address router rules for a Service
// should forward to.
//
// LoadBalancer services resolve to their ingress IP; NodePort services resolve
// to a node's LAN address, which needs a cluster read, so this is a function on
// the client rather than a pure lookup on the Service object.
//
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
func resolveServiceDestination(ctx context.Context, c client.Client, service *corev1.Service) (destination.Destination, error) {
	resolver := destination.Resolver{Client: c}
	return resolver.ForService(ctx, service)
}

// serviceDestinationIP resolves just the address, yielding "" when none is
// available yet. It suits call sites that already treat a missing address as
// "nothing to do".
func serviceDestinationIP(ctx context.Context, c client.Client, service *corev1.Service) string {
	dest, err := resolveServiceDestination(ctx, c, service)
	if err != nil {
		return ""
	}
	return dest.IP
}
