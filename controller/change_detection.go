package controller

import (
	"encoding/json"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"kube-router-port-forward/helpers"
)

// ChangeContext captures what changed and how
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

// PortChangeDetail describes specific port changes
type PortChangeDetail struct {
	ChangeType   string              `json:"change_type"` // "added", "removed", "modified"
	OldPort      *corev1.ServicePort `json:"old_port,omitempty"`
	NewPort      *corev1.ServicePort `json:"new_port,omitempty"`
	ExternalPort int                 `json:"external_port,omitempty"`
}

// HasRelevantChanges returns true if any relevant changes occurred
func (c *ChangeContext) HasRelevantChanges() bool {
	return c.IPChanged || c.AnnotationChanged || c.SpecChanged
}

// ChangeContextAnnotationKey is the key used to store change context in service annotations
const ChangeContextAnnotationKey = "kube-port-forward-controller/change-context"

// analyzeChanges performs granular analysis of what changed between old and new service
func analyzeChanges(oldSvc, newSvc *corev1.Service) *ChangeContext {
	context := &ChangeContext{
		ServiceKey:       fmt.Sprintf("%s/%s", newSvc.Namespace, newSvc.Name),
		ServiceNamespace: newSvc.Namespace,
		ServiceName:      newSvc.Name,
	}

	// IP changes
	oldIP := helpers.GetLBIP(oldSvc)
	newIP := helpers.GetLBIP(newSvc)
	if oldIP != newIP {
		context.IPChanged = true
		context.OldIP = oldIP
		context.NewIP = newIP
	}

	// Annotation changes
	oldAnn := oldSvc.GetAnnotations()
	newAnn := newSvc.GetAnnotations()
	if oldAnn != nil && newAnn != nil {
		oldPortAnn := oldAnn["kube-port-forward-controller/ports"]
		newPortAnn := newAnn["kube-port-forward-controller/ports"]
		if oldPortAnn != newPortAnn {
			context.AnnotationChanged = true
			context.OldAnnotation = oldPortAnn
			context.NewAnnotation = newPortAnn
		}
	}

	// Port spec changes - detect changes in service port specifications
	oldPorts := oldSvc.Spec.Ports
	newPorts := newSvc.Spec.Ports
	if !reflect.DeepEqual(oldPorts, newPorts) {
		context.SpecChanged = true
		context.PortChanges = analyzePortChanges(oldPorts, newPorts)
	}

	return context
}

// analyzePortChanges performs detailed analysis of port spec changes
func analyzePortChanges(oldPorts, newPorts []corev1.ServicePort) []PortChangeDetail {
	var changes []PortChangeDetail

	// Create maps for comparison - use name+protocol as key to detect port number changes
	// This allows detection when port numbers change but name/protocol remain the same
	oldPortMap := make(map[string]corev1.ServicePort)
	newPortMap := make(map[string]corev1.ServicePort)

	for _, port := range oldPorts {
		key := portKeyByName(port)
		oldPortMap[key] = port
	}

	for _, port := range newPorts {
		key := portKeyByName(port)
		newPortMap[key] = port
	}

	// Find removed ports (by name+protocol)
	for key, oldPort := range oldPortMap {
		if _, exists := newPortMap[key]; !exists {
			changes = append(changes, PortChangeDetail{
				ChangeType: "removed",
				OldPort:    &oldPort,
			})
		}
	}

	// Find added ports (by name+protocol)
	for key, newPort := range newPortMap {
		if _, exists := oldPortMap[key]; !exists {
			changes = append(changes, PortChangeDetail{
				ChangeType: "added",
				NewPort:    &newPort,
			})
		}
	}

	// Find modified ports (by name+protocol)
	for key, oldPort := range oldPortMap {
		if newPort, exists := newPortMap[key]; exists {
			if !reflect.DeepEqual(oldPort, newPort) {
				changes = append(changes, PortChangeDetail{
					ChangeType: "modified",
					OldPort:    &oldPort,
					NewPort:    &newPort,
				})
			}
		}
	}

	return changes
}

// portKeyByName creates a key for a service port using only name and protocol (to detect port number changes)
// This excludes the port number to detect when port numbers change across service updates
func portKeyByName(port corev1.ServicePort) string {
	return fmt.Sprintf("%s-%s", port.Name, port.Protocol)
}

// serializeChangeContext converts ChangeContext to JSON string for annotation storage
func serializeChangeContext(context *ChangeContext) (string, error) {
	data, err := json.Marshal(context)
	if err != nil {
		return "", fmt.Errorf("failed to serialize change context: %w", err)
	}
	return string(data), nil
}

// extractChangeContext extracts ChangeContext from service annotation
func extractChangeContext(service *corev1.Service) (*ChangeContext, error) {
	ann := service.GetAnnotations()
	if ann == nil {
		return &ChangeContext{
			ServiceKey:       fmt.Sprintf("%s/%s", service.Namespace, service.Name),
			ServiceNamespace: service.Namespace,
			ServiceName:      service.Name,
		}, nil
	}

	contextJSON := ann[ChangeContextAnnotationKey]
	if contextJSON == "" {
		return &ChangeContext{
			ServiceKey:       fmt.Sprintf("%s/%s", service.Namespace, service.Name),
			ServiceNamespace: service.Namespace,
			ServiceName:      service.Name,
		}, nil
	}

	var context ChangeContext
	if err := json.Unmarshal([]byte(contextJSON), &context); err != nil {
		return nil, fmt.Errorf("failed to deserialize change context: %w", err)
	}

	return &context, nil
}
