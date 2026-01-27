package utils

import (
	"context"
	"strconv"
	"strings"

	ctrlruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"sigs.k8s.io/controller-runtime/pkg/client"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// extractServiceKeyFromRuleName extracts service namespace/name from rule name
func extractServiceKeyFromRuleName(ruleName string) string {
	// For controller-managed rules, extract namespace/service from namespace/service:port
	parts := strings.Split(ruleName, ":")
	if len(parts) >= 1 {
		// Take the part before colon (namespace/service)
		servicePart := strings.TrimSpace(parts[0])
		if servicePart != "" {
			return servicePart
		}
	}
	return ""
}

// isManagedRule checks if a rule follows the controller's naming pattern
func isManagedRule(ruleName string) bool {
	// Controller rules have format: namespace/service:port
	// Must contain both / and :, and they must be in the right order
	slashIndex := strings.Index(ruleName, "/")
	colonIndex := strings.Index(ruleName, ":")

	// Must have both separators and slash must come before colon
	if slashIndex == -1 || colonIndex == -1 || slashIndex >= colonIndex {
		return false
	}

	// Must have content before slash, between slash and colon, and after colon
	if slashIndex == 0 || colonIndex == slashIndex+1 || colonIndex == len(ruleName)-1 {
		return false
	}

	return true
}

// ExtractServiceKeyFromRuleName extracts service namespace/name from rule name (exported)
func ExtractServiceKeyFromRuleName(ruleName string) string {
	return extractServiceKeyFromRuleName(ruleName)
}

// ExtractServiceKeyFromRuleNameUnexported extracts service namespace/name from rule name (unexported for helpers)
func ExtractServiceKeyFromRuleNameUnexported(ruleName string) string {
	return extractServiceKeyFromRuleName(ruleName)
}

// IsManagedRule checks if a rule follows the controller's naming pattern (exported)
func IsManagedRule(ruleName string) bool {
	return isManagedRule(ruleName)
}

// IsManagedRuleUnexported checks if a rule follows the controller's naming pattern (unexported for helpers)
func IsManagedRuleUnexported(ruleName string) bool {
	return isManagedRule(ruleName)
}

// ParseIntField parses a string field to int with fallback
func ParseIntField(input string) int {
	if input == "" {
		return 0
	}
	if result, err := strconv.Atoi(input); err == nil && result > 0 {
		return result
	}
	return 0
}

// RuleBelongsToService checks if a port forward rule belongs to a specific service
func RuleBelongsToService(ruleName, namespace, serviceName string) bool {
	// Expected format: namespace/service:port
	expectedPrefix := namespace + "/" + serviceName + ":"
	return strings.HasPrefix(ruleName, expectedPrefix)
}

// createUncachedClient creates a new uncached client to overcome CRD discovery issues
func createUncachedClient(restConfig *rest.Config, scheme *ctrlruntime.Scheme) (client.Client, error) {
	return client.New(restConfig, client.Options{Scheme: scheme})
}

// IsPortForwardRuleCRDAvailable checks if a PortForwardRule CRD is available for the given service
func IsPortForwardRuleCRDAvailable(ctx context.Context, restConfig *rest.Config, scheme *ctrlruntime.Scheme) bool {
	logger := ctrllog.FromContext(ctx).WithValues("function", "IsPortForwardRuleCRDAvailable")
	crdName := "portforwardrules.unifi-port-forward.fiskhe.st"

	logger.V(1).Info("Checking CRD availability", "crd_name", crdName)

	uncachedClient, err := createUncachedClient(restConfig, scheme)
	if err != nil {
		logger.Error(err, "failed to create uncached client", "crd_name", crdName)
		return false
	}

	// We need to check if the CRD exists and is established
	crd := &apiextensionsv1.CustomResourceDefinition{}
	err = uncachedClient.Get(ctx, client.ObjectKey{Name: crdName}, crd)
	if err != nil {
		return false
	}

	// Check if CRD is established and has accepted names
	return len(crd.Status.AcceptedNames.Kind) > 0
}
