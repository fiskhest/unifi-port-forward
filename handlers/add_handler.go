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

	if _, exists := service.Annotations[h.filterAnnotation]; exists {
		fmt.Printf("Load Balancer found: %s/%s\n", service.Namespace, service.Name)

		// TODO: Loop through _all_ ports on a service and add them
		svcAddress := GetLBIP(service)
		svcPort := int(service.Spec.Ports[0].Port)

		pf, portExists, err := h.router.CheckPort(h.ctx, svcPort)
		if err != nil {
			h.logError("Error checking port", err, service)
			return
		}

		if portExists {
			if pf.Fwd == svcAddress {
				// Existing IP:Port Forward policy already exists
				fmt.Println("Found matching record:", pf.Name)
				return
			}
			// Update existing rule with new IP
			pc := getPortConfig(service)
			if err := h.router.UpdatePort(h.ctx, svcPort, pc); err != nil {
				h.logError("Updating existing port forward rule", err, service)
				return
			}
			fmt.Printf("Successfully updated existing port forward rule for port %d\n", svcPort)
			return
		}

		fmt.Printf("add_handler: %s/%s -- Add IP %s port: %d to router\n", service.Namespace, service.Name, svcAddress, svcPort)
		pc := getPortConfig(service)

		err = h.router.AddPort(h.ctx, pc)
		if err != nil {
			h.logError("Trying to add port", err, service)
			return
		}

		fmt.Printf("Successfully added new port forward rule for port %d\n", svcPort)
	}
}
