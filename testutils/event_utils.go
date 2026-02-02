package testutils

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

type ChangeContext struct {
	IPChanged bool   `json:"ip_changed"`
	OldIP     string `json:"old_ip,omitempty"`
	NewIP     string `json:"new_ip,omitempty"`

	AnnotationChanged bool   `json:"annotation_changed"`
	OldAnnotation     string `json:"old_annotation,omitempty"`
	NewAnnotation     string `json:"new_annotation,omitempty"`

	SpecChanged bool               `json:"spec_changed"`
	PortChanges []PortChangeDetail `json:"port_changes,omitempty"`

	ServiceKey       string `json:"service_key"`
	ServiceNamespace string `json:"service_namespace"`
	ServiceName      string `json:"service_name"`
}

type PortChangeDetail struct {
	ChangeType   string              `json:"change_type"`
	OldPort      *corev1.ServicePort `json:"old_port,omitempty"`
	NewPort      *corev1.ServicePort `json:"new_port,omitempty"`
	ExternalPort int                 `json:"external_port,omitempty"`
}

type FakeEventRecorder struct {
	Events []corev1.Event
	mu     sync.Mutex
}

func NewFakeEventRecorder() *FakeEventRecorder {
	return &FakeEventRecorder{
		Events: make([]corev1.Event, 0),
	}
}

func (f *FakeEventRecorder) Event(object runtime.Object, eventType, reason, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	service, ok := object.(*corev1.Service)
	if !ok {
		return
	}

	event := corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: service.Name + "-",
			Namespace:    service.Namespace,
			Annotations:  make(map[string]string),
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:            "Service",
			Namespace:       service.Namespace,
			Name:            service.Name,
			UID:             service.UID,
			ResourceVersion: service.ResourceVersion,
		},
		Reason:  reason,
		Message: message,
		Source: corev1.EventSource{
			Component: "port-forward-controller",
		},
		Type:          eventType,
		LastTimestamp: metav1.Now(),
	}

	// Store the event
	f.Events = append(f.Events, event)
}

func (f *FakeEventRecorder) Eventf(object runtime.Object, eventType, reason, messageFmt string, args ...interface{}) {
	message := fmt.Sprintf(messageFmt, args...)
	f.Event(object, eventType, reason, message)
}

func (f *FakeEventRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventType, reason, messageFmt string, args ...interface{}) {
	message := fmt.Sprintf(messageFmt, args...)
	f.mu.Lock()
	defer f.mu.Unlock()

	service, ok := object.(*corev1.Service)
	if !ok {
		return
	}

	event := corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: service.Name + "-",
			Namespace:    service.Namespace,
			Annotations:  make(map[string]string),
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:            "Service",
			Namespace:       service.Namespace,
			Name:            service.Name,
			UID:             service.UID,
			ResourceVersion: service.ResourceVersion,
		},
		Reason:  reason,
		Message: message,
		Source: corev1.EventSource{
			Component: "port-forward-controller",
		},
		Type:          eventType,
		LastTimestamp: metav1.Now(),
	}

	// Copy annotations
	for k, v := range annotations {
		event.Annotations[k] = v
		// Note: Removed debug printf - using structured logging would require passing logger context
	}

	f.Events = append(f.Events, event)

}

func (f *FakeEventRecorder) GetEvents() []corev1.Event {
	f.mu.Lock()
	defer f.mu.Unlock()

	events := make([]corev1.Event, len(f.Events))
	copy(events, f.Events)
	return events
}

func (f *FakeEventRecorder) GetEventsForService(serviceName, namespace string) []corev1.Event {
	f.mu.Lock()
	defer f.mu.Unlock()

	var events []corev1.Event
	for _, event := range f.Events {
		if event.InvolvedObject.Name == serviceName && event.InvolvedObject.Namespace == namespace {
			events = append(events, event)
		}
	}
	return events
}

func (f *FakeEventRecorder) GetEventsByReason(reason string) []corev1.Event {
	f.mu.Lock()
	defer f.mu.Unlock()

	var events []corev1.Event
	for _, event := range f.Events {
		if event.Reason == reason {
			events = append(events, event)
		}
	}
	return events
}

func (f *FakeEventRecorder) Clear() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Events = make([]corev1.Event, 0)
}

func (f *FakeEventRecorder) HasEvent(serviceName, namespace, reason, message string) bool {
	events := f.GetEventsForService(serviceName, namespace)
	for _, event := range events {
		if event.Reason == reason && event.Message == message {
			return true
		}
	}
	return false
}

func (f *FakeEventRecorder) HasEventContaining(serviceName, namespace, reason, messageFragment string) bool {
	events := f.GetEventsForService(serviceName, namespace)
	for _, event := range events {
		if event.Reason == reason && strings.Contains(event.Message, messageFragment) {
			return true
		}
	}
	return false
}

type EventTestHelper struct {
	Recorder  *FakeEventRecorder
	Publisher interface{} // Will be set to the actual EventPublisher type
	Client    client.Client
	T         *testing.T
}

func NewEventTestHelper(t *testing.T, client client.Client, scheme *runtime.Scheme) *EventTestHelper {
	recorder := NewFakeEventRecorder()

	return &EventTestHelper{
		Recorder: recorder,
		Client:   client,
		T:        t,
	}
}

func (h *EventTestHelper) SetPublisher(publisher interface{}) {
	h.Publisher = publisher
}

func (h *EventTestHelper) AssertEventPublished(serviceName, namespace, reason, messageContains string) {
	events := h.Recorder.GetEventsForService(serviceName, namespace)

	if len(events) == 0 {
		h.T.Errorf("Expected event for service %s/%s with reason %s, but no events found", namespace, serviceName, reason)
		return
	}

	found := false
	for _, event := range events {
		if event.Reason == reason && strings.Contains(event.Message, messageContains) {
			found = true
			break
		}
	}

	if !found {
		h.T.Errorf("Expected event for service %s/%s with reason %s containing message '%s', but not found in events: %v",
			namespace, serviceName, reason, messageContains, events)
	}
}

func (h *EventTestHelper) AssertEventNotPublished(serviceName, namespace, reason string) {
	events := h.Recorder.GetEventsForService(serviceName, namespace)

	for _, event := range events {
		if event.Reason == reason {
			h.T.Errorf("Expected no event for service %s/%s with reason %s, but found: %s", namespace, serviceName, reason, event.Message)
			return
		}
	}
}

func (h *EventTestHelper) AssertEventCount(serviceName, namespace, reason string, expectedCount int) {
	events := h.Recorder.GetEventsForService(serviceName, namespace)

	count := 0
	for _, event := range events {
		if event.Reason == reason {
			count++
		}
	}

	if count != expectedCount {
		h.T.Errorf("Expected %d events for service %s/%s with reason %s, but found %d", expectedCount, namespace, serviceName, reason, count)
	}
}

func (h *EventTestHelper) ExtractEventData(event corev1.Event) (*PortForwardEventData, error) {
	if event.Annotations == nil {
		return nil, fmt.Errorf("event has no annotations")
	}

	eventDataStr := event.Annotations["unifi-port-forward.fiskhe.st/event-data"]
	if eventDataStr == "" {
		return nil, fmt.Errorf("event has no event-data annotation")
	}

	var eventData PortForwardEventData
	if err := json.Unmarshal([]byte(eventDataStr), &eventData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event data: %w", err)
	}

	return &eventData, nil
}

func (h *EventTestHelper) ClearEvents() {
	h.Recorder.Clear()
}

func (h *EventTestHelper) GetEventCount() int {
	return len(h.Recorder.Events)
}
