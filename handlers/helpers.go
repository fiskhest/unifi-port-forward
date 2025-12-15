package handlers

import (
	"fmt"
	// "strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"kube-router-port-forward/routers"
)

// GetLBIP extracts the LoadBalancer IP from a service
func GetLBIP(service *v1.Service) string {
	fmt.Printf("DEBUG: getLBIP called for service %s/%s\n", service.Namespace, service.Name)

	// Only use status.loadBalancer.ingress for LoadBalancer services
	// Filter out node IPs and only use VIPs
	fmt.Printf("DEBUG: LoadBalancer Ingress count: %d\n", len(service.Status.LoadBalancer.Ingress))
	if len(service.Status.LoadBalancer.Ingress) > 0 {
		for i, ingress := range service.Status.LoadBalancer.Ingress {
			fmt.Printf("DEBUG: Ingress[%d]: IP=%s, Hostname=%s, IPMode=%s\n", i, ingress.IP, ingress.Hostname, getIPMode(ingress))
		}

		// Prefer VIP mode IPs (most stable for LoadBalancer)
		for _, ingress := range service.Status.LoadBalancer.Ingress {
			if ingress.IP != "" && isVIPIngress(ingress) {
				fmt.Printf("DEBUG: Using VIP IP: %s\n", ingress.IP)
				return ingress.IP
			}
		}

		// Fallback to any IP if no VIP found
		for _, ingress := range service.Status.LoadBalancer.Ingress {
			if ingress.IP != "" {
				fmt.Printf("DEBUG: Using fallback IP: %s\n", ingress.IP)
				return ingress.IP
			}
		}
	}

	fmt.Printf("DEBUG: Service %s has no LoadBalancer IP\n", service.Name)
	return ""
}

// isVIPIngress checks if ingress is VIP mode (stable LoadBalancer IP)
func isVIPIngress(ingress v1.LoadBalancerIngress) bool {
	// Check if it's likely a VIP by IP range or mode
	if ingress.IP != "" {
		// MetalLB VIPs are typically in specific ranges
		// For your case, 192.168.72.1 is VIP, 192.168.27.130 is a node IP
		// This is a heuristic - adjust based on your network
		return !isNodeIP(ingress.IP)
	}
	return false
}

// isNodeIP detects node IPs vs VIP IPs
func isNodeIP(ip string) bool {
	// Add logic to identify node IPs vs LoadBalancer VIPs
	// This is network-specific - adjust for your environment
	nodeIPRanges := []string{
		"192.168.27.", // Your node IP range
		// Add other node IP ranges as needed
	}

	for _, rangePrefix := range nodeIPRanges {
		if len(ip) >= len(rangePrefix) && ip[:len(rangePrefix)] == rangePrefix {
			return true
		}
	}
	return false
}

// getIPMode gets IP mode from ingress
func getIPMode(ingress v1.LoadBalancerIngress) string {
	if ingress.IPMode != nil {
		return string(*ingress.IPMode)
	}
	return "unknown"
}

// getPortConfigs creates multiple PortConfigs from a service (supports multiple ports)
// func getPortConfigs(service *v1.Service) []routers.PortConfig {
// 	var configs []routers.PortConfig

// 	for _, port := range service.Spec.Ports {
// 		protocol := strings.ToLower(string(port.Protocol))

// 		// Parse annotation for custom source port mapping
// 		srcPort := int(port.Port) // Default to same as service port
// 		if annotation := service.Annotations["kube-port-forward-controller/ports"]; annotation != "" {
// 			// Parse annotation like "80:8080,81:8081" or just "8080"
// 			if mappings := parsePortMapping(annotation); len(mappings) > 0 {
// 				// Find mapping for this port
// 				for _, mapping := range mappings {
// 					if mapping.dstPort == int(port.Port) {
// 						srcPort = mapping.srcPort
// 						break
// 					}
// 				}
// 			}
// 		}

// 		configs = append(configs, routers.PortConfig{
// 			Name:      service.Name,
// 			SrcPort:   srcPort,        // External port
// 			DstPort:   int(port.Port), // Internal port
// 			Enabled:   true,
// 			Interface: "wan",
// 			DstIP:     GetLBIP(service),
// 			SrcIP:     "any",
// 			Protocol:  protocol,
// 		})
// 	}

// 	return configs
// }

// getPortConfig creates a single PortConfig from a service (for backward compatibility)
func getPortConfig(service *v1.Service) routers.PortConfig {
	// TODO: update here, we should loop portConfig and test if tcp, udp or tcp_udp
	protocol := "tcp"
	if len(service.Spec.Ports) > 0 {
		protocol = strings.ToLower(string(service.Spec.Ports[0].Protocol))
	}
	return routers.PortConfig{
		Name:      service.Name,
		DstPort:   int(service.Spec.Ports[0].Port),
		Enabled:   true,
		Interface: "wan",
		DstIP:     GetLBIP(service),
		SrcIP:     "any",
		Protocol:  protocol,
	}
}

// portMapping represents source:destination port mapping
// type portMapping struct {
// 	srcPort int
// 	dstPort int
// }

// // parsePortMapping parses annotation like "80:8080,81:8081" or "8080"
// func parsePortMapping(annotation string) []portMapping {
// 	var mappings []portMapping

// 	// Simple case: just "8080" (use as both src and dst)
// 	if !strings.Contains(annotation, ":") {
// 		if port, err := strconv.Atoi(annotation); err == nil {
// 			mappings = append(mappings, portMapping{srcPort: port, dstPort: port})
// 		}
// 		return mappings
// 	}

// 	// Complex case: "80:8080,81:8081"
// 	parts := strings.Split(annotation, ",")
// 	for _, part := range parts {
// 		portParts := strings.Split(strings.TrimSpace(part), ":")
// 		if len(portParts) == 2 {
// 			if srcPort, err1 := strconv.Atoi(strings.TrimSpace(portParts[0])); err1 == nil {
// 				if dstPort, err2 := strconv.Atoi(strings.TrimSpace(portParts[1])); err2 == nil {
// 					mappings = append(mappings, portMapping{srcPort: srcPort, dstPort: dstPort})
// 				}
// 			}
// 		}
// 	}

// 	return mappings
// }
