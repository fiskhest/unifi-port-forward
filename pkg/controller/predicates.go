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
	ctrllog.Log.Info("🔍 UPDATE EVENT RECEIVED - DIAGNOSTIC MODE",
		"event_type", "UPDATE",
		"object_type", fmt.Sprintf("%T", e.ObjectOld),
		"namespace", e.ObjectOld.GetNamespace(),
		"name", e.ObjectOld.GetName())

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
		ctrllog.Log.Info("SERVICE LACKED PORTFORWARD ANNOTATION")
		return false
	}

	// Analyze what changed
	changeContext := analyzeChanges(oldSvc, newSvc)

	// Only trigger if relevant changes occurred
	if !changeContext.HasRelevantChanges() {
		ctrllog.Log.Info("SERVICE LACKED HasRelevantChanges()")

		return false
	}

	ctrllog.Log.Info("🔍 UPDATE EVENT RECEIVED - DID WE COME TO THE END OF THE UPDATE PREDICATE???",
		"event_type", "UPDATE",
		"object_type", fmt.Sprintf("%T", e.ObjectOld),
		"namespace", e.ObjectOld.GetNamespace(),
		"name", e.ObjectOld.GetName())

	return true
}

func (ServiceChangePredicate) Create(e event.CreateEvent) bool {
	ctrllog.Log.Info("🔍 CREATE EVENT RECEIVED - DIAGNOSTIC MODE",
		"event_type", "CREATE",
		"object_type", fmt.Sprintf("%T", e.Object),
		"namespace", e.Object.GetNamespace(),
		"name", e.Object.GetName())

	svc, ok := e.Object.(*corev1.Service)
	if !ok {
		ctrllog.Log.Info("❌ Create event object is not Service type",
			"object_type", fmt.Sprintf("%T", e.Object))
		return false
	}

	hasAnnotation := hasPortForwardAnnotation(svc)

	// Enhanced logging for creation filtering decisions
	logger := ctrllog.Log.WithName("predicate-create").WithValues(
		"namespace", svc.Namespace,
		"name", svc.Name,
		"has_annotation", hasAnnotation,
		"finalizers", svc.Finalizers,
	)

	if !hasAnnotation {
		logger.Info("Create event filtered: service does not have port forwarding annotation")
		return false
	}

	logger.Info("Create event accepted: service has port forwarding annotation")
	return true
}

func (ServiceChangePredicate) Delete(e event.DeleteEvent) bool {
	// CRITICAL DIAGNOSTIC: Log ALL Delete events at entry to see if they reach us
	ctrllog.Log.Info("🔍 DELETE EVENT RECEIVED - DIAGNOSTIC MODE",
		"event_type", "DELETE",
		"object_type", fmt.Sprintf("%T", e.Object),
		"namespace", e.Object.GetNamespace(),
		"name", e.Object.GetName(),
		"deletion_timestamp", e.Object.GetDeletionTimestamp())

	svc, ok := e.Object.(*corev1.Service)
	if !ok {
		ctrllog.Log.Info("❌ Delete event object is not Service type",
			"object_type", fmt.Sprintf("%T", e.Object))
		return false
	}

	hasFinalizer := controllerutil.ContainsFinalizer(svc, config.FinalizerLabel)
	hasAnnotation := hasPortForwardAnnotation(svc)

	// Enhanced logging for deletion filtering decisions
	logger := ctrllog.Log.WithName("predicate-delete").WithValues(
		"namespace", svc.Namespace,
		"name", svc.Name,
		"has_finalizer", hasFinalizer,
		"has_annotation", hasAnnotation,
		"deletion_timestamp", svc.DeletionTimestamp,
		"finalizers", svc.Finalizers,
	)

	// STRATEGY: Accept ANY service that could have been managed
	// This prevents the race condition where service gets deleted before reconciliation can process finalizer
	logger.Info("🔍 DELETE EVENT DECISION ANALYSIS",
		"namespace", svc.Namespace,
		"name", svc.Name,
		"has_finalizer", hasFinalizer,
		"has_annotation", hasAnnotation,
		"deletion_timestamp", svc.DeletionTimestamp)

	// PRIMARY PATH: Service currently has our finalizer - highest priority
	if hasFinalizer {
		logger.Info("✅ DELETE EVENT ACCEPTED - service has our finalizer (PRIMARY PATH)",
			"namespace", svc.Namespace,
			"name", svc.Name,
			"deletion_timestamp", svc.DeletionTimestamp)
		return true
	}

	// SECONDARY PATH: Service ever had port forwarding annotation - orphaned cleanup
	// This catches services that lost finalizer during deletion race conditions
	if hasAnnotation {
		logger.Info("✅ DELETE EVENT ACCEPTED - service had port forwarding (ORPHANED CLEANUP)",
			"namespace", svc.Namespace,
			"name", svc.Name,
			"deletion_timestamp", svc.DeletionTimestamp)
		return true
	}

	// FILTER OUT: Service never had port forwarding - not our responsibility
	logger.Info("❌ DELETE EVENT FILTERED - service never had port forwarding",
		"namespace", svc.Namespace,
		"name", svc.Name,
		"deletion_timestamp", svc.DeletionTimestamp)
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
