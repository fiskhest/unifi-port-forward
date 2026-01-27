package utils

import (
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"unifi-port-forward/pkg/routers"
)

// PortMapping represents parsed annotation mapping
type PortMapping struct {
	PortName     string // Service port name
	ExternalPort int    // External port (DstPort)
}

// GetLBIP extracts the LoadBalancer IP from a service
func GetLBIP(service *v1.Service) string {
	if len(service.Status.LoadBalancer.Ingress) > 0 {
		for _, ingress := range service.Status.LoadBalancer.Ingress {
			if ingress.IP != "" {
				return ingress.IP
			}
		}
	}

	return ""
}

// parsePortMappingAnnotation parses port mapping annotation like "1234:http,8443:https"
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

// parseSingleMapping parses individual port mapping like "1234:http" or "https"
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
		// Custom mapping: "1234:http" (externalPort:serviceName)
		externalPort, err := strconv.Atoi(parts[0])
		if err != nil {
			return PortMapping{}, fmt.Errorf("invalid external port '%s' in mapping '%s' - must be a number between 1-65535. Valid format: 'externalPort:portname' or 'portname'. Example: '8080:http,8443:https'", parts[0], mapping)
		}

		if externalPort < 1 || externalPort > 65535 {
			return PortMapping{}, fmt.Errorf("external port %d out of valid range (1-65535) in mapping '%s'. Valid format: 'externalPort:portname' or 'portname'. Example: '8080:http,8443:https'", externalPort, mapping)
		}

		return PortMapping{
			PortName:     parts[1],
			ExternalPort: externalPort,
		}, nil

	default:
		return PortMapping{}, fmt.Errorf("invalid mapping format: too many colons in '%s'. Valid format: 'externalPort:portname' or 'portname'. Example: '8080:http,8443:https'", mapping)
	}
}

// validatePortMappings validates that all mapped port names exist in service and no conflicts
func validatePortMappings(service *v1.Service, mappings []PortMapping) error {
	// Check that all mapped port names exist in service
	servicePortNames := make(map[string]bool)
	for _, port := range service.Spec.Ports {
		servicePortNames[port.Name] = true
	}

	// Build available ports list for better error message
	var availablePorts []string
	for _, port := range service.Spec.Ports {
		availablePorts = append(availablePorts, fmt.Sprintf("%s(%d)", port.Name, port.Port))
	}

	for _, mapping := range mappings {
		if !servicePortNames[mapping.PortName] {
			return fmt.Errorf("port mapping references non-existent port '%s' in service %s/%s - available ports: %s. Valid format: 'externalPort:portname' or 'portname'. Example: '8080:http,8443:https'",
				mapping.PortName, service.Namespace, service.Name, strings.Join(availablePorts, ", "))
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

// GetPortNameByNumber returns the port name for a given port number from service spec
func GetPortNameByNumber(service *v1.Service, portNumber int) string {
	for _, port := range service.Spec.Ports {
		if int(port.Port) == portNumber {
			if port.Name != "" {
				return port.Name
			}
			// Fallback to port if no name is set
			return string(port.Port)
		}
	}
	return fmt.Sprintf("%d", portNumber)
}

// GetServicePortByName returns the port config for a given port name from service spec
func GetServicePortByName(service *v1.Service, portName string) *v1.ServicePort {
	for _, port := range service.Spec.Ports {
		if port.Name == portName {
			return &port
		}
	}
	return nil
}

// GetPortConfigs creates multiple PortConfigs from a service (supports multiple ports)
func GetPortConfigs(service *v1.Service, lbIP, annotationKey string) ([]routers.PortConfig, error) {
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
		if err := CheckPortConflict(externalPort, serviceKey); err != nil {
			return nil, err
		}

		// Mark this port as used by this service
		markPortUsed(externalPort, serviceKey)

		protocol := strings.ToLower(string(servicePort.Protocol))

		configs = append(configs, routers.PortConfig{
			Name:      fmt.Sprintf("%s/%s:%s", service.Namespace, service.Name, servicePort.Name),
			DstPort:   externalPort,          // External port from annotation
			FwdPort:   int(servicePort.Port), // Internal service port
			Enabled:   true,
			Interface: "wan",
			DstIP:     lbIP,
			SrcIP:     "any",
			Protocol:  protocol,
		})
	}

	return configs, nil
}
