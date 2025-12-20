package validation

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	intstr "k8s.io/apimachinery/pkg/util/intstr"
	"kube-router-port-forward/routers"
)

// ValidationSummary provides detailed validation results with warnings and context
type ValidationSummary struct {
	ValidMappings []PortMapping
	Warnings      []string
	Error         error
	Context       map[string]interface{}
}

// ParseSummary provides detailed parsing results with warnings and context
type ParseSummary struct {
	Warnings []string
	Error    error
	Context  map[string]interface{}
}

// PortMapping represents a parsed annotation mapping
type PortMapping struct {
	PortName     string
	ExternalPort int
	Warning      string
}

// ValidationError represents a validation error with context
type ValidationError struct {
	Field   string
	Value   any
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed for field '%s' with value '%v': %s", e.Field, e.Value, e.Message)
}

// ValidationErrors represents multiple validation errors
type ValidationErrors struct {
	Errors []error
}

func (e *ValidationErrors) Error() string {
	var messages []string
	for _, err := range e.Errors {
		messages = append(messages, err.Error())
	}
	return strings.Join(messages, "; ")
}

// Validator interface for types that can validate themselves
type Validator interface {
	Validate() error
}

// PortConfigValidator validates router PortConfig
type PortConfigValidator struct {
	Config routers.PortConfig
}

func (v *PortConfigValidator) Validate() error {
	var errors []error

	// Validate destination port
	if v.Config.DstPort < 1 || v.Config.DstPort > 65535 {
		errors = append(errors, &ValidationError{
			Field:   "dst_port",
			Value:   v.Config.DstPort,
			Message: "port must be between 1 and 65535",
		})
	}

	// Validate forward port
	if v.Config.FwdPort < 1 || v.Config.FwdPort > 65535 {
		errors = append(errors, &ValidationError{
			Field:   "fwd_port",
			Value:   v.Config.FwdPort,
			Message: "port must be between 1 and 65535",
		})
	}

	// Validate destination IP
	if v.Config.DstIP == "" {
		errors = append(errors, &ValidationError{
			Field:   "dst_ip",
			Value:   v.Config.DstIP,
			Message: "IP address cannot be empty",
		})
	} else if net.ParseIP(v.Config.DstIP) == nil {
		errors = append(errors, &ValidationError{
			Field:   "dst_ip",
			Value:   v.Config.DstIP,
			Message: "invalid IP address format",
		})
	}

	// Validate protocol
	if !isValidProtocol(v.Config.Protocol) {
		errors = append(errors, &ValidationError{
			Field:   "protocol",
			Value:   v.Config.Protocol,
			Message: "protocol must be 'tcp' or 'udp'",
		})
	}

	// Validate name
	if v.Config.Name == "" {
		errors = append(errors, &ValidationError{
			Field:   "name",
			Value:   v.Config.Name,
			Message: "name cannot be empty",
		})
	}

	// Validate interface
	if !isValidInterface(v.Config.Interface) {
		errors = append(errors, &ValidationError{
			Field:   "interface",
			Value:   v.Config.Interface,
			Message: "interface must be 'wan', 'lan', or valid interface name",
		})
	}

	if len(errors) > 0 {
		return &ValidationErrors{Errors: errors}
	}

	return nil
}

// ServiceValidator validates Kubernetes Service
type ServiceValidator struct {
	Service          *v1.Service
	FilterAnnotation string
}

func (v *ServiceValidator) Validate() error {
	var errors []error

	// Validate service type
	if v.Service.Spec.Type != v1.ServiceTypeLoadBalancer {
		errors = append(errors, &ValidationError{
			Field:   "type",
			Value:   v.Service.Spec.Type,
			Message: "service must be of type LoadBalancer",
		})
	}

	// Validate service name
	if v.Service.Name == "" {
		errors = append(errors, &ValidationError{
			Field:   "name",
			Value:   v.Service.Name,
			Message: "service name cannot be empty",
		})
	}

	// Validate namespace
	if v.Service.Namespace == "" {
		errors = append(errors, &ValidationError{
			Field:   "namespace",
			Value:   v.Service.Namespace,
			Message: "service namespace cannot be empty",
		})
	}

	// Validate annotation format if present
	if v.FilterAnnotation != "" {
		if annotation, exists := v.Service.Annotations[v.FilterAnnotation]; exists {
			if err := validateAnnotationFormat(annotation); err != nil {
				errors = append(errors, &ValidationError{
					Field:   "annotation",
					Value:   annotation,
					Message: fmt.Sprintf("invalid annotation format: %v", err),
				})
			}
		}
	}

	// Validate ports
	if len(v.Service.Spec.Ports) == 0 {
		errors = append(errors, &ValidationError{
			Field:   "ports",
			Value:   v.Service.Spec.Ports,
			Message: "service must have at least one port defined",
		})
	} else {
		for i, port := range v.Service.Spec.Ports {
			if err := validateServicePort(port, i); err != nil {
				errors = append(errors, err)
			}
		}
	}

	if len(errors) > 0 {
		return &ValidationErrors{Errors: errors}
	}

	return nil
}

// AnnotationValidator validates port mapping annotation
type AnnotationValidator struct {
	Annotation string
}

func (v *AnnotationValidator) Validate() error {
	return validateAnnotationFormat(v.Annotation)
}

// validateProtocol checks if protocol is valid
func isValidProtocol(protocol string) bool {
	switch strings.ToLower(protocol) {
	case "tcp", "udp":
		return true
	default:
		return false
	}
}

// validateInterface checks if interface is valid
func isValidInterface(iface string) bool {
	switch strings.ToLower(iface) {
	case "wan", "lan":
		return true
	default:
		// Allow any non-empty interface name for flexibility
		return iface != ""
	}
}

// validateServicePort validates a single service port
func validateServicePort(port v1.ServicePort, index int) error {
	var errors []error

	if port.Port < 1 || port.Port > 65535 {
		errors = append(errors, &ValidationError{
			Field:   fmt.Sprintf("ports[%d].port", index),
			Value:   port.Port,
			Message: "port must be between 1 and 65535",
		})
	}

	if port.TargetPort.Type == intstr.Int && port.TargetPort.IntVal == 0 {
		errors = append(errors, &ValidationError{
			Field:   fmt.Sprintf("ports[%d].target_port", index),
			Value:   port.TargetPort,
			Message: "target port must be specified",
		})
	} else if port.TargetPort.Type == intstr.Int && (port.TargetPort.IntVal < 1 || port.TargetPort.IntVal > 65535) {
		errors = append(errors, &ValidationError{
			Field:   fmt.Sprintf("ports[%d].target_port", index),
			Value:   port.TargetPort.IntVal,
			Message: "target port must be between 1 and 65535",
		})
	}

	if !isValidProtocol(string(port.Protocol)) {
		errors = append(errors, &ValidationError{
			Field:   fmt.Sprintf("ports[%d].protocol", index),
			Value:   port.Protocol,
			Message: "protocol must be 'TCP' or 'UDP'",
		})
	}

	if len(errors) > 0 {
		return &ValidationErrors{Errors: errors}
	}

	return nil
}

// validateAnnotationFormat validates the annotation string format
func validateAnnotationFormat(annotation string) error {
	if annotation == "" {
		return nil
	}

	// Split by comma for multiple port mappings
	parts := strings.Split(annotation, ",")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Validate individual mapping format
		if err := validatePortMapping(part, i); err != nil {
			return err
		}
	}

	return nil
}

// validatePortMapping validates individual port mapping like "http:8080" or "https"
func validatePortMapping(mapping string, index int) error {
	parts := strings.Split(mapping, ":")

	switch len(parts) {
	case 1:
		// Default mapping: "http" - just port name
		if parts[0] == "" {
			return &ValidationError{
				Field:   fmt.Sprintf("mapping[%d]", index),
				Value:   mapping,
				Message: "port name cannot be empty",
			}
		}
		return nil

	case 2:
		// Custom mapping: "http:8080"
		if parts[0] == "" {
			return &ValidationError{
				Field:   fmt.Sprintf("mapping[%d]", index),
				Value:   mapping,
				Message: "port name cannot be empty",
			}
		}

		externalPort, err := strconv.Atoi(parts[1])
		if err != nil {
			return &ValidationError{
				Field:   fmt.Sprintf("mapping[%d].external_port", index),
				Value:   parts[1],
				Message: "external port must be a valid integer",
			}
		}

		if externalPort < 1 || externalPort > 65535 {
			return &ValidationError{
				Field:   fmt.Sprintf("mapping[%d].external_port", index),
				Value:   externalPort,
				Message: "external port must be between 1 and 65535",
			}
		}

		return nil

	default:
		return &ValidationError{
			Field:   fmt.Sprintf("mapping[%d]", index),
			Value:   mapping,
			Message: "invalid mapping format: too many colons",
		}
	}
}

// ValidatePortConfigs validates multiple port configurations
func ValidatePortConfigs(configs []routers.PortConfig) error {
	if len(configs) == 0 {
		return &ValidationError{
			Field:   "port_configs",
			Value:   configs,
			Message: "at least one port configuration is required",
		}
	}

	var errors []error
	externalPorts := make(map[int]bool)

	for i, config := range configs {
		validator := &PortConfigValidator{Config: config}
		if err := validator.Validate(); err != nil {
			errors = append(errors, fmt.Errorf("port_configs[%d]: %w", i, err))
		}

		// Check for duplicate external ports
		if externalPorts[config.DstPort] {
			errors = append(errors, &ValidationError{
				Field:   fmt.Sprintf("port_configs[%d].dst_port", i),
				Value:   config.DstPort,
				Message: "duplicate external port found",
			})
		}
		externalPorts[config.DstPort] = true
	}

	if len(errors) > 0 {
		return &ValidationErrors{Errors: errors}
	}

	return nil
}

// ValidateService validates a service for port forwarding
func ValidateService(service *v1.Service, filterAnnotation string) error {
	validator := &ServiceValidator{
		Service:          service,
		FilterAnnotation: filterAnnotation,
	}
	return validator.Validate()
}

// ValidatePortConfig validates a single port configuration
func ValidatePortConfig(config routers.PortConfig) error {
	validator := &PortConfigValidator{Config: config}
	return validator.Validate()
}

// ValidateAnnotation validates port mapping annotation
func ValidateAnnotation(annotation string) error {
	validator := &AnnotationValidator{Annotation: annotation}
	return validator.Validate()
}
