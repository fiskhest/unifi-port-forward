package controller

import (
	"fmt"
	"unifi-port-forwarder/pkg/config"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// ServiceChangePredicate replaces the individual predicates with unified change detection
type ServiceChangePredicate struct{}

// Generic implements predicate.Predicate interface
func (ServiceChangePredicate) Generic(e event.GenericEvent) bool {
	// We don't use generic events, but this method is required by predicate.Predicate
	_ = e
	var _ predicate.Predicate = ServiceChangePredicate{}
	return false
}

func (ServiceChangePredicate) Update(e event.UpdateEvent) bool {
	oldSvc, ok := e.ObjectOld.(*corev1.Service)
	if !ok {
		return false
	}

	newSvc, ok := e.ObjectNew.(*corev1.Service)
	if !ok {
		return false
	}

	oldHasFinalizer := controllerutil.ContainsFinalizer(oldSvc, config.FinalizerLabel)
	newHasFinalizer := controllerutil.ContainsFinalizer(newSvc, config.FinalizerLabel)
	oldHasAnnotation := hasPortForwardAnnotation(oldSvc)
	newHasAnnotation := hasPortForwardAnnotation(newSvc)

	logger := ctrllog.Log.WithName("predicate-update").WithValues(
		"namespace", oldSvc.Namespace,
		"name", oldSvc.Name,
		"old_has_finalizer", oldHasFinalizer,
		"new_has_finalizer", newHasFinalizer,
		"old_has_annotation", oldHasAnnotation,
		"new_has_annotation", newHasAnnotation,
		"old_finalizers", oldSvc.Finalizers,
		"new_finalizers", newSvc.Finalizers,
	)

	// Only process if service has port forwarding annotation (old or new)
	if !oldHasAnnotation && !newHasAnnotation {
		logger.V(1).Info("Update event filtered: service lacks port forwarding annotation",
			"old_has_annotation", oldHasAnnotation, "new_has_annotation", newHasAnnotation)
		return false
	}

	// Analyze what changed
	changeContext := analyzeChanges(oldSvc, newSvc)

	// Only trigger if relevant changes occurred
	if !changeContext.HasRelevantChanges() {
		ctrllog.Log.V(1).Info("Service lacks relevant changes")
		return false
	}

	return true
}

func (ServiceChangePredicate) Create(e event.CreateEvent) bool {
	svc, ok := e.Object.(*corev1.Service)
	if !ok {
		ctrllog.Log.V(1).Info("Create event object is not Service type",
			"object_type", fmt.Sprintf("%T", e.Object))
		return false
	}

	hasAnnotation := hasPortForwardAnnotation(svc)

	logger := ctrllog.Log.WithName("predicate-create").WithValues(
		"namespace", svc.Namespace,
		"name", svc.Name,
		"has_annotation", hasAnnotation,
		"finalizers", svc.Finalizers,
	)

	if !hasAnnotation {
		logger.V(1).Info("Create event filtered: service does not have port forwarding annotation")
		return false
	}

	return true
}

func (ServiceChangePredicate) Delete(e event.DeleteEvent) bool {
	svc, ok := e.Object.(*corev1.Service)
	if !ok {
		ctrllog.Log.V(1).Info("Delete event object is not Service type",
			"object_type", fmt.Sprintf("%T", e.Object))
		return false
	}

	hasFinalizer := controllerutil.ContainsFinalizer(svc, config.FinalizerLabel)
	hasAnnotation := hasPortForwardAnnotation(svc)

	// Extract service key for logging and potential duplicate detection
	serviceKey := fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)

	logger := ctrllog.Log.WithName("predicate-delete").WithValues(
		"namespace", svc.Namespace,
		"name", svc.Name,
		"service_key", serviceKey,
		"has_finalizer", hasFinalizer,
		"has_annotation", hasAnnotation,
		"deletion_timestamp", svc.DeletionTimestamp,
		"finalizers", svc.Finalizers,
		"resource_version", svc.ResourceVersion,
		"uid", svc.UID,
	)

	// PRIMARY PATH: Service currently has our finalizer - normal deletion
	if hasFinalizer {
		logger.Info("DELETE event: Service has finalizer - normal deletion path",
			"event_type", "normal_deletion")
		return true
	}

	// SECONDARY PATH: Service has annotation but no finalizer
	// This could be: 1) trailing event after cleanup, 2) orphaned service
	if hasAnnotation {
		logger.Info("DELETE event: Service has annotation but no finalizer - potential trailing event",
			"event_type", "trailing_orphaned")

		// NOTE: We can't access reconciler state from predicate
		// So we return true and let reconcile handle the decision
		// The reconciler will check recent cleanups and decide appropriately
		return true
	}

	// THIRD PATH: Service has neither finalizer nor annotation - ignore
	logger.V(1).Info("DELETE event: Service has no finalizer or annotation - ignoring",
		"event_type", "ignore")
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
