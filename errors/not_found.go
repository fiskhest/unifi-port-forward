package errors

import (
	"fmt"
	"strings"
	"time"
)

// NotFoundError is a specialized error for "not found" scenarios with rich context
type NotFoundError struct {
	ResourceType       string                 `json:"resource_type"`
	ResourceID         string                 `json:"resource_id"`
	Operation          string                 `json:"operation"`
	SearchCriteria     map[string]interface{} `json:"search_criteria"`
	AvailableResources []Alternative          `json:"available_resources"`
	Suggestions        []string               `json:"suggestions"`
	Context            map[string]interface{} `json:"context"`
}

// Error implements the error interface
func (e *NotFoundError) Error() string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("%s '%s' not found during %s operation",
		e.ResourceType, e.ResourceID, e.Operation))

	if len(e.SearchCriteria) > 0 {
		builder.WriteString("\nSearch criteria:")
		for k, v := range e.SearchCriteria {
			builder.WriteString(fmt.Sprintf("\n  %s: %v", k, v))
		}
	}

	if len(e.AvailableResources) > 0 {
		builder.WriteString(fmt.Sprintf("\nAvailable %ss (%d total):",
			e.ResourceType, len(e.AvailableResources)))
		for _, r := range e.AvailableResources {
			builder.WriteString(fmt.Sprintf("\n  - %s: %s", r.ID, formatAlternativeMetadata(r.Metadata)))
		}
	}

	if len(e.Suggestions) > 0 {
		builder.WriteString("\nSuggestions:")
		for _, suggestion := range e.Suggestions {
			builder.WriteString(fmt.Sprintf("\n  - %s", suggestion))
		}
	}

	return builder.String()
}

// ToControllerError converts NotFoundError to ControllerError
func (e *NotFoundError) ToControllerError() *ControllerError {
	context := make(map[string]interface{})
	for k, v := range e.Context {
		context[k] = v
	}
	for k, v := range e.SearchCriteria {
		context["search_"+k] = v
	}
	if len(e.AvailableResources) > 0 {
		context["available_alternatives"] = e.AvailableResources
	}
	if len(e.Suggestions) > 0 {
		context["suggestions"] = e.Suggestions
	}

	return &ControllerError{
		Type:      ErrorTypeNotFound,
		Severity:  SeverityPermanent,
		Operation: e.Operation,
		Resource:  fmt.Sprintf("%s/%s", e.ResourceType, e.ResourceID),
		Cause:     fmt.Errorf(e.Error()),
		Context:   context,
		Timestamp: time.Now(),
		Retryable: false,
	}
}

// NewPortNotFoundError creates a specialized port forward not found error
func NewPortNotFoundError(port int, protocol string, serviceKey string, availablePorts []PortForwardAlternative, cause error) *NotFoundError {
	searchCriteria := map[string]interface{}{
		"port":     port,
		"protocol": protocol,
		"service":  serviceKey,
	}

	alternatives := make([]Alternative, len(availablePorts))
	for i, p := range availablePorts {
		alternatives[i] = Alternative{
			ID:   fmt.Sprintf("%d/%s", p.Port, p.Protocol),
			Type: "port_forward",
			Name: p.Name,
			Metadata: map[string]interface{}{
				"port":           p.Port,
				"protocol":       p.Protocol,
				"destination_ip": p.DestinationIP,
				"service_key":    p.ServiceKey,
				"enabled":        p.Enabled,
			},
		}
	}

	suggestions := generatePortSuggestions(port, protocol, availablePorts)

	return &NotFoundError{
		ResourceType:       "port_forward",
		ResourceID:         fmt.Sprintf("%d/%s", port, protocol),
		Operation:          "port_lookup_or_update",
		SearchCriteria:     searchCriteria,
		AvailableResources: alternatives,
		Suggestions:        suggestions,
		Context: map[string]interface{}{
			"service_key": serviceKey,
			"cause":       cause,
		},
	}
}

// NewServiceNotFoundError creates a service not found error
func NewServiceNotFoundError(namespace, name string, cause error) *NotFoundError {
	return &NotFoundError{
		ResourceType: "service",
		ResourceID:   fmt.Sprintf("%s/%s", namespace, name),
		Operation:    "service_reconciliation",
		SearchCriteria: map[string]interface{}{
			"namespace": namespace,
			"name":      name,
		},
		Suggestions: []string{
			"Check if service exists in the cluster",
			"Verify service namespace is correct",
			"Check service deletion status",
		},
		Context: map[string]interface{}{
			"cause": cause,
		},
	}
}

// NewRouterResourceNotFoundError creates a router-specific not found error
func NewRouterResourceNotFoundError(resourceType, resourceID string, searchCriteria map[string]interface{}, availableResources []Alternative) *NotFoundError {
	return &NotFoundError{
		ResourceType:       resourceType,
		ResourceID:         resourceID,
		Operation:          "router_operation",
		SearchCriteria:     searchCriteria,
		AvailableResources: availableResources,
		Suggestions:        []string{"Verify resource exists in UniFi controller", "Check API permissions"},
	}
}

// PortForwardAlternative represents an available port forward rule
type PortForwardAlternative struct {
	Port          int    `json:"port"`
	Protocol      string `json:"protocol"`
	Name          string `json:"name"`
	DestinationIP string `json:"destination_ip"`
	ServiceKey    string `json:"service_key"`
	Enabled       bool   `json:"enabled"`
}

// formatAlternativeMetadata formats metadata for display
func formatAlternativeMetadata(metadata map[string]interface{}) string {
	var parts []string
	if port, ok := metadata["port"]; ok {
		parts = append(parts, fmt.Sprintf("port:%v", port))
	}
	if protocol, ok := metadata["protocol"]; ok {
		parts = append(parts, fmt.Sprintf("proto:%v", protocol))
	}
	if service, ok := metadata["service_key"]; ok {
		parts = append(parts, fmt.Sprintf("service:%v", service))
	}
	if ip, ok := metadata["destination_ip"]; ok {
		parts = append(parts, fmt.Sprintf("dst:%v", ip))
	}
	if len(parts) == 0 {
		return "no metadata"
	}
	return strings.Join(parts, ", ")
}

// generatePortSuggestions creates helpful suggestions based on available ports
func generatePortSuggestions(searchedPort int, searchedProtocol string, availablePorts []PortForwardAlternative) []string {
	var suggestions []string

	// Check if port exists with different protocol
	for _, port := range availablePorts {
		if port.Port == searchedPort && port.Protocol != searchedProtocol {
			suggestions = append(suggestions,
				fmt.Sprintf("Port %d exists with protocol '%s' - try protocol '%s' instead of '%s'",
					searchedPort, port.Protocol, port.Protocol, searchedProtocol))
		}
	}

	// Find similar port numbers
	closestPort := findClosestPort(searchedPort, searchedProtocol, availablePorts)
	if closestPort != 0 && closestPort != searchedPort {
		suggestions = append(suggestions,
			fmt.Sprintf("Port %d is close to searched port %d - check if this is the correct port",
				closestPort, searchedPort))
	}

	// General suggestions
	if len(availablePorts) > 0 {
		suggestions = append(suggestions,
			fmt.Sprintf("Available ports: %s", listAvailablePorts(availablePorts)))
	}

	if len(suggestions) == 0 {
		suggestions = append(suggestions,
			"Port forward rule may need to be created first before updating")
	}

	return suggestions
}

// findClosestPort finds the port number closest to the searched port
func findClosestPort(searchedPort int, protocol string, availablePorts []PortForwardAlternative) int {
	var closestPort int
	minDiff := 1000

	for _, port := range availablePorts {
		if port.Protocol == protocol {
			diff := abs(port.Port - searchedPort)
			if diff < minDiff && diff != 0 {
				minDiff = diff
				closestPort = port.Port
			}
		}
	}

	return closestPort
}

// listAvailablePorts creates a comma-separated list of available ports
func listAvailablePorts(availablePorts []PortForwardAlternative) string {
	var portStrings []string
	for _, port := range availablePorts {
		portStrings = append(portStrings, fmt.Sprintf("%d/%s", port.Port, port.Protocol))
	}
	return strings.Join(portStrings, ", ")
}

// abs returns the absolute value of an integer
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
