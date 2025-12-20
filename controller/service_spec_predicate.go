package controller

import (
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// ServiceSpecChangedPredicate triggers reconciliation when service spec changes
type ServiceSpecChangedPredicate struct{}

func (ServiceSpecChangedPredicate) Update(e event.UpdateEvent) bool {
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

	// Compare service specs (excluding irrelevant fields)
	return !serviceSpecsEqual(oldSvc, newSvc)
}

func (ServiceSpecChangedPredicate) Create(e event.CreateEvent) bool {
	return hasPortForwardAnnotation(e.Object.(*corev1.Service))
}

func (ServiceSpecChangedPredicate) Delete(e event.DeleteEvent) bool {
	return hasPortForwardAnnotation(e.Object.(*corev1.Service))
}

func (ServiceSpecChangedPredicate) Generic(e event.GenericEvent) bool {
	return false
}

// serviceSpecsEqual compares service specs excluding status and metadata
func serviceSpecsEqual(oldSvc, newSvc *corev1.Service) bool {
	return reflect.DeepEqual(oldSvc.Spec, newSvc.Spec)
}
