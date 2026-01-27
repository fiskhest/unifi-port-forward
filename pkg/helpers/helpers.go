package helpers

import (
	"context"
	"strconv"

	"unifi-port-forward/pkg/routers"
	"unifi-port-forward/pkg/utils"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// GetLBIP extracts the LoadBalancer IP from a service using utils package
func GetLBIP(service *v1.Service) string {
	return utils.GetLBIP(service)
}

// GetPortConfigs creates multiple PortConfigs from a service using utils package
func GetPortConfigs(service *v1.Service, lbIP string, annotationKey string) ([]routers.PortConfig, error) {
	return utils.GetPortConfigs(service, lbIP, annotationKey)
}

// UnmarkPortUsed removes external port from tracking using utils package
// This function is called during service deletion to free up external ports for reuse
func UnmarkPortUsed(externalPort int) {
	utils.UnmarkPortUsed(externalPort)
}

// ClearPortConflictTracking clears all port tracking using utils package
// This function should NOT be used in production code
func ClearPortConflictTracking() {
	utils.ClearPortConflictTracking()
}

// RuleBelongsToService checks if a port forward rule belongs to a specific service using utils package
func RuleBelongsToService(ruleName, namespace, serviceName string) bool {
	return utils.RuleBelongsToService(ruleName, namespace, serviceName)
}

// ParseIntField parses a string field to int using utils package
func ParseIntField(input string) int {
	return utils.ParseIntField(input)
}

// GetPortNameByNumber returns the port name for a given port number using utils package
func GetPortNameByNumber(service *v1.Service, portNumber int) string {
	return utils.GetPortNameByNumber(service, portNumber)
}

// SyncPortTrackingWithRouter synchronizes port tracking with actual router state
func SyncPortTrackingWithRouter(ctx context.Context, router routers.Router) error {
	return SyncPortTrackingWithRouterSelective(ctx, router, false)
}

// SyncPortTrackingWithRouterSelective synchronizes port tracking with router state, optionally skipping empty tracking
func SyncPortTrackingWithRouterSelective(ctx context.Context, router routers.Router, skipIfEmpty bool) error {
	logger := ctrllog.FromContext(ctx)

	rules, err := router.ListAllPortForwards(ctx)
	if err != nil {
		logger.Error(err, "failed to list port forwards from router")
		return err
	}

	// Count managed rules for skipIfEmpty logic
	managedRuleCount := 0
	for _, rule := range rules {
		if IsManagedRule(rule.Name) {
			managedRuleCount++
		}
	}

	// Check if we should skip syncing (based on managed rules only)
	if skipIfEmpty && managedRuleCount == 0 {
		logger.Info("Router has no managed port forwards, skipping port tracking sync")
		return nil
	}

	// If we have managed rules to sync (either skipIfEmpty=false or managed rules exist),
	// get fresh state for sync to ensure we have the latest
	if skipIfEmpty && managedRuleCount > 0 {
		// Re-read to ensure we have latest state for sync
		rules, err = router.ListAllPortForwards(ctx)
		if err != nil {
			logger.Error(err, "failed to re-list port forwards from router")
			return err
		}
	}

	logger.Info("Synchronizing port tracking with router state", "total_rules", len(rules))

	// Clear existing tracking
	utils.ClearPortConflictTracking()

	// Rebuild tracking from router state
	managedCount := 0
	manualCount := 0
	for _, rule := range rules {
		if IsManagedRule(rule.Name) {
			// Rule follows controller naming, mark as used
			managedCount++
			serviceKey := utils.ExtractServiceKeyFromRuleName(rule.Name)
			// Parse the external port and mark it as used
			if externalPort, err := strconv.Atoi(rule.DstPort); err == nil {
				markPortUsed(externalPort, serviceKey)
			}
		} else {
			// Manual rule - skip from tracking to allow managed rules to use these ports
			manualCount++
		}
	}

	logger.Info("Port tracking synchronization completed",
		"managed_rules", managedCount,
		"manual_rules", manualCount)

	return nil
}

// IsManagedRule checks if a rule follows controller's naming pattern using utils package
func IsManagedRule(ruleName string) bool {
	return utils.IsManagedRule(ruleName)
}

// ExtractServiceKeyFromRuleName extracts service key from rule name using utils package
func ExtractServiceKeyFromRuleName(ruleName string) string {
	return utils.ExtractServiceKeyFromRuleName(ruleName)
}

// GetUsedExternalPorts returns a copy of the used external ports map using utils package
func GetUsedExternalPorts() map[int]string {
	return utils.GetUsedExternalPorts()
}

// GetServicePortByName returns the port config for a given port name using utils package
func GetServicePortByName(service *v1.Service, portName string) *v1.ServicePort {
	return utils.GetServicePortByName(service, portName)
}

// isManagedRule checks if a rule follows controller's naming pattern using utils package (unexported for tests)
func isManagedRule(ruleName string) bool {
	return utils.IsManagedRuleUnexported(ruleName)
}

// extractServiceKeyFromRuleName extracts service key from rule name using utils package (unexported for tests)
func extractServiceKeyFromRuleName(ruleName string) string {
	return utils.ExtractServiceKeyFromRuleNameUnexported(ruleName)
}

// IsPortForwardRuleCRDAvailable checks if a PortForwardRule CRD is available using utils package
func IsPortForwardRuleCRDAvailable(ctx context.Context, restConfig *rest.Config, scheme *runtime.Scheme) bool {
	return utils.IsPortForwardRuleCRDAvailable(ctx, restConfig, scheme)
}

// Port conflict tracking functions - delegates to utils package

// CheckPortConflict checks if a port conflicts with existing ports using utils package
func CheckPortConflict(externalPort int, serviceKey string) error {
	return utils.CheckPortConflict(externalPort, serviceKey)
}

// markPortUsed marks a port as used by a specific service using utils package
func markPortUsed(externalPort int, serviceKey string) {
	// Access the unexported function through utils package by calling the exported MarkPortUsed
	utils.MarkPortUsed(externalPort, serviceKey)
}

// UnmarkPortsForService removes all port tracking for a specific service using utils package
func UnmarkPortsForService(serviceKey string) {
	utils.UnmarkPortsForService(serviceKey)
}
