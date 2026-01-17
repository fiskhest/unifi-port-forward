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

	// Only process if service has both our finalizer AND port forwarding annotation
	if (!oldHasFinalizer && !newHasFinalizer) || (!oldHasAnnotation && !newHasAnnotation) {
		logger.V(1).Info("Update event filtered: service lacks finalizer and/or annotation",
			"old_has_finalizer", oldHasFinalizer, "new_has_finalizer", newHasFinalizer,
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

	logger := ctrllog.Log.WithName("predicate-delete").WithValues(
		"namespace", svc.Namespace,
		"name", svc.Name,
		"has_finalizer", hasFinalizer,
		"has_annotation", hasAnnotation,
		"deletion_timestamp", svc.DeletionTimestamp,
		"finalizers", svc.Finalizers,
	)

	// PRIMARY PATH: Service currently has our finalizer - highest priority
	if hasFinalizer {
		logger.Info("Deleting service with managed finalizer",
			"namespace", svc.Namespace,
			"name", svc.Name,
			"deletion_timestamp", svc.DeletionTimestamp)
		return true
	}

	// SECONDARY PATH: Service ever had port forwarding annotation - orphaned cleanup
	// AI: "This catches services that lost finalizer during deletion race conditions"
	// TODO: I'm not sure this should be here? Or will this return true and delete a Port Forward if we have a "valid and deployed annotation" but no finalizer?
	if hasAnnotation {
		logger.Info("Deleting service with unmanaged port forward",
			"namespace", svc.Namespace,
			"name", svc.Name,
			"deletion_timestamp", svc.DeletionTimestamp)
		return true
	}

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
