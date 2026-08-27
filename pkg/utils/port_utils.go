package utils

import (
	"fmt"
	"maps"
	"sync"

	"unifi-port-forward/pkg/ports"
)

// Port conflict detection and tracking.
//
// Tracking is per individual port rather than per spec: a rule claiming
// 8000-8100 marks all 101 ports. That way a later rule asking for 8050 - or for
// an overlapping range - is caught by a plain map lookup, with no interval
// arithmetic at the call site.
var (
	usedExternalPorts = make(map[int]string) // port -> serviceKey
	portMutex         sync.RWMutex
)

// CheckPortConflict checks if any of the external ports are already used by another service
func CheckPortConflict(externalPorts ports.Spec, serviceKey string) error {
	portMutex.Lock()
	defer portMutex.Unlock()

	for _, port := range externalPorts.All() {
		if existingService, exists := usedExternalPorts[port]; exists {
			if existingService != serviceKey {
				return fmt.Errorf("external port %d already used by service %s", port, existingService)
			}
		}
	}
	return nil
}

// markPortsUsed marks external ports as used by a service
func markPortsUsed(externalPorts ports.Spec, serviceKey string) {
	portMutex.Lock()
	defer portMutex.Unlock()

	for _, port := range externalPorts.All() {
		usedExternalPorts[port] = serviceKey
	}
}

// MarkPortUsed marks external ports as used by a service (exported)
func MarkPortUsed(externalPorts ports.Spec, serviceKey string) {
	markPortsUsed(externalPorts, serviceKey)
}

// UnmarkPortUsed removes external ports from tracking (exported for use by controller)
// This function is called during service deletion to free up external ports for reuse
func UnmarkPortUsed(externalPorts ports.Spec) {
	portMutex.Lock()
	defer portMutex.Unlock()

	for _, port := range externalPorts.All() {
		delete(usedExternalPorts, port)
	}
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
	return maps.Clone(usedExternalPorts)
}

// GetPortMutex returns the port mutex for external coordination
// Exported for controller to safely access port tracking state
func GetPortMutex() *sync.RWMutex {
	return &portMutex
}
