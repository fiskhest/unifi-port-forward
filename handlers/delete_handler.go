package handlers

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
)

// handleDelete implements the service deletion logic
func (h *serviceHandler) handleDelete(service *v1.Service) {
	if service.Spec.Type != "LoadBalancer" {
		return
	}

	// Check for port annotation - skip if not present
	if _, exists := service.Annotations[h.filterAnnotation]; !exists {
		return
	}

	serviceKey := h.getServiceKey(service)
	fmt.Printf("Deleted service %s\n", serviceKey)

	// Get all port configs from annotation
	portConfigs, err := GetPortConfigs(service, h.filterAnnotation)
	if err != nil {
		h.logError("Failed to get port configurations", err, service)
		return
	}

	successCount := 0

	// Remove each port individually
	for _, pc := range portConfigs {
		fmt.Printf("Removing port %d for service %s\n", pc.DstPort, serviceKey)

		if err := h.router.RemovePort(h.ctx, pc); err != nil {
			h.logError(fmt.Sprintf("Trying to remove port %d", pc.DstPort), err, service)
			continue // Continue with other ports
		}

		// Clean up port tracking
		unmarkPortUsed(pc.DstPort)
		fmt.Printf("Port %d: Successfully removed port forward rule\n", pc.DstPort)
		successCount++
	}

	fmt.Printf("Service %s: Successfully removed %d/%d ports\n", serviceKey, successCount, len(portConfigs))
}
