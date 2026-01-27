package v1alpha1

import (
	"context"
	"fmt"
	"net"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ValidateCreate validates the PortForwardRule on creation
func (r *PortForwardRule) ValidateCreate() field.ErrorList {
	var allErrs field.ErrorList
	allErrs = append(allErrs, r.validateSpec()...)
	allErrs = append(allErrs, r.validateMutuallyExclusiveFields()...)
	return allErrs
}

// ValidateUpdate validates the PortForwardRule on update
func (r *PortForwardRule) ValidateUpdate(old client.Object) field.ErrorList {
	var allErrs field.ErrorList
	allErrs = append(allErrs, r.validateSpec()...)
	allErrs = append(allErrs, r.validateMutuallyExclusiveFields()...)
	return allErrs
}

// ValidateDelete validates the PortForwardRule on deletion
func (r *PortForwardRule) ValidateDelete() field.ErrorList {
	return nil
}

// validateSpec validates the spec fields
func (r *PortForwardRule) validateSpec() field.ErrorList {
	var allErrs field.ErrorList
	specPath := field.NewPath("spec")

	// Validate external port
	if r.Spec.ExternalPort < 1 || r.Spec.ExternalPort > 65535 {
		allErrs = append(allErrs, field.Invalid(
			specPath.Child("externalPort"),
			r.Spec.ExternalPort,
			"external port must be between 1 and 65535",
		))
	}

	// Validate protocol
	validProtocols := []string{"tcp", "udp", "both"}
	if !contains(validProtocols, r.Spec.Protocol) {
		allErrs = append(allErrs, field.NotSupported(
			specPath.Child("protocol"),
			r.Spec.Protocol,
			validProtocols,
		))
	}

	// Validate destination IP if specified
	if r.Spec.DestinationIP != nil {
		if ip := net.ParseIP(*r.Spec.DestinationIP); ip == nil {
			allErrs = append(allErrs, field.Invalid(
				specPath.Child("destinationIP"),
				*r.Spec.DestinationIP,
				"must be a valid IPv4 address",
			))
		}
	}

	// Validate destination port if specified
	if r.Spec.DestinationPort != nil {
		if *r.Spec.DestinationPort < 1 || *r.Spec.DestinationPort > 65535 {
			allErrs = append(allErrs, field.Invalid(
				specPath.Child("destinationPort"),
				*r.Spec.DestinationPort,
				"destination port must be between 1 and 65535",
			))
		}
	}

	// Validate source IP restriction if specified
	if r.Spec.SourceIPRestriction != nil && *r.Spec.SourceIPRestriction != "" {
		if ip := net.ParseIP(*r.Spec.SourceIPRestriction); ip == nil {
			allErrs = append(allErrs, field.Invalid(
				specPath.Child("sourceIPRestriction"),
				*r.Spec.SourceIPRestriction,
				"must be a valid IPv4 address",
			))
		}
	}

	// Validate priority
	if r.Spec.Priority < 0 || r.Spec.Priority > 1000 {
		allErrs = append(allErrs, field.Invalid(
			specPath.Child("priority"),
			r.Spec.Priority,
			"priority must be between 0 and 1000",
		))
	}

	// Validate conflict policy
	validPolicies := []string{"warn", "error", "ignore"}
	if !contains(validPolicies, r.Spec.ConflictPolicy) {
		allErrs = append(allErrs, field.NotSupported(
			specPath.Child("conflictPolicy"),
			r.Spec.ConflictPolicy,
			validPolicies,
		))
	}

	// Validate service reference if specified
	if r.Spec.ServiceRef != nil {
		allErrs = append(allErrs, r.validateServiceRef(specPath.Child("serviceRef"))...)
	}

	return allErrs
}

// validateServiceRef validates the service reference
func (r *PortForwardRule) validateServiceRef(path *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	// Simple DNS name validation for service name
	if !isValidDNSName(r.Spec.ServiceRef.Name) {
		allErrs = append(allErrs, field.Invalid(
			path.Child("name"),
			r.Spec.ServiceRef.Name,
			"service name must be a valid DNS name",
		))
	}

	// Validate namespace if specified
	if r.Spec.ServiceRef.Namespace != nil {
		if !isValidDNSName(*r.Spec.ServiceRef.Namespace) {
			allErrs = append(allErrs, field.Invalid(
				path.Child("namespace"),
				*r.Spec.ServiceRef.Namespace,
				"namespace must be a valid DNS name",
			))
		}
	}

	// Validate port name/number
	if r.Spec.ServiceRef.Port == "" {
		allErrs = append(allErrs, field.Required(
			path.Child("port"),
			"service port must be specified",
		))
	}

	return allErrs
}

// isValidDNSName validates DNS names
func isValidDNSName(name string) bool {
	if name == "" {
		return false
	}
	// Simple validation - allow lowercase letters, numbers, and hyphens
	matched, _ := regexp.MatchString(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`, name)
	return matched
}

// validateMutuallyExclusiveFields validates that serviceRef and destinationIP are mutually exclusive
func (r *PortForwardRule) validateMutuallyExclusiveFields() field.ErrorList {
	var allErrs field.ErrorList
	specPath := field.NewPath("spec")

	hasServiceRef := r.Spec.ServiceRef != nil
	hasDestinationIP := r.Spec.DestinationIP != nil

	if hasServiceRef && hasDestinationIP {
		allErrs = append(allErrs, field.Forbidden(
			specPath.Child("destinationIP"),
			"destinationIP cannot be specified when serviceRef is specified",
		))
	}

	if !hasServiceRef && !hasDestinationIP {
		allErrs = append(allErrs, field.Required(
			specPath,
			"either serviceRef or destinationIP must be specified",
		))
	}

	// If destinationIP is specified, destinationPort must also be specified
	if hasDestinationIP && r.Spec.DestinationPort == nil {
		allErrs = append(allErrs, field.Required(
			specPath.Child("destinationPort"),
			"destinationPort is required when destinationIP is specified",
		))
	}

	return allErrs
}

// ValidateCrossNamespacePortConflict checks for port conflicts across all namespaces
func (r *PortForwardRule) ValidateCrossNamespacePortConflict(ctx context.Context, client client.Client) field.ErrorList {
	var allErrs field.ErrorList
	specPath := field.NewPath("spec")

	// If client is nil, we can't validate conflicts
	if client == nil {
		return allErrs
	}

	// List all PortForwardRules in all namespaces
	var ruleList PortForwardRuleList
	if err := client.List(ctx, &ruleList); err != nil {
		// If we can't list, we can't validate conflicts
		return allErrs
	}

	for _, existingRule := range ruleList.Items {
		// Skip self
		if existingRule.Namespace == r.Namespace && existingRule.Name == r.Name {
			continue
		}

		// Check port conflict
		if existingRule.Spec.ExternalPort == r.Spec.ExternalPort &&
			(existingRule.Spec.Protocol == r.Spec.Protocol || existingRule.Spec.Protocol == "both" || r.Spec.Protocol == "both") {

			// Same namespace conflict = error
			if existingRule.Namespace == r.Namespace {
				allErrs = append(allErrs, field.Forbidden(
					specPath.Child("externalPort"),
					fmt.Sprintf("port %d conflicts with existing rule %s in same namespace", r.Spec.ExternalPort, existingRule.Name),
				))
			}
		}

		// Check conflicts with existing Service annotations (backward compatibility)
		var serviceList corev1.ServiceList
		if err := client.List(ctx, &serviceList); err != nil {
			return allErrs
		}

		for _, service := range serviceList.Items {
			if port, hasAnnotation := service.Annotations["port-forwarder.unifi.com/external-port"]; hasAnnotation {
				if servicePort, protocol := parseServiceAnnotation(port); servicePort == r.Spec.ExternalPort &&
					(protocol == r.Spec.Protocol || protocol == "both" || r.Spec.Protocol == "both") {

					if service.Namespace == r.Namespace {
						allErrs = append(allErrs, field.Forbidden(
							specPath.Child("externalPort"),
							fmt.Sprintf("port %d conflicts with existing Service annotation on %s/%s", r.Spec.ExternalPort, service.Namespace, service.Name),
						))
					}
				}
			}
		}
	}
	return allErrs
}

// ValidateServiceExists validates that the referenced service exists
func (r *PortForwardRule) ValidateServiceExists(ctx context.Context, client client.Client) field.ErrorList {
	var allErrs field.ErrorList
	specPath := field.NewPath("spec")

	if r.Spec.ServiceRef == nil || client == nil {
		return allErrs
	}

	// Determine namespace
	namespace := r.Namespace
	if r.Spec.ServiceRef.Namespace != nil {
		namespace = *r.Spec.ServiceRef.Namespace
	}

	// Get the service
	var service corev1.Service
	err := client.Get(ctx, types.NamespacedName{
		Name:      r.Spec.ServiceRef.Name,
		Namespace: namespace,
	}, &service)

	if err != nil {
		allErrs = append(allErrs, field.Invalid(
			specPath.Child("serviceRef"),
			r.Spec.ServiceRef.Name,
			fmt.Sprintf("service %s/%s not found: %v", namespace, r.Spec.ServiceRef.Name, err),
		))
		return allErrs
	}

	// Validate that the service port exists
	portFound := false
	for _, port := range service.Spec.Ports {
		if port.Name == r.Spec.ServiceRef.Port || fmt.Sprintf("%d", port.Port) == r.Spec.ServiceRef.Port {
			portFound = true
			break
		}
	}

	if !portFound {
		allErrs = append(allErrs, field.Invalid(
			specPath.Child("serviceRef").Child("port"),
			r.Spec.ServiceRef.Port,
			fmt.Sprintf("port %s not found in service %s/%s", r.Spec.ServiceRef.Port, namespace, r.Spec.ServiceRef.Name),
		))
	}

	return allErrs
}

// Helper functions

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func parseServiceAnnotation(annotation string) (int, string) {
	// Parse format like "80:tcp" or "8080"
	port := 80
	protocol := "tcp"

	if n, err := fmt.Sscanf(annotation, "%d:%s", &port, &protocol); err != nil || n == 0 {
		if n, err := fmt.Sscanf(annotation, "%d", &port); err != nil || n == 0 {
			return 0, ""
		}
	}

	return port, protocol
}
