package handlers

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	v1 "k8s.io/api/core/v1"
	"kube-router-port-forward/routers"
)

// Port conflict detection and tracking
var (
	usedExternalPorts = make(map[int]string) // port -> serviceKey
	portMutex         sync.RWMutex
)

// PortMapping represents parsed annotation mapping
type PortMapping struct {
	PortName     string // Service port name
	ExternalPort int    // External port (DstPort)
}

// checkPortConflict checks if external port is already used by another service
func checkPortConflict(externalPort int, serviceKey string) error {
	portMutex.Lock()
	defer portMutex.Unlock()

	if existingService, exists := usedExternalPorts[externalPort]; exists {
		if existingService != serviceKey {
			return fmt.Errorf("external port %d already used by service %s", externalPort, existingService)
		}
	}
	return nil
}

// markPortUsed marks an external port as used by a service
func markPortUsed(externalPort int, serviceKey string) {
	portMutex.Lock()
	defer portMutex.Unlock()
	usedExternalPorts[externalPort] = serviceKey
}

// unmarkPortUsed removes external port from tracking
func unmarkPortUsed(externalPort int) {
	portMutex.Lock()
	defer portMutex.Unlock()
	delete(usedExternalPorts, externalPort)
}

// ClearPortConflictTracking clears all port tracking (for testing)
func ClearPortConflictTracking() {
	portMutex.Lock()
	defer portMutex.Unlock()
	usedExternalPorts = make(map[int]string)
}

// GetLBIP extracts the LoadBalancer IP from a service
func GetLBIP(service *v1.Service) string {
	fmt.Printf("DEBUG: getLBIP called for service %s/%s\n", service.Namespace, service.Name)

	// Only use status.loadBalancer.ingress for LoadBalancer services
	// Filter out node IPs and only use VIPs
	fmt.Printf("DEBUG: LoadBalancer Ingress count: %d\n", len(service.Status.LoadBalancer.Ingress))
	if len(service.Status.LoadBalancer.Ingress) > 0 {
		for i, ingress := range service.Status.LoadBalancer.Ingress {
			fmt.Printf("DEBUG: Ingress[%d]: IP=%s, Hostname=%s, IPMode=%s\n", i, ingress.IP, ingress.Hostname, getIPMode(ingress))
		}

		// Prefer VIP mode IPs (most stable for LoadBalancer)
		for _, ingress := range service.Status.LoadBalancer.Ingress {
			if ingress.IP != "" && isVIPIngress(ingress) {
				fmt.Printf("DEBUG: Using VIP IP: %s\n", ingress.IP)
				return ingress.IP
			}
		}

		// Fallback to any IP if no VIP found
		for _, ingress := range service.Status.LoadBalancer.Ingress {
			if ingress.IP != "" {
				fmt.Printf("DEBUG: Using fallback IP: %s\n", ingress.IP)
				return ingress.IP
			}
		}
	}

	fmt.Printf("DEBUG: Service %s has no LoadBalancer IP\n", service.Name)
	return ""
}

// isVIPIngress checks if ingress is VIP mode (stable LoadBalancer IP)
func isVIPIngress(ingress v1.LoadBalancerIngress) bool {
	// Check if it's likely a VIP by IP range or mode
	if ingress.IP != "" {
		// MetalLB VIPs are typically in specific ranges
		// For your case, 192.168.72.1 is VIP, 192.168.27.130 is a node IP
		// This is a heuristic - adjust based on your network
		return !isNodeIP(ingress.IP)
	}
	return false
}

// isNodeIP detects node IPs vs VIP IPs
func isNodeIP(ip string) bool {
	// Add logic to identify node IPs vs LoadBalancer VIPs
	// This is network-specific - adjust for your environment
	nodeIPRanges := []string{
		"192.168.27.", // Your node IP range
		// Add other node IP ranges as needed
	}

	for _, rangePrefix := range nodeIPRanges {
		if len(ip) >= len(rangePrefix) && ip[:len(rangePrefix)] == rangePrefix {
			return true
		}
	}
	return false
}

// getIPMode gets IP mode from ingress
func getIPMode(ingress v1.LoadBalancerIngress) string {
	if ingress.IPMode != nil {
		return string(*ingress.IPMode)
	}
	return "unknown"
}

// parsePortMappingAnnotation parses port mapping annotation like "http:1234,https:8443"
func parsePortMappingAnnotation(annotation string) ([]PortMapping, error) {
	if annotation == "" {
		return nil, nil
	}

	var mappings []PortMapping
	parts := strings.Split(annotation, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		mapping, err := parseSingleMapping(part)
		if err != nil {
			return nil, fmt.Errorf("invalid port mapping '%s': %w", part, err)
		}

		mappings = append(mappings, mapping)
	}

	return mappings, nil
}

// parseSingleMapping parses individual port mapping like "http:1234" or "https"
func parseSingleMapping(mapping string) (PortMapping, error) {
	parts := strings.Split(mapping, ":")

	switch len(parts) {
	case 1:
		// Default mapping: "http" -> use service port as external port
		return PortMapping{
			PortName: parts[0],
			// ExternalPort will be set from service port later
		}, nil

	case 2:
		// Custom mapping: "http:1234"
		externalPort, err := strconv.Atoi(parts[1])
		if err != nil {
			return PortMapping{}, fmt.Errorf("invalid external port '%s': %w", parts[1], err)
		}

		if externalPort < 1 || externalPort > 65535 {
			return PortMapping{}, fmt.Errorf("external port %d out of valid range (1-65535)", externalPort)
		}

		return PortMapping{
			PortName:     parts[0],
			ExternalPort: externalPort,
		}, nil

	default:
		return PortMapping{}, fmt.Errorf("invalid mapping format: too many colons in '%s'", mapping)
	}
}

// validatePortMappings validates that all mapped port names exist in service and no conflicts
func validatePortMappings(service *v1.Service, mappings []PortMapping) error {
	// Check that all mapped port names exist in service
	servicePortNames := make(map[string]bool)
	for _, port := range service.Spec.Ports {
		servicePortNames[port.Name] = true
	}

	for _, mapping := range mappings {
		if !servicePortNames[mapping.PortName] {
			return fmt.Errorf("port mapping references non-existent port '%s'", mapping.PortName)
		}
	}

	// Check for duplicate external ports within this service
	externalPorts := make(map[int]bool)
	for _, port := range service.Spec.Ports {
		for _, mapping := range mappings {
			if mapping.PortName == port.Name {
				externalPort := mapping.ExternalPort
				if externalPort == 0 {
					externalPort = int(port.Port)
				}

				if externalPorts[externalPort] {
					return fmt.Errorf("duplicate external port %d within service", externalPort)
				}
				externalPorts[externalPort] = true
			}
		}
	}

	return nil
}

// GetPortConfigs creates multiple PortConfigs from a service (supports multiple ports)
func GetPortConfigs(service *v1.Service, annotationKey string) ([]routers.PortConfig, error) {
	serviceKey := fmt.Sprintf("%s/%s", service.Namespace, service.Name)

	// Parse annotation
	annotation := service.Annotations[annotationKey]
	if annotation == "" {
		return nil, fmt.Errorf("no port annotation found")
	}

	mappings, err := parsePortMappingAnnotation(annotation)
	if err != nil {
		return nil, fmt.Errorf("failed to parse port mapping: %w", err)
	}

	// Validate mappings against service definition
	if err := validatePortMappings(service, mappings); err != nil {
		return nil, err
	}

	var configs []routers.PortConfig

	// Create PortConfig for each service port
	for _, servicePort := range service.Spec.Ports {
		// Find matching annotation mapping
		var externalPort int
		var foundMapping bool

		for _, mapping := range mappings {
			if mapping.PortName == servicePort.Name {
				if mapping.ExternalPort != 0 {
					externalPort = mapping.ExternalPort
				} else {
					externalPort = int(servicePort.Port) // Default to service port
				}
				foundMapping = true
				break
			}
		}

		// Skip ports not mentioned in annotation
		if !foundMapping {
			continue
		}

		// Check for port conflicts with other services
		if err := checkPortConflict(externalPort, serviceKey); err != nil {
			return nil, err
		}

		protocol := strings.ToLower(string(servicePort.Protocol))

		configs = append(configs, routers.PortConfig{
			Name:      fmt.Sprintf("%s-%s", service.Name, servicePort.Name),
			DstPort:   externalPort,          // External port from annotation
			FwdPort:   int(servicePort.Port), // Internal service port
			Enabled:   true,
			Interface: "wan",
			DstIP:     GetLBIP(service),
			SrcIP:     "any",
			Protocol:  protocol, // From service definition
		})
	}

	return configs, nil
}

// getPortConfig creates a single PortConfig from a service (for backward compatibility)
// DEPRECATED: Use getPortConfigs() for multi-port support
func getPortConfig(service *v1.Service) routers.PortConfig {
	protocol := "tcp"
	if len(service.Spec.Ports) > 0 {
		protocol = strings.ToLower(string(service.Spec.Ports[0].Protocol))
	}
	return routers.PortConfig{
		Name:      service.Name,
		DstPort:   int(service.Spec.Ports[0].Port),
		FwdPort:   int(service.Spec.Ports[0].Port),
		Enabled:   true,
		Interface: "wan",
		DstIP:     GetLBIP(service),
		SrcIP:     "any",
		Protocol:  protocol,
	}
}
