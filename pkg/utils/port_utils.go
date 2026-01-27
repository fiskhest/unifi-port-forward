package utils

import (
	"fmt"
	"sync"
)

// Port conflict detection and tracking
var (
	usedExternalPorts = make(map[int]string) // port -> serviceKey
	portMutex         sync.RWMutex
)

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

// MarkPortUsed marks an external port as used by a service (exported)
func MarkPortUsed(externalPort int, serviceKey string) {
	markPortUsed(externalPort, serviceKey)
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
