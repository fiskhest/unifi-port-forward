package handlers

import (
	"fmt"
	"log"
	"strings"

	v1 "k8s.io/api/core/v1"
	"kube-router-port-forward/routers"
)

// handleUpdate implements the service update logic
func (h *serviceHandler) handleUpdate(oldService, newService *v1.Service) {
	// Cache IPs at the beginning to avoid race conditions
	oldLBIP := GetLBIP(oldService)
	newLBIP := GetLBIP(newService)

	serviceKey := h.getServiceKey(newService)

	fmt.Printf("update_handler: called for %s\n", serviceKey)
	fmt.Printf("update_handler: Old LB IP: %s, New LB IP: %s\n", oldLBIP, newLBIP)

	// Skip updates if both IPs are node IPs (transient)
	if h.shouldSkipUpdate(serviceKey, oldLBIP, newLBIP) {
		return
	}

	if newService.Spec.Type != "LoadBalancer" {
		log.Printf("Service %s is not a Load Balancer.. Skipping\n", newService.Name)
		return
	}

	_, oldExists := oldService.Annotations[h.filterAnnotation]
	_, newExists := newService.Annotations[h.filterAnnotation]

	oldPort := int(oldService.Spec.Ports[0].Port)
	newPort := int(newService.Spec.Ports[0].Port)

	// If service was unannotated, remove all ports
	if oldExists && !newExists {
		fmt.Printf("Remove port %d from router\n", oldService.Spec.Ports[0].Port)
		pc := routers.PortConfig{
			Name:      oldService.Name,
			DstPort:   oldPort,
			Enabled:   true,
			Interface: "wan",
			DstIP:     GetLBIP(oldService),
			Protocol:  strings.ToLower(string(oldService.Spec.Ports[0].Protocol)),
		}
		err := h.router.RemovePort(h.ctx, pc)
		if err != nil {
			h.logError("Trying to delete service after annotation removal", err, oldService)
		}
		h.updateDebounceTimestamp(serviceKey)
		return
	}
	// If the service was annotated, add the port
	if !oldExists && newExists {
		fmt.Printf("Update Detected %s\n", newService.Name)
		fmt.Printf("Add port %d to router", newPort)
		pc := getPortConfig(newService)
		err := h.router.AddPort(h.ctx, pc)
		if err != nil {
			h.logError("Error trying to add port", err, newService)
		}

		h.updateDebounceTimestamp(serviceKey)
		return
	}

	// If the old service and new service have a port
	if oldExists && newExists {
		if oldPort != newPort {
			// Port changed: find existing rule and update it
			fmt.Printf("UpdateFunc: Port changing from %d→%d for service %s/%s (IP: %s)\n", oldPort, newPort, newService.Namespace, newService.Name, newLBIP)

			_, portExists, err := h.router.CheckPort(h.ctx, oldPort)
			if err != nil {
				h.logError("Error checking port", err, newService)
				return
			}

			if portExists {
				// Update existing rule with new port
				pc := getPortConfig(newService)
				pc.DstPort = newPort
				err := h.router.UpdatePort(h.ctx, oldPort, pc)
				if err != nil {
					h.logError("Error updating port forward rule", err, newService)
					return
				}
				fmt.Printf("Successfully updated port forward rule from %d to %d\n", oldPort, newPort)
				h.updateDebounceTimestamp(serviceKey)
			} else {
				// No existing rule found, just add new one
				fmt.Printf("UpdateFunc: No existing rule found for port %d, adding new rule\n", oldPort)
				pc := getPortConfig(newService)
				err := h.router.AddPort(h.ctx, pc)
				if err != nil {
					h.logError("Error adding new port forward rule", err, newService)
				} else {
					fmt.Printf("Successfully added new port forward rule for port %d\n", newPort)
					h.updateDebounceTimestamp(serviceKey)
				}
			}
			return
		}

		// Port didn't change, but we should ensure rule exists and has correct IP
		// This handles cases where rule might have been manually deleted or IP changed
		fmt.Printf("DEBUG: UpdateFunc - Port unchanged (%d), ensuring rule exists for ip: %s (service: %s/%s)\n", newPort, newLBIP, newService.Namespace, newService.Name)
		pf, portExists, err := h.router.CheckPort(h.ctx, newPort)
		if err != nil {
			h.logError("Error checking port", err, newService)
			return
		}

		if !portExists {
			// Rule doesn't exist, add it
			pc := getPortConfig(newService)
			err := h.router.AddPort(h.ctx, pc)
			if err != nil {
				h.logError("Error adding missing port forward rule", err, newService)
			} else {
				fmt.Printf("Successfully added missing port forward rule for port %d\n", newPort)
				h.updateDebounceTimestamp(serviceKey)
			}
		} else {
			// Rule exists, check if IP needs updating
			if pf.Fwd != newLBIP {
				// IP changed, update the existing rule
				fmt.Printf("UpdateFunc: IP changed from %s to %s, updating rule for port %d\n", pf.Fwd, newLBIP, newPort)

				pc := getPortConfig(newService)
				err := h.router.UpdatePort(h.ctx, newPort, pc)
				if err != nil {
					h.logError("Error updating port forward rule", err, newService)
				} else {
					fmt.Printf("Successfully updated port forward rule for port %d with new IP\n", newPort)
					h.updateDebounceTimestamp(serviceKey)
				}
			} else {
				fmt.Printf("UpdateFunc: Port %d rule already exists with correct IP, no action needed\n", newPort)
			}
		}

		if !portExists {
			// Rule doesn't exist, add it
			pc := getPortConfig(newService)
			err := h.router.AddPort(h.ctx, pc)
			if err != nil {
				h.logError("Error adding missing port forward rule", err, newService)
			} else {
				fmt.Printf("Successfully added missing port forward rule for port %d\n", newPort)
				h.updateDebounceTimestamp(serviceKey)
			}
		} else {
			fmt.Printf("UpdateFunc: Port %d rule already exists, no action needed\n", newPort)
		}
	}
}
