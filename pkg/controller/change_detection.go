package controller

import (
	// "encoding/json"
	"fmt"
	"reflect"
	"strings"

	"unifi-port-forwarder/pkg/config"
	"unifi-port-forwarder/pkg/helpers"
	"unifi-port-forwarder/pkg/routers"

	"github.com/filipowm/go-unifi/unifi"
	corev1 "k8s.io/api/core/v1"
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

	DeletionChanged bool `json:"deletion_changed"`

	IsInitialSync    bool     `json:"is_initial_sync,omitempty"`
	ServiceKey       string   `json:"service_key"`
	ServiceNamespace string   `json:"-"`                            // Not serialized, derived from ServiceKey
	ServiceName      string   `json:"-"`                            // Not serialized, derived from ServiceKey
	PortForwardRules []string `json:"port_forward_rules,omitempty"` // Final rules created for this service
}

// ChangeContextSerializable is what gets stored in annotations (without redundant fields)
type ChangeContextSerializable struct {
	IPChanged         bool               `json:"ip_changed"`
	OldIP             string             `json:"old_ip,omitempty"`
	NewIP             string             `json:"new_ip,omitempty"`
	AnnotationChanged bool               `json:"annotation_changed"`
	OldAnnotation     string             `json:"old_annotation,omitempty"`
	NewAnnotation     string             `json:"new_annotation,omitempty"`
	SpecChanged       bool               `json:"spec_changed"`
	DeletionChanged   bool               `json:"deletion_changed"`
	IsInitialSync     bool               `json:"is_initial_sync,omitempty"`
	PortChanges       []PortChangeDetail `json:"port_changes,omitempty"`
	ServiceKey        string             `json:"service_key"`
	PortForwardRules  []string           `json:"port_forward_rules,omitempty"` // Final rules created for this service
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
	// Don't consider changes during initial sync
	if c.IsInitialSync {
		fmt.Println("triggered", c.IsInitialSync)
		return false
	}

	return c.IPChanged || c.AnnotationChanged || c.SpecChanged || c.DeletionChanged
}

// analyzeChanges performs granular analysis of what changed between old and new service
func analyzeChanges(oldSvc, newSvc *corev1.Service) *ChangeContext {
	context := &ChangeContext{
		ServiceKey:       fmt.Sprintf("%s/%s", newSvc.Namespace, newSvc.Name),
		ServiceNamespace: newSvc.Namespace,
		ServiceName:      newSvc.Name,
	}

	oldDeletionTimestamp := oldSvc.GetDeletionTimestamp()
	newDeletionTimestamp := newSvc.GetDeletionTimestamp()

	if oldDeletionTimestamp.IsZero() && !newDeletionTimestamp.IsZero() {
		context.DeletionChanged = true
		return context // Early return - deletion is most critical
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
		oldPortAnn := oldAnn[config.FilterAnnotation]
		newPortAnn := newAnn[config.FilterAnnotation]
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

// serializeChangeContext converts ChangeContext to multi-line formatted JSON string for annotation storage
// func serializeChangeContext(context *ChangeContext) (string, error) {
// 	// Create serializable version (without redundant fields)
// 	serializable := &ChangeContextSerializable{
// 		IPChanged:         context.IPChanged,
// 		OldIP:             context.OldIP,
// 		NewIP:             context.NewIP,
// 		AnnotationChanged: context.AnnotationChanged,
// 		OldAnnotation:     context.OldAnnotation,
// 		NewAnnotation:     context.NewAnnotation,
// 		SpecChanged:       context.SpecChanged,
// 		DeletionChanged:   context.DeletionChanged,
// 		PortChanges:       context.PortChanges,
// 		ServiceKey:        context.ServiceKey,
// 		PortForwardRules:  context.PortForwardRules,
// 	}

// 	// Marshal to JSON with proper formatting for block scalar
// 	jsonBytes, err := json.MarshalIndent(serializable, "", "  ")
// 	if err != nil {
// 		return "", fmt.Errorf("failed to marshal change context: %w", err)
// 	}

// 	return string(jsonBytes), nil
// }

// collectRulesForService extracts rule names from port configurations
func collectRulesForService(configs []routers.PortConfig) []string {
	var rules []string
	for _, config := range configs {
		rules = append(rules, config.Name) // Already in "namespace/service:port" format
	}
	return rules
}

// parseServiceKey extracts namespace and name from a service key (format: "namespace/name")
func parseServiceKey(serviceKey string) (namespace, name string) {
	parts := strings.SplitN(serviceKey, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	// Fallback if format is unexpected
	return serviceKey, ""
}

// SerializeChangeContextForTest is a test helper function to expose serialization
// func SerializeChangeContextForTest(context *ChangeContext) (string, error) {
// 	return serializeChangeContext(context)
// }

// ExtractChangeContextForTest is a test helper function to expose extraction with fallback
// func ExtractChangeContextForTest(contextJSON, fallbackNamespace, fallbackName string) (*ChangeContext, error) {
// 	if contextJSON == "" {
// 		return &ChangeContext{
// 			ServiceKey:       fmt.Sprintf("%s/%s", fallbackNamespace, fallbackName),
// 			ServiceNamespace: fallbackNamespace,
// 			ServiceName:      fallbackName,
// 		}, nil
// 	}

// 	// Try to unmarshal as new format first (without redundant fields)
// 	var serializable ChangeContextSerializable
// 	if err := json.Unmarshal([]byte(contextJSON), &serializable); err == nil {
// 		// Successfully parsed new format, convert to full ChangeContext
// 		namespace, name := parseServiceKey(serializable.ServiceKey)
// 		return &ChangeContext{
// 			IPChanged:         serializable.IPChanged,
// 			OldIP:             serializable.OldIP,
// 			NewIP:             serializable.NewIP,
// 			AnnotationChanged: serializable.AnnotationChanged,
// 			OldAnnotation:     serializable.OldAnnotation,
// 			NewAnnotation:     serializable.NewAnnotation,
// 			SpecChanged:       serializable.SpecChanged,
// 			DeletionChanged:   serializable.DeletionChanged,
// 			PortChanges:       serializable.PortChanges,
// 			ServiceKey:        serializable.ServiceKey,
// 			ServiceNamespace:  namespace,
// 			ServiceName:       name,
// 		}, nil
// 	}

// 	// Fallback: try to unmarshal as old format (with redundant fields)
// 	var context ChangeContext
// 	if err := json.Unmarshal([]byte(contextJSON), &context); err != nil {
// 		return nil, fmt.Errorf("failed to deserialize change context: %w", err)
// 	}

// 	// Ensure ServiceNamespace and ServiceName are populated
// 	if context.ServiceNamespace == "" || context.ServiceName == "" {
// 		namespace, name := parseServiceKey(context.ServiceKey)
// 		context.ServiceNamespace = namespace
// 		context.ServiceName = name
// 	}

// 	return &context, nil
// }

// compareIPsWithRouterState compares desired IP against actual router state to detect real IP changes
// This prevents false positives when service status is empty or inconsistent
func compareIPsWithRouterState(desiredIP string, currentRules []*unifi.PortForward) (ipChanged bool, oldIP, newIP string) {
	// If desired IP is empty, we can't determine if there's a real change
	if desiredIP == "" {
		return false, "", ""
	}

	// If no current rules exist, this is a new service (no IP change to detect)
	if len(currentRules) == 0 {
		return false, "", ""
	}

	// Check if any existing rule has a different IP than desired
	for _, rule := range currentRules {
		if rule.Fwd != desiredIP {
			// Real IP change detected - router has different IP than desired
			return true, rule.Fwd, desiredIP
		}
	}

	// All existing rules have the correct IP - no change needed
	return false, "", ""
}

// logServiceVsRouterStateDifferences logs when service status differs from router state
// This helps debug issues where service LoadBalancer IP is empty/inconsistent but router has correct IP
func logServiceVsRouterStateDifferences(serviceIP string, currentRules []*unifi.PortForward, serviceName, serviceNamespace string) {
	if len(currentRules) == 0 {
		return // No rules to compare against
	}

	// Get the IP from the first router rule for comparison
	routerIP := currentRules[0].Fwd

	// Check for differences between service status and router state
	if serviceIP != routerIP {
		// This indicates a potential issue where service status is not synced with router state
		fmt.Printf("SERVICE_vs_ROUTER_IP_MISMATCH: service=%s/%s, service_ip=%s, router_ip=%s\n",
			serviceNamespace, serviceName, serviceIP, routerIP)
	}
}
