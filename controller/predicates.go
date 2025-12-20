package controller

import (
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// LoadBalancerIPChangedPredicate triggers reconciliation when LoadBalancer IPs change
type LoadBalancerIPChangedPredicate struct{}

func (LoadBalancerIPChangedPredicate) Update(e event.UpdateEvent) bool {
	oldSvc, ok := e.ObjectOld.(*corev1.Service)
	if !ok {
		return false
	}

	newSvc, ok := e.ObjectNew.(*corev1.Service)
	if !ok {
		return false
	}

	// Only trigger if service has our annotation
	if !hasPortForwardAnnotation(newSvc) {
		return false
	}

	// Check if LoadBalancer IPs have changed
	return !loadBalancerIPsEqual(oldSvc, newSvc)
}

func (LoadBalancerIPChangedPredicate) Create(e event.CreateEvent) bool {
	svc, ok := e.Object.(*corev1.Service)
	if !ok {
		return false
	}
	return hasPortForwardAnnotation(svc)
}

func (LoadBalancerIPChangedPredicate) Delete(e event.DeleteEvent) bool {
	svc, ok := e.Object.(*corev1.Service)
	if !ok {
		return false
	}
	return hasPortForwardAnnotation(svc)
}

func (LoadBalancerIPChangedPredicate) Generic(e event.GenericEvent) bool {
	// We don't use generic events
	return false
}

// hasPortForwardAnnotation checks if service has port forwarding annotation
func hasPortForwardAnnotation(service *corev1.Service) bool {
	annotations := service.GetAnnotations()
	if annotations == nil {
		return false
	}
	_, exists := annotations["kube-port-forward-controller/ports"]
	return exists
}

// loadBalancerIPsEqual compares LoadBalancer IPs between two services
func loadBalancerIPsEqual(oldSvc, newSvc *corev1.Service) bool {
	oldIPs := getLoadBalancerIPs(oldSvc)
	newIPs := getLoadBalancerIPs(newSvc)
	return reflect.DeepEqual(oldIPs, newIPs)
}

// getLoadBalancerIPs extracts LoadBalancer IPs from service status
func getLoadBalancerIPs(service *corev1.Service) []string {
	var ips []string
	for _, ingress := range service.Status.LoadBalancer.Ingress {
		if ingress.IP != "" {
			ips = append(ips, ingress.IP)
		}
	}
	return ips
}

// PortForwardAnnotationChangedPredicate triggers reconciliation when annotations change
// for services that have or had the port forwarding annotation
type PortForwardAnnotationChangedPredicate struct{}

func (PortForwardAnnotationChangedPredicate) Update(e event.UpdateEvent) bool {
	oldSvc, ok := e.ObjectOld.(*corev1.Service)
	if !ok {
		return false
	}

	newSvc, ok := e.ObjectNew.(*corev1.Service)
	if !ok {
		return false
	}

	// Only trigger if annotations actually changed
	oldAnnotations := oldSvc.GetAnnotations()
	newAnnotations := newSvc.GetAnnotations()
	if reflect.DeepEqual(oldAnnotations, newAnnotations) {
		return false
	}

	// Only trigger if at least one version has our annotation
	return hasPortForwardAnnotation(oldSvc) || hasPortForwardAnnotation(newSvc)
}

func (PortForwardAnnotationChangedPredicate) Create(e event.CreateEvent) bool {
	svc, ok := e.Object.(*corev1.Service)
	if !ok {
		return false
	}
	return hasPortForwardAnnotation(svc)
}

func (PortForwardAnnotationChangedPredicate) Delete(e event.DeleteEvent) bool {
	svc, ok := e.Object.(*corev1.Service)
	if !ok {
		return false
	}
	return hasPortForwardAnnotation(svc)
}

func (PortForwardAnnotationChangedPredicate) Generic(e event.GenericEvent) bool {
	return false
}
