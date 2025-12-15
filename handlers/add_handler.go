package handlers

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
)

// handleAdd implements the service addition logic
func (h *serviceHandler) handleAdd(service *v1.Service) {
	if service.Spec.Type != "LoadBalancer" {
		return
	}

	// Check for port annotation - skip if not present
	if _, exists := service.Annotations[h.filterAnnotation]; !exists {
		return
	}

	fmt.Printf("Load Balancer found: %s/%s\n", service.Namespace, service.Name)

	// Get all port configs from annotation
	portConfigs, err := GetPortConfigs(service, h.filterAnnotation)
	if err != nil {
		h.logError("Failed to get port configurations", err, service)
		return
	}

	// Check if service IP is available
	svcAddress := GetLBIP(service)
	if svcAddress == "" {
		h.logError("Service has no LoadBalancer IP", fmt.Errorf("empty IP address"), service)
		return
	}

	serviceKey := h.getServiceKey(service)
	successCount := 0

	// Process each port individually
	for _, pc := range portConfigs {
		fmt.Printf("Processing port %d for service %s\n", pc.DstPort, serviceKey)

		pf, portExists, err := h.router.CheckPort(h.ctx, pc.DstPort)
		if err != nil {
			h.logError(fmt.Sprintf("Error checking port %d", pc.DstPort), err, service)
			continue // Continue with other ports
		}

		if portExists {
			if pf.Fwd == svcAddress {
				fmt.Printf("Port %d: Found matching record: %s\n", pc.DstPort, pf.Name)
				markPortUsed(pc.DstPort, serviceKey)
				successCount++
				continue
			}

			// Update existing rule with new IP
			if err := h.router.UpdatePort(h.ctx, pc.DstPort, pc); err != nil {
				h.logError(fmt.Sprintf("Updating existing port forward rule %d", pc.DstPort), err, service)
				continue
			}
			fmt.Printf("Port %d: Successfully updated existing port forward rule\n", pc.DstPort)
		} else {
			// Add new rule
			fmt.Printf("add_handler: %s/%s -- Add IP %s port: %d to router\n", service.Namespace, service.Name, svcAddress, pc.DstPort)

			if err := h.router.AddPort(h.ctx, pc); err != nil {
				h.logError(fmt.Sprintf("Trying to add port %d", pc.DstPort), err, service)
				continue
			}
			fmt.Printf("Port %d: Successfully added new port forward rule\n", pc.DstPort)
		}

		markPortUsed(pc.DstPort, serviceKey)
		successCount++
	}

	fmt.Printf("Service %s: Successfully processed %d/%d ports\n", serviceKey, successCount, len(portConfigs))
}
