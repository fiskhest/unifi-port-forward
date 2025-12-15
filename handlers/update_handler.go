package handlers

import (
	"fmt"
	"log"

	v1 "k8s.io/api/core/v1"
	"kube-router-port-forward/routers"
)

// handleUpdate implements the service update logic
func (h *serviceHandler) handleUpdate(oldService, newService *v1.Service) {
	// Cache IPs at the beginning to avoid race conditions
	oldLBIP := GetLBIP(oldService)
	newLBIP := GetLBIP(newService)

	serviceKey := h.getServiceKey(newService)

	if newService.Spec.Type != "LoadBalancer" {
		log.Printf("Service %s is not a Load Balancer.. Skipping\n", newService.Name)
		return
	}

	_, oldExists := oldService.Annotations[h.filterAnnotation]
	_, newExists := newService.Annotations[h.filterAnnotation]

	fmt.Printf("update_handler: called for %s\n", serviceKey)
	fmt.Printf("update_handler: Old LB IP: %s, New LB IP: %s\n", oldLBIP, newLBIP)

	// Skip updates if both IPs are node IPs (transient)
	if h.shouldSkipUpdate(serviceKey, oldLBIP, newLBIP) {
		return
	}

	// Handle annotation removal
	if oldExists && !newExists {
		fmt.Printf("Service %s: Annotation removed, cleaning up all ports\n", serviceKey)
		h.handleAnnotationRemoval(oldService)
		h.updateDebounceTimestamp(serviceKey)
		return
	}

	// Handle annotation addition
	if !oldExists && newExists {
		fmt.Printf("Service %s: Annotation added, adding all ports\n", serviceKey)
		h.handleAnnotationAddition(newService)
		h.updateDebounceTimestamp(serviceKey)
		return
	}

	// Handle port changes when both have annotations
	if oldExists && newExists {
		fmt.Printf("Service %s: Both services have annotations, comparing ports\n", serviceKey)
		h.handlePortComparison(oldService, newService)
		h.updateDebounceTimestamp(serviceKey)
	}
}

// handleAnnotationRemoval removes all ports when annotation is removed
func (h *serviceHandler) handleAnnotationRemoval(oldService *v1.Service) {
	serviceKey := h.getServiceKey(oldService)

	oldPortConfigs, err := GetPortConfigs(oldService, h.filterAnnotation)
	if err != nil {
		h.logError("Failed to get old port configurations", err, oldService)
		return
	}

	successCount := 0
	for _, pc := range oldPortConfigs {
		fmt.Printf("Removing port %d for service %s (annotation removed)\n", pc.DstPort, serviceKey)

		if err := h.router.RemovePort(h.ctx, pc); err != nil {
			h.logError(fmt.Sprintf("Removing port %d after annotation removal", pc.DstPort), err, oldService)
			continue
		}

		unmarkPortUsed(pc.DstPort)
		fmt.Printf("Port %d: Successfully removed port forward rule\n", pc.DstPort)
		successCount++
	}

	fmt.Printf("Service %s: Successfully removed %d/%d ports after annotation removal\n", serviceKey, successCount, len(oldPortConfigs))
}

// handleAnnotationAddition adds all ports when annotation is added
func (h *serviceHandler) handleAnnotationAddition(newService *v1.Service) {
	// This is essentially the same as handleAdd logic
	h.handleAdd(newService)
}

// handlePortComparison compares old and new port configurations
func (h *serviceHandler) handlePortComparison(oldService, newService *v1.Service) {
	serviceKey := h.getServiceKey(newService)

	oldPortConfigs, err := GetPortConfigs(oldService, h.filterAnnotation)
	if err != nil {
		h.logError("Failed to get old port configurations", err, oldService)
		return
	}

	newPortConfigs, err := GetPortConfigs(newService, h.filterAnnotation)
	if err != nil {
		h.logError("Failed to get new port configurations", err, newService)
		return
	}

	// Create maps for easier comparison
	oldPorts := make(map[int]routers.PortConfig)
	newPorts := make(map[int]routers.PortConfig)

	for _, pc := range oldPortConfigs {
		oldPorts[pc.DstPort] = pc
	}

	for _, pc := range newPortConfigs {
		newPorts[pc.DstPort] = pc
	}

	// Handle port additions (ports in new but not in old)
	for externalPort, newPc := range newPorts {
		if _, exists := oldPorts[externalPort]; !exists {
			fmt.Printf("Port %d: Added to service %s\n", externalPort, serviceKey)

			if err := h.router.AddPort(h.ctx, newPc); err != nil {
				h.logError(fmt.Sprintf("Adding new port %d", externalPort), err, newService)
				continue
			}

			markPortUsed(externalPort, serviceKey)
			fmt.Printf("Port %d: Successfully added new port forward rule\n", externalPort)
		}
	}

	// Handle port removals (ports in old but not in new)
	for externalPort, oldPc := range oldPorts {
		if _, exists := newPorts[externalPort]; !exists {
			fmt.Printf("Port %d: Removed from service %s\n", externalPort, serviceKey)

			if err := h.router.RemovePort(h.ctx, oldPc); err != nil {
				h.logError(fmt.Sprintf("Removing port %d", externalPort), err, newService)
				continue
			}

			unmarkPortUsed(externalPort)
			fmt.Printf("Port %d: Successfully removed port forward rule\n", externalPort)
		}
	}

	// Handle port updates (ports in both old and new)
	for externalPort, newPc := range newPorts {
		if oldPc, exists := oldPorts[externalPort]; exists {
			// Check if IP or other properties changed
			if oldPc.DstIP != newPc.DstIP || oldPc.FwdPort != newPc.FwdPort {
				fmt.Printf("Port %d: Configuration changed for service %s\n", externalPort, serviceKey)

				if err := h.router.UpdatePort(h.ctx, externalPort, newPc); err != nil {
					h.logError(fmt.Sprintf("Updating port %d", externalPort), err, newService)
					continue
				}

				fmt.Printf("Port %d: Successfully updated port forward rule\n", externalPort)
			} else {
				fmt.Printf("Port %d: No changes needed for service %s\n", externalPort, serviceKey)
			}
		}
	}
}
