package cleaner

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"kube-router-port-forward/routers"
)

// Config holds cleaner configuration
type Config struct {
	Host     string
	Username string
	Password string
	Site     string
	APIKey   string
}

// Run executes cleaner with given configuration and port mappings
func Run(cfg Config, portMaps map[string]string) error {
	// Validate port maps is not empty
	if len(portMaps) == 0 {
		return fmt.Errorf("port mappings cannot be empty")
	}

	// Create router
	router, err := routers.CreateUnifiRouter(cfg.Host, cfg.Username, cfg.Password, cfg.Site, cfg.APIKey)
	if err != nil {
		return fmt.Errorf("failed to create router: %w", err)
	}

	// List all port forwards
	portforwards, err := router.ListAllPortForwards(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list port forward rules: %w", err)
	}

	// Delete each rule
	for dstPort, dstIP := range portMaps {
		for _, pf := range portforwards {
			fmt.Printf("Checking rule: %+v\n", pf)
			if pf.FwdPort == dstPort && pf.Fwd == dstIP {
				fmt.Println("port matched")

				// Convert external port from string to int
				extPort, err := strconv.Atoi(pf.DstPort)
				if err != nil {
					log.Printf("failed to parse external port %s: %v", pf.DstPort, err)
					continue
				}

				// Construct PortConfig for removal
				portConfig := routers.PortConfig{
					Name:     pf.Name,
					Enabled:  pf.Enabled,
					DstPort:  extPort, // External port (what users connect to)
					Protocol: pf.Proto,
					DstIP:    pf.Fwd,
				}

				err = router.RemovePort(context.Background(), portConfig)
				if err != nil {
					log.Printf("failed to delete port-forward rule %s: %v", pf.ID, err)
				} else {
					log.Printf("deleted port-forward rule %s successfully", pf.ID)
				}
			}
		}
	}

	return nil
}
