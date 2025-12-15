package handlers

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"kube-router-port-forward/routers"
)

// handleDelete implements the service deletion logic
func (h *serviceHandler) handleDelete(service *v1.Service) {
	if service.Spec.Type != "LoadBalancer" {
		return
	}

	if _, exists := service.Annotations[h.filterAnnotation]; exists {
		serviceIP := GetLBIP(service)
		port := int(service.Spec.Ports[0].Port)
		fmt.Printf("Deleted service %s/%s", service.Namespace, service.Name)
		fmt.Printf("Remove ip: %s port: %d from router\n", serviceIP, port)

		// TODO: Loop through _all_ ports on a service and remove them
		pc := routers.PortConfig{
			Name:      service.Name,
			DstPort:   port,
			Enabled:   true,
			Interface: "wan",
			SrcIP:     "any",
			DstIP:     serviceIP,
			Protocol:  strings.ToLower(string(service.Spec.Ports[0].Protocol)),
		}

		err := h.router.RemovePort(h.ctx, pc)
		if err != nil {
			h.logError("Trying to remove port", err, service)
			return
		}

		fmt.Printf("Successfully removed port forward rule for port %d\n", port)
		return
	}
}
