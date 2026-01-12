package controller

import (
	"unifi-port-forwarder/pkg/config"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// ServiceChangePredicate replaces the individual predicates with unified change detection
type ServiceChangePredicate struct{}

func (ServiceChangePredicate) Update(e event.UpdateEvent) bool {
	oldSvc, ok := e.ObjectOld.(*corev1.Service)
	if !ok {
		return false
	}

	newSvc, ok := e.ObjectNew.(*corev1.Service)
	if !ok {
		return false
	}

	// Only process if service has/had port forwarding
	if !hasPortForwardAnnotation(oldSvc) && !hasPortForwardAnnotation(newSvc) {
		return false
	}

	// Analyze what changed
	changeContext := analyzeChanges(oldSvc, newSvc)

	// Only trigger if relevant changes occurred
	if !changeContext.HasRelevantChanges() {
		return false
	}

	return true
}

func (ServiceChangePredicate) Create(e event.CreateEvent) bool {
	svc, ok := e.Object.(*corev1.Service)
	if !ok {
		return false
	}

	// Only process if service has port forwarding annotation
	if !hasPortForwardAnnotation(svc) {
		return false
	}

	return true
}

func (ServiceChangePredicate) Delete(e event.DeleteEvent) bool {
	svc, ok := e.Object.(*corev1.Service)
	if !ok {
		return false
	}

	// Process deletion if service has finalizer
	return controllerutil.ContainsFinalizer(svc, config.FinalizerLabel)
}

func (ServiceChangePredicate) Generic(e event.GenericEvent) bool {
	return false
}

// hasPortForwardAnnotation checks if service has port forwarding annotation
func hasPortForwardAnnotation(service *corev1.Service) bool {
	annotations := service.GetAnnotations()
	if annotations == nil {
		return false
	}
	_, exists := annotations[config.FilterAnnotation]
	return exists
}
