package controller

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	EventPortForwardCreated                     = "PortForwardCreated"
	EventPortForwardUpdated                     = "PortForwardUpdated"
	EventPortForwardDeleted                     = "PortForwardDeleted"
	EventPortForwardFailed                      = "PortForwardFailed"
	EventPortForwardTakenOwnership              = "PortForwardTakenOwnership"
	EventDriftDetected                          = "DriftDetected"
	EventDriftCorrected                         = "DriftCorrected"
	EventServicePeriodicReconciliationCompleted = "ServicePeriodicReconciliationCompleted"
)

type PortForwardEventData struct {
	ServiceKey       string `json:"service_key"`
	ServiceNamespace string `json:"service_namespace"`
	ServiceName      string `json:"service_name"`
	PortMapping      string `json:"port_mapping"`
	ExternalIP       string `json:"external_ip"`
	InternalIP       string `json:"internal_ip"`
	ExternalPort     int    `json:"external_port"`
	Protocol         string `json:"protocol"`
	Reason           string `json:"reason"`
	Message          string `json:"message"`
	Error            string `json:"error,omitempty"`
}

type EventPublisher struct {
	client   client.Client
	recorder record.EventRecorder
	scheme   *runtime.Scheme
}

func NewEventPublisher(client client.Client, recorder record.EventRecorder, scheme *runtime.Scheme) *EventPublisher {
	return &EventPublisher{
		client:   client,
		recorder: recorder,
		scheme:   scheme,
	}
}

func (ep *EventPublisher) PublishPortForwardCreatedEvent(ctx context.Context, service *corev1.Service, portName, portMapping, externalIP, internalIP string, internalPort, externalPort int, protocol, reason string) {
	logger := ctrllog.FromContext(ctx)

	eventData := &PortForwardEventData{
		ServiceKey:       fmt.Sprintf("%s/%s", service.Namespace, service.Name),
		ServiceNamespace: service.Namespace,
		ServiceName:      service.Name,
		PortMapping:      portMapping,
		ExternalIP:       externalIP,
		InternalIP:       internalIP,
		ExternalPort:     externalPort,
		Protocol:         protocol,
		Reason:           reason,
		Message:          fmt.Sprintf("%s(%s) -> %s:%d", portName, protocol, internalIP, externalPort),
	}

	message := fmt.Sprintf("Created port forward rule '%s/%s:%s': %d(%s) -> %s:%d", service.Namespace, service.Name, portName, externalPort, protocol, internalIP, internalPort)

	if err := ep.createEvent(ctx, service, EventPortForwardCreated, message, eventData); err != nil {
		logger.Error(err, "Failed to publish PortForwardCreated event")
		return
	}

	logger.V(1).Info("Published PortForwardCreated event", "port_mapping", portMapping, "external_port", externalPort)
}

func (ep *EventPublisher) PublishPortForwardUpdatedEvent(ctx context.Context, service *corev1.Service, portName, portMapping, externalIP, internalIP string, externalPort int, protocol, reason string) {
	logger := ctrllog.FromContext(ctx)

	eventData := &PortForwardEventData{
		ServiceKey:       fmt.Sprintf("%s/%s", service.Namespace, service.Name),
		ServiceNamespace: service.Namespace,
		ServiceName:      service.Name,
		PortMapping:      portMapping,
		ExternalIP:       externalIP,
		InternalIP:       internalIP,
		ExternalPort:     externalPort,
		Protocol:         protocol,
		Reason:           reason,
		Message:          fmt.Sprintf("%s -> %s:%d (%s)", portMapping, internalIP, externalPort, protocol),
	}

	message := fmt.Sprintf("Updated port forward rule '%s/%s:%s'", service.Namespace, service.Name, portName)

	if err := ep.createEvent(ctx, service, EventPortForwardUpdated, message, eventData); err != nil {
		logger.Error(err, "Failed to publish PortForwardUpdated event")
		return
	}

	logger.V(1).Info("Published PortForwardUpdated event", "port_mapping", portMapping, "external_port", externalPort)
}

func (ep *EventPublisher) PublishPortForwardDeletedEvent(ctx context.Context, service *corev1.Service, portName, portMapping string, externalPort int, protocol, reason string) {
	logger := ctrllog.FromContext(ctx)

	eventData := &PortForwardEventData{
		ServiceKey:       fmt.Sprintf("%s/%s", service.Namespace, service.Name),
		ServiceNamespace: service.Namespace,
		ServiceName:      service.Name,
		PortMapping:      portMapping,
		ExternalPort:     externalPort,
		Protocol:         protocol,
		Reason:           reason,
		Message:          fmt.Sprintf("%s(%s) (port %d)", portName, protocol, externalPort),
	}

	message := fmt.Sprintf("Deleted port forward rule '%s/%s:%s': %d(%s)", service.Namespace, service.Name, portName, externalPort, protocol)

	if err := ep.createEvent(ctx, service, EventPortForwardDeleted, message, eventData); err != nil {
		logger.Error(err, "Failed to publish PortForwardDeleted event")
		return
	}

	logger.V(1).Info("Published PortForwardDeleted event", "port_mapping", portMapping, "external_port", externalPort)
}

func (ep *EventPublisher) PublishPortForwardFailedEvent(ctx context.Context, service *corev1.Service, portMapping, externalIP, internalIP string, externalPort int, protocol, reason string, err error) {
	logger := ctrllog.FromContext(ctx)

	errorMsg := ""
	if err != nil {
		errorMsg = err.Error()
	}

	eventData := &PortForwardEventData{
		ServiceKey:       fmt.Sprintf("%s/%s", service.Namespace, service.Name),
		ServiceNamespace: service.Namespace,
		ServiceName:      service.Name,
		PortMapping:      portMapping,
		ExternalIP:       externalIP,
		InternalIP:       internalIP,
		ExternalPort:     externalPort,
		Protocol:         protocol,
		Reason:           reason,
		Message:          reason,
		Error:            errorMsg,
	}

	message := fmt.Sprintf("Failed to create port forward rule: %s service: %s - %s", portMapping, service.Name, reason)
	if err != nil {
		message += fmt.Sprintf(" - Error: %s", err.Error())
	}

	if createErr := ep.createEvent(ctx, service, EventPortForwardFailed, message, eventData); createErr != nil {
		logger.Error(createErr, "Failed to publish PortForwardFailed event")
		return
	}

	logger.V(1).Info("Published PortForwardFailed event", "port_mapping", portMapping, "reason", reason)
}

func (ep *EventPublisher) createEvent(ctx context.Context, service *corev1.Service, eventType, message string, eventData *PortForwardEventData) error {
	logger := ctrllog.FromContext(ctx)

	annotations := map[string]string{}
	if eventDataJSON, err := json.Marshal(eventData); err == nil {
		annotations["unifi-port-forward.fiskhe.st/event-data"] = string(eventDataJSON)
	}

	logger.V(1).Info("createEvent called", "recorder_nil", ep.recorder == nil, "annotations_count", len(annotations))

	if ep.recorder != nil {
		eventTypeValue := "Normal"
		if eventType == EventPortForwardFailed {
			eventTypeValue = "Warning"
		}

		// Use annotated event to include metadata
		if len(annotations) > 0 {
			ep.recorder.AnnotatedEventf(service, annotations, eventTypeValue, eventType, "%s", message)
		} else {
			ep.recorder.Eventf(service, eventTypeValue, eventType, "%s", message)
		}
	} else {
		logger.Info("Event recorder not available, skipping event publication")
	}

	return nil
}

func (ep *EventPublisher) PublishPortForwardTakenOwnershipEvent(ctx context.Context, service *corev1.Service, oldRuleName, newRuleName string, externalPort int, protocol string) {
	logger := ctrllog.FromContext(ctx)

	eventData := &PortForwardEventData{
		ServiceKey:       fmt.Sprintf("%s/%s", service.Namespace, service.Name),
		ServiceNamespace: service.Namespace,
		ServiceName:      service.Name,
		PortMapping:      fmt.Sprintf("%d:%d", externalPort, externalPort),
		ExternalPort:     externalPort,
		Protocol:         protocol,
		Reason:           "PortConflictTakeOwnership",
		Message:          fmt.Sprintf("Renamed manual rule '%s' to '%s' (port %d, %s)", oldRuleName, newRuleName, externalPort, protocol),
	}

	message := fmt.Sprintf("Took ownership of existing port forward rule: %s service: %s - renamed from '%s' to '%s'",
		eventData.Message, service.Name, oldRuleName, newRuleName)

	if err := ep.createEvent(ctx, service, EventPortForwardTakenOwnership, message, eventData); err != nil {
		logger.Error(err, "Failed to publish PortForwardTakenOwnership event")
	} else {
		logger.Info("Published PortForwardTakenOwnership event",
			"old_rule", oldRuleName, "new_rule", newRuleName, "external_port", externalPort)
	}
}

// PublishDriftDetectedEvent publishes an event when drift is detected for a service
func (ep *EventPublisher) PublishDriftDetectedEvent(ctx context.Context, service *corev1.Service, analysis *DriftAnalysis) {
	logger := ctrllog.FromContext(ctx)

	eventData := &PortForwardEventData{
		ServiceKey:       fmt.Sprintf("%s/%s", service.Namespace, service.Name),
		ServiceNamespace: service.Namespace,
		ServiceName:      service.Name,
		Reason:           "DriftDetected",
		Message: fmt.Sprintf("Drift detected - missing: %d, wrong: %d, extra: %d",
			len(analysis.MissingRules), len(analysis.WrongRules), len(analysis.ExtraRules)),
	}

	message := fmt.Sprintf("Drift detected for service %s/%s - corrective actions will be taken", service.Namespace, service.Name)

	if err := ep.createEvent(ctx, service, EventDriftDetected, message, eventData); err != nil {
		logger.Error(err, "Failed to publish DriftDetected event")
	}
}

// PublishDriftCorrectedEvent publishes an event when drift is successfully corrected for a service
func (ep *EventPublisher) PublishDriftCorrectedEvent(ctx context.Context, service *corev1.Service, analysis *DriftAnalysis) {
	logger := ctrllog.FromContext(ctx)

	eventData := &PortForwardEventData{
		ServiceKey:       fmt.Sprintf("%s/%s", service.Namespace, service.Name),
		ServiceNamespace: service.Namespace,
		ServiceName:      service.Name,
		Reason:           "DriftCorrected",
		Message: fmt.Sprintf("Drift corrected - missing: %d, wrong: %d, extra: %d",
			len(analysis.MissingRules), len(analysis.WrongRules), len(analysis.ExtraRules)),
	}

	message := fmt.Sprintf("Drift corrected for service %s/%s", service.Namespace, service.Name)

	if err := ep.createEvent(ctx, service, EventDriftCorrected, message, eventData); err != nil {
		logger.Error(err, "Failed to publish DriftCorrected event")
	}
}

// PublishDriftCorrectionFailedEvent publishes an event when drift correction fails for a service
func (ep *EventPublisher) PublishDriftCorrectionFailedEvent(ctx context.Context, service *corev1.Service, analysis *DriftAnalysis, err error) {
	logger := ctrllog.FromContext(ctx)

	eventData := &PortForwardEventData{
		ServiceKey:       fmt.Sprintf("%s/%s", service.Namespace, service.Name),
		ServiceNamespace: service.Namespace,
		ServiceName:      service.Name,
		Reason:           "DriftCorrectionFailed",
		Message:          "Failed to correct drift",
		Error:            err.Error(),
	}

	message := fmt.Sprintf("Failed to correct drift for service %s/%s - %s", service.Namespace, service.Name, err.Error())

	if createErr := ep.createEvent(ctx, service, EventPortForwardFailed, message, eventData); createErr != nil {
		logger.Error(createErr, "Failed to publish DriftCorrectionFailed event")
	} else {
		logger.Info("Published DriftCorrectionFailed event", "service", service.Name, "error", err.Error())
	}
}

// PublishServicePeriodicReconciliationCompletedEvent publishes an event when periodic reconciliation completes
// for a specific service that had drift. This is only called when drift was detected and handled.
func (ep *EventPublisher) PublishServicePeriodicReconciliationCompletedEvent(ctx context.Context, service *corev1.Service, hasDrift bool, correctedRules int, failedOperations int) {
	logger := ctrllog.FromContext(ctx)

	eventData := &PortForwardEventData{
		ServiceKey:       fmt.Sprintf("%s/%s", service.Namespace, service.Name),
		ServiceNamespace: service.Namespace,
		ServiceName:      service.Name,
		Reason:           "ServicePeriodicReconciliationCompleted",
		Message: fmt.Sprintf("Reconciliation completed - drift: %t, corrected: %d, failed: %d",
			hasDrift, correctedRules, failedOperations),
	}

	message := fmt.Sprintf("Periodic reconciliation completed for service %s/%s - drift detected: %t, rules corrected: %d, failed: %d",
		service.Namespace, service.Name, hasDrift, correctedRules, failedOperations)

	if err := ep.createEvent(ctx, service, EventServicePeriodicReconciliationCompleted, message, eventData); err != nil {
		logger.Error(err, "Failed to publish ServicePeriodicReconciliationCompleted event")
	} else {
		logger.V(1).Info("Published ServicePeriodicReconciliationCompleted event",
			"has_drift", hasDrift,
			"corrected_rules", correctedRules,
			"failed_operations", failedOperations)
	}
}
