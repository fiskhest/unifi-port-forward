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
	EventPortForwardCreated = "PortForwardCreated"
	EventPortForwardUpdated = "PortForwardUpdated"
	EventPortForwardDeleted = "PortForwardDeleted"
	EventPortForwardFailed  = "PortForwardFailed"
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

func (ep *EventPublisher) PublishPortForwardCreatedEvent(ctx context.Context, service *corev1.Service, changeContext *ChangeContext, portMapping, externalIP, internalIP string, externalPort int, protocol, reason string) {
	logger := ctrllog.FromContext(ctx).WithValues("service", service.Name, "namespace", service.Namespace)
	logger.V(1).Info("DEBUG: PublishPortForwardCreatedEvent called for service", "namespace", service.Namespace, "service", service.Name)

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

	message := fmt.Sprintf("Created port forward rule: %s service: %s", eventData.Message, service.Name)

	logger.V(1).Info("DEBUG: About to call createEvent")
	if err := ep.createEvent(ctx, service, EventPortForwardCreated, message, eventData, changeContext); err != nil {
		logger.V(1).Info("DEBUG: createEvent returned error", "error", err)
		logger.Error(err, "Failed to publish PortForwardCreated event")
	} else {
		logger.V(1).Info("DEBUG: createEvent succeeded")
		logger.V(1).Info("Published PortForwardCreated event", "port_mapping", portMapping, "external_port", externalPort)
	}
}

func (ep *EventPublisher) PublishPortForwardUpdatedEvent(ctx context.Context, service *corev1.Service, changeContext *ChangeContext, portMapping, externalIP, internalIP string, externalPort int, protocol, reason string) {
	logger := ctrllog.FromContext(ctx).WithValues("service", service.Name, "namespace", service.Namespace)

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

	message := fmt.Sprintf("Updated port forward rule: %s service: %s", eventData.Message, service.Name)

	if err := ep.createEvent(ctx, service, EventPortForwardUpdated, message, eventData, changeContext); err != nil {
		logger.Error(err, "Failed to publish PortForwardUpdated event")
	} else {
		logger.V(1).Info("Published PortForwardUpdated event", "port_mapping", portMapping, "external_port", externalPort)
	}
}

func (ep *EventPublisher) PublishPortForwardDeletedEvent(ctx context.Context, service *corev1.Service, changeContext *ChangeContext, portMapping string, externalPort int, protocol, reason string) {
	logger := ctrllog.FromContext(ctx).WithValues("service", service.Name, "namespace", service.Namespace)

	eventData := &PortForwardEventData{
		ServiceKey:       fmt.Sprintf("%s/%s", service.Namespace, service.Name),
		ServiceNamespace: service.Namespace,
		ServiceName:      service.Name,
		PortMapping:      portMapping,
		ExternalPort:     externalPort,
		Protocol:         protocol,
		Reason:           reason,
		Message:          fmt.Sprintf("%s (port %d, %s)", portMapping, externalPort, protocol),
	}

	message := fmt.Sprintf("Deleted port forward rule: %s service: %s", eventData.Message, service.Name)

	if err := ep.createEvent(ctx, service, EventPortForwardDeleted, message, eventData, changeContext); err != nil {
		logger.Error(err, "Failed to publish PortForwardDeleted event")
	} else {
		logger.V(1).Info("Published PortForwardDeleted event", "port_mapping", portMapping, "external_port", externalPort)
	}
}

func (ep *EventPublisher) PublishPortForwardFailedEvent(ctx context.Context, service *corev1.Service, changeContext *ChangeContext, portMapping, externalIP, internalIP string, externalPort int, protocol, reason string, err error) {
	logger := ctrllog.FromContext(ctx).WithValues("service", service.Name, "namespace", service.Namespace)

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
		Message:          fmt.Sprintf("%s", reason),
		Error:            errorMsg,
	}

	message := fmt.Sprintf("Failed to create port forward rule: %s service: %s - %s", portMapping, service.Name, reason)
	if err != nil {
		message += fmt.Sprintf(" - Error: %s", err.Error())
	}

	if createErr := ep.createEvent(ctx, service, EventPortForwardFailed, message, eventData, changeContext); createErr != nil {
		logger.Error(createErr, "Failed to publish PortForwardFailed event")
	} else {
		logger.V(1).Info("Published PortForwardFailed event", "port_mapping", portMapping, "reason", reason)
	}
}

func (ep *EventPublisher) PublishIPChangedEvent(ctx context.Context, service *corev1.Service, changeContext *ChangeContext, oldIP, newIP string) {
	logger := ctrllog.FromContext(ctx).WithValues("service", service.Name, "namespace", service.Namespace)

	eventData := &PortForwardEventData{
		ServiceKey:       fmt.Sprintf("%s/%s", service.Namespace, service.Name),
		ServiceNamespace: service.Namespace,
		ServiceName:      service.Name,
		ExternalIP:       newIP,
		Reason:           "IPChanged",
		Message:          fmt.Sprintf("%s -> %s", oldIP, newIP),
	}

	message := fmt.Sprintf("Changed LoadBalancer IP: %s service: %s", eventData.Message, service.Name)

	if err := ep.createEvent(ctx, service, "IPChanged", message, eventData, changeContext); err != nil {
		logger.Error(err, "Failed to publish IPChanged event")
	} else {
		logger.V(1).Info("Published IPChanged event", "old_ip", oldIP, "new_ip", newIP)
	}
}

func (ep *EventPublisher) createEvent(ctx context.Context, service *corev1.Service, eventType, message string, eventData *PortForwardEventData, changeContext *ChangeContext) error {
	logger := ctrllog.FromContext(ctx)

	annotations := map[string]string{}
	if changeContext != nil {
		if contextJSON, err := json.Marshal(changeContext); err == nil {
			annotations["kube-port-forward-controller/change-context"] = string(contextJSON)
		}
	}

	if eventDataJSON, err := json.Marshal(eventData); err == nil {
		annotations["kube-port-forward-controller/event-data"] = string(eventDataJSON)
	}

	logger.V(1).Info("DEBUG: createEvent called", "recorder_nil", ep.recorder == nil, "annotations_count", len(annotations))

	if ep.recorder != nil {
		eventTypeValue := "Normal"
		if eventType == EventPortForwardFailed {
			eventTypeValue = "Warning"
		}

		// Use annotated event to include metadata
		if len(annotations) > 0 {
			logger.V(1).Info("DEBUG: Calling AnnotatedEventf")
			ep.recorder.AnnotatedEventf(service, annotations, eventTypeValue, eventType, "%s", message)
		} else {
			logger.V(1).Info("DEBUG: Calling Eventf")
			ep.recorder.Eventf(service, eventTypeValue, eventType, "%s", message)
		}
	} else {
		logger.Info("Event recorder not available, skipping event publication")
	}

	logger.V(1).Info("Event published", "event_type", eventType, "service", service.Name, "message", message)

	return nil
}
