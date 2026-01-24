package helpers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"unifi-port-forward/pkg/config"
	"unifi-port-forward/pkg/routers"

	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
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

// CheckPortConflict checks if external port is already used by another service
func CheckPortConflict(externalPort int, serviceKey string) error {
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

// UnmarkPortUsed removes external port from tracking (exported for use by controller)
// This function is called during service deletion to free up external ports for reuse
func UnmarkPortUsed(externalPort int) {
	portMutex.Lock()
	defer portMutex.Unlock()
	delete(usedExternalPorts, externalPort)
}

// ResetPortTracking clears all external port tracking (for testing)
func ResetPortTracking() {
	portMutex.Lock()
	defer portMutex.Unlock()
	usedExternalPorts = make(map[int]string)
}

// ClearPortConflictTracking clears all port tracking (for testing only)
// This function should NOT be used in production code
func ClearPortConflictTracking() {
	portMutex.Lock()
	defer portMutex.Unlock()
	usedExternalPorts = make(map[int]string)
}

// UnmarkPortsForService removes all external ports used by a specific service
// This is useful for bulk cleanup during service deletion
func UnmarkPortsForService(serviceKey string) {
	portMutex.Lock()
	defer portMutex.Unlock()

	for port, svc := range usedExternalPorts {
		if svc == serviceKey {
			delete(usedExternalPorts, port)
		}
	}
}

// GetUsedExternalPorts returns a copy of the used external ports map
// Exported for controller to read port conflict tracking state
func GetUsedExternalPorts() map[int]string {
	portMutex.RLock()
	defer portMutex.RUnlock()

	// Return a copy to prevent race conditions
	copy := make(map[int]string)
	for k, v := range usedExternalPorts {
		copy[k] = v
	}
	return copy
}

// GetPortMutex returns the port mutex for external coordination
// Exported for controller to safely access port tracking state
func GetPortMutex() *sync.RWMutex {
	return &portMutex
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

// GetPortConfigs creates multiple PortConfigs from a service (supports multiple ports)
func GetPortConfigs(service *v1.Service, lbIP string, annotationKey string) ([]routers.PortConfig, error) {
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
			Protocol:  protocol, // From service definition
		})
	}

	// Check if we generated any port configurations
	if len(configs) == 0 {
		// Build helpful error message with available ports and format examples
		var availablePorts []string
		for _, port := range service.Spec.Ports {
			availablePorts = append(availablePorts, fmt.Sprintf("%s(%d)", port.Name, port.Port))
		}

		annotation := service.Annotations[annotationKey]
		return nil, fmt.Errorf("no valid port configurations generated from annotation '%s' for service %s/%s. Available ports: %s. Valid format: 'externalPort:portname' or 'portname'. Example: '8080:%s'",
			annotation, service.Namespace, service.Name, strings.Join(availablePorts, ", "), service.Spec.Ports[0].Name)
	}

	return configs, nil
}

// GetServicePortByName finds a service port by name (used in tests)
func GetServicePortByName(service *v1.Service, name string) v1.ServicePort {
	for _, port := range service.Spec.Ports {
		if port.Name == name {
			return port
		}
	}
	return v1.ServicePort{}
}

// SyncPortTrackingWithRouter synchronizes port tracking with the router's current port forwarding rules
func SyncPortTrackingWithRouter(ctx context.Context, router routers.Router) error {
	rules, err := router.ListAllPortForwards(ctx)
	if err != nil {
		return fmt.Errorf("failed to list port forwards: %w", err)
	}

	// Clear existing tracking
	ResetPortTracking()

	// Add managed rules to tracking
	for _, rule := range rules {
		if isManagedRule(rule.Name) {
			externalPort := 0
			if rule.DstPort != "" {
				if port, err := strconv.Atoi(rule.DstPort); err == nil {
					externalPort = port
				}
			}
			if externalPort > 0 {
				serviceKey := extractServiceKeyFromRuleName(rule.Name)
				markPortUsed(externalPort, serviceKey)
			}
		}
	}

	return nil
}

// SyncPortTrackingWithRouterSelective synchronizes port tracking only when there are managed rules
func SyncPortTrackingWithRouterSelective(ctx context.Context, router routers.Router, skipIfEmpty bool) error {
	// When skipIfEmpty=false, we always sync (single call)
	if !skipIfEmpty {
		rules, err := router.ListAllPortForwards(ctx)
		if err != nil {
			return fmt.Errorf("failed to list port forwards: %w", err)
		}

		// Clear existing tracking
		ResetPortTracking()

		// Add managed rules to tracking
		for _, rule := range rules {
			if isManagedRule(rule.Name) {
				externalPort := 0
				if rule.DstPort != "" {
					if port, err := strconv.Atoi(rule.DstPort); err == nil {
						externalPort = port
					}
				}
				if externalPort > 0 {
					serviceKey := extractServiceKeyFromRuleName(rule.Name)
					markPortUsed(externalPort, serviceKey)
				}
			}
		}
		return nil
	}

	// When skipIfEmpty=true, we check first, then sync if needed (potentially 2 calls)
	rules, err := router.ListAllPortForwards(ctx)
	if err != nil {
		return fmt.Errorf("failed to list port forwards: %w", err)
	}

	// Check if we have any managed rules
	hasManagedRules := false
	for _, rule := range rules {
		if isManagedRule(rule.Name) {
			hasManagedRules = true
			break
		}
	}

	// Skip sync if no managed rules and skipIfEmpty is true
	if skipIfEmpty && !hasManagedRules {
		return nil
	}

	// We have managed rules, need to sync - call ListAllPortForwards again
	rules, err = router.ListAllPortForwards(ctx)
	if err != nil {
		return fmt.Errorf("failed to list port forwards for sync: %w", err)
	}

	// Clear existing tracking
	ResetPortTracking()

	// Add managed rules to tracking
	for _, rule := range rules {
		if isManagedRule(rule.Name) {
			externalPort := 0
			if rule.DstPort != "" {
				if port, err := strconv.Atoi(rule.DstPort); err == nil {
					externalPort = port
				}
			}
			if externalPort > 0 {
				serviceKey := extractServiceKeyFromRuleName(rule.Name)
				markPortUsed(externalPort, serviceKey)
			}
		}
	}

	return nil
}

// isManagedRule checks if a rule is managed by the controller (has namespace/service:port format)
func isManagedRule(ruleName string) bool {
	// Managed rules follow the pattern: namespace/service:port
	parts := strings.SplitN(ruleName, ":", 2)
	if len(parts) != 2 {
		return false
	}

	// Check if the first part contains a namespace/service pattern
	servicePart := parts[0]
	serviceParts := strings.SplitN(servicePart, "/", 2)

	// Must have both namespace and service, and neither should be empty
	return len(serviceParts) == 2 && serviceParts[0] != "" && serviceParts[1] != ""
}

// extractServiceKeyFromRuleName extracts the service key (namespace/service) from a rule name
func extractServiceKeyFromRuleName(ruleName string) string {
	// Rule format: namespace/service:port
	parts := strings.SplitN(ruleName, ":", 2)
	if len(parts) == 0 {
		return ""
	}

	// Return the first part (namespace/service) or the whole string if no colon
	if len(parts) == 1 {
		return parts[0]
	}

	return parts[0]
}

// ParseIntField parses a string field to int with graceful fallback
// Returns 0 for empty strings, negative numbers, or parse errors
func ParseIntField(field string) int {
	if field == "" {
		return 0
	}
	if result, err := strconv.Atoi(field); err == nil && result >= 0 {
		return result
	}
	return 0
}

// RuleBelongsToService checks if a port forwarding rule belongs to a specific service
// by performing exact matching of namespace and service name
func RuleBelongsToService(ruleName, namespace, serviceName string) bool {
	// Rule format: namespace/service:port
	parts := strings.SplitN(ruleName, ":", 2)
	if len(parts) != 2 {
		return false
	}

	// Extract namespace/service part
	servicePart := parts[0]
	serviceParts := strings.SplitN(servicePart, "/", 2)
	if len(serviceParts) != 2 {
		return false
	}

	ruleNamespace, ruleServiceName := serviceParts[0], serviceParts[1]
	return ruleNamespace == namespace && ruleServiceName == serviceName
}

// createUncachedClient creates a controller-runtime client without cache
func createUncachedClient(restConfig *rest.Config, scheme *runtime.Scheme) (client.Client, error) {
	return client.New(restConfig, client.Options{
		Scheme: scheme,
	})
}

// IsPortForwardRuleCRDAvailable checks if the PortForwardRule CRD exists and is established
func IsPortForwardRuleCRDAvailable(ctx context.Context, restConfig *rest.Config, scheme *runtime.Scheme) bool {
	logger := ctrllog.FromContext(ctx).WithValues("function", "IsPortForwardRuleCRDAvailable")
	crdName := config.PortForwardRulesCRDName

	logger.V(1).Info("Checking CRD availability", "crd_name", crdName)

	uncachedClient, err := createUncachedClient(restConfig, scheme)
	if err != nil {
		logger.Error(err, "failed to create uncached client", "crd_name", crdName)
		return false
	}

	crd := &apiextensionsv1.CustomResourceDefinition{}
	err = uncachedClient.Get(ctx, types.NamespacedName{Name: crdName}, crd)
	if err != nil {
		logger.Error(err, "Failed to get CRD", "crd_name", crdName)
		return false
	}

	logger.V(1).Info("CRD found", "crd_name", crdName, "conditions_count", len(crd.Status.Conditions))

	for _, condition := range crd.Status.Conditions {
		if condition.Type == apiextensionsv1.Established && condition.Status == apiextensionsv1.ConditionTrue {
			logger.V(1).Info("CRD is established", "crd_name", crdName)
			return true
		}
	}

	logger.Info("CRD found but not established", "crd_name", crdName)
	return false
}
