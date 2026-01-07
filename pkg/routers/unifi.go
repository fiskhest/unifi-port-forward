package routers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/filipowm/go-unifi/unifi"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// PortForward aliases unifi.PortForward for external access
// type PortForward = unifi.PortForward

type UnifiRouter struct {
	SiteID string
	Client unifi.Client
}

func CreateUnifiRouter(baseURL, username, password, site, apiKey string) (*UnifiRouter, error) {
	clientConfig := &unifi.ClientConfig{
		URL:            baseURL,
		VerifySSL:      false,
		ValidationMode: unifi.HardValidation,
		User:           username,
		Password:       password,
		RememberMe:     true,
	}

	// override if using API key (recommended, requires UniFi Controller 9.0.108+)
	if apiKey != "" {
		clientConfig.APIKey = apiKey
		clientConfig.User = ""
		clientConfig.Password = ""
	}

	client, err := unifi.NewClient(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	err = client.Login()
	if err != nil {
		return nil, err
	}

	fmt.Printf("UniFi Controller Version: %s\n", client.Version())

	router := &UnifiRouter{
		SiteID: site,
		Client: client,
	}

	return router, nil
}

// withAuthRetry executes a function with automatic authentication retry on 401 errors
func (router *UnifiRouter) withAuthRetry(ctx context.Context, operation string, fn func() error) error {
	logger := ctrllog.FromContext(ctx)

	err := fn()
	if err != nil {
		if serverErr, ok := err.(*unifi.ServerError); ok && serverErr.StatusCode == http.StatusUnauthorized {
			logger.Info("Authentication failure detected, reauthenticating", "operation", operation)
			if loginErr := router.Client.Login(); loginErr == nil {
				err = fn()
			}
		}
		if err != nil {
			logger.Error(err, "Operation failed after authentication retry", "operation", operation)
		}
	}
	return err
}

func (router *UnifiRouter) CheckPort(ctx context.Context, port int) (*unifi.PortForward, bool, error) {
	logger := ctrllog.FromContext(ctx)

	var portforwards []unifi.PortForward
	err := router.withAuthRetry(ctx, "CheckPort", func() error {
		var err error
		portforwards, err = router.Client.ListPortForward(ctx, router.SiteID)
		return err
	})
	if err != nil {
		logger.Error(err, "Failed to list port forwards during CheckPort",
			"site_id", router.SiteID,
			"searched_port", port,
		)
		return &unifi.PortForward{}, false, err
	}

	// Process each rule
	for _, portforward := range portforwards {
		portNum, parseErr := strconv.Atoi(portforward.DstPort)

		if parseErr != nil {
			continue
		}

		if portNum == port {
			logger.Info("Found matching port forward rule",
				"port", port,
				"rule_id", portforward.ID,
				"rule_name", portforward.Name,
				"destination_ip", portforward.Fwd,
				"protocol", portforward.Proto)
			return &portforward, true, nil
		}
	}

	logger.Info("Port forward rule not found",
		"searched_port", port,
		"total_rules_checked", len(portforwards),
		"available_ports", router.getAvailablePorts(portforwards))

	return &unifi.PortForward{}, false, nil
}

func (router *UnifiRouter) AddPort(ctx context.Context, config PortConfig) error {
	logger := ctrllog.FromContext(ctx)

	logger.Info("Creating new port forward rule",
		"operation", "add_port",
		"config_name", config.Name,
		"dst_port", config.DstPort,
		"fwd_port", config.FwdPort,
		"dst_ip", config.DstIP,
		"protocol", config.Protocol,
		"interface", config.Interface,
		"enabled", config.Enabled,
	)

	if config.DstIP == "" {
		err := fmt.Errorf("forward IP was empty - I don't want to create such a rule")
		logger.Error(err, "Failed validation: destination IP is empty",
			"config", config,
		)
		return err
	}

	portforward := &unifi.PortForward{
		SiteID:        router.SiteID,
		DestinationIP: "any",
		Enabled:       config.Enabled,
		Fwd:           config.DstIP,
		FwdPort:       strconv.Itoa(config.FwdPort),
		DstPort:       strconv.Itoa(config.DstPort),
		Name:          config.Name,
		PfwdInterface: config.Interface,
		Proto:         config.Protocol,
		Src:           "any",
	}

	logger.V(1).Info("Sending port forward creation to UniFi API",
		"creation_payload", portforward,
	)

	var result *unifi.PortForward
	err := router.withAuthRetry(ctx, "AddPort", func() error {
		var err error
		result, err = router.Client.CreatePortForward(ctx, router.SiteID, portforward)
		return err
	})
	if err != nil {
		logger.Error(err, "Failed to create port forward rule via UniFi API",
			"config", config,
			"creation_payload", portforward,
		)
		return err
	}

	logger.Info("Successfully created port forward rule",
		"dst_port", config.DstPort,
		"rule_name", config.Name,
		"result", result,
	)

	return nil
}

func (router *UnifiRouter) UpdatePort(ctx context.Context, port int, config PortConfig) error {
	logger := ctrllog.FromContext(ctx)

	logger.Info("Starting port forward rule update",
		"port", port,
		"operation", "update_port",
		"config_name", config.Name,
		"config_dst_ip", config.DstIP,
		"config_protocol", config.Protocol,
		"config_fwd_port", config.FwdPort,
		"config_enabled", config.Enabled,
	)

	pf, portExists, err := router.CheckPort(ctx, port)
	if err != nil {
		logger.Error(err, "Failed to check port during update operation",
			"port", port,
		)
		return err
	}

	if !portExists {
		err := fmt.Errorf("port forward rule for port %d not found - cannot update non-existent rule. Available ports may differ from expected. Consider creating rule first", port)
		logger.Error(err, "Port forward rule not found for update",
			"port", port,
			"searched_port", port,
			"config", config)
		return err
	}

	logger.V(1).Info("Found existing port forward rule to update",
		"port", port,
		"rule_id", pf.ID,
		"current_destination_ip", pf.Fwd,
		"new_destination_ip", config.DstIP,
		"current_name", pf.Name,
		"new_name", config.Name,
	)

	portforward := &unifi.PortForward{
		ID:            pf.ID,
		SiteID:        router.SiteID,
		DestinationIP: "any",
		Enabled:       config.Enabled,
		Fwd:           config.DstIP,
		FwdPort:       strconv.Itoa(config.FwdPort),
		DstPort:       strconv.Itoa(config.DstPort),
		Name:          config.Name,
		PfwdInterface: config.Interface,
		Proto:         config.Protocol,
		Src:           "any",
	}

	var result *unifi.PortForward
	err = router.withAuthRetry(ctx, "UpdatePort", func() error {
		var retryErr error
		result, retryErr = router.Client.UpdatePortForward(ctx, router.SiteID, portforward)
		return retryErr
	})
	if err != nil {
		logger.Error(err, "Failed to update port forward rule via UniFi API",
			"port", port,
			"rule_id", pf.ID,
			"update_payload", portforward,
		)
		return fmt.Errorf("failed to update port forward rule for port %d: %w", port, err)
	}

	logger.Info("Successfully updated port forward rule",
		"port", port,
		"rule_id", pf.ID,
		"result", result,
		"new_destination_ip", config.DstIP,
		"new_name", config.Name,
	)

	return nil
}

func (router *UnifiRouter) DeletePortForwardByID(ctx context.Context, ruleID string) error {
	err := router.withAuthRetry(ctx, "DeletePortForwardByID", func() error {
		return router.Client.DeletePortForward(ctx, router.SiteID, ruleID)
	})
	return err
}

func (router *UnifiRouter) ListAllPortForwards(ctx context.Context) ([]*unifi.PortForward, error) {
	var portforwards []unifi.PortForward
	err := router.withAuthRetry(ctx, "ListAllPortForwards", func() error {
		var err error
		portforwards, err = router.Client.ListPortForward(ctx, router.SiteID)
		return err
	})
	if err != nil {
		return nil, err
	}

	var result []*unifi.PortForward
	for i := range portforwards {
		result = append(result, &portforwards[i])
	}

	return result, nil
}

func (router *UnifiRouter) RemovePort(ctx context.Context, config PortConfig) error {
	pf, portExists, err := router.CheckPort(ctx, config.DstPort)
	if err != nil {
		return err
	}

	if portExists {
		err := router.withAuthRetry(ctx, "RemovePort", func() error {
			return router.Client.DeletePortForward(ctx, router.SiteID, pf.ID)
		})
		if err != nil {
			return fmt.Errorf("deleting port-forward rule %s: %v", pf.ID, err)
		}
	}
	return nil
}

// getAvailablePorts extracts available port numbers from list of port forwards
func (router *UnifiRouter) getAvailablePorts(portforwards []unifi.PortForward) []string {
	var ports []string
	for _, pf := range portforwards {
		if pf.DstPort != "" {
			ports = append(ports, fmt.Sprintf("%s/%s", pf.DstPort, pf.Proto))
		}
	}
	return ports
}
