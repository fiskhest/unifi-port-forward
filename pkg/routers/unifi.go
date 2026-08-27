package routers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"unifi-port-forward/pkg/ports"

	"github.com/filipowm/go-unifi/unifi"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// PortForward aliases unifi.PortForward for external access
// type PortForward = unifi.PortForward

type UnifiRouter struct {
	SiteID string
	Client unifi.Client

	// usesAPIKey records which credential the client was built with. API key
	// auth has no session to renew, so a 401 cannot be retried away.
	usesAPIKey bool
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
	//
	// User, Password and RememberMe must be cleared, not merely ignored: the
	// client validates them as `excluded_with=APIKey`, so leaving any of them set
	// fails client creation outright under HardValidation.
	usesAPIKey := apiKey != ""
	if usesAPIKey {
		clientConfig.APIKey = apiKey
		clientConfig.User = ""
		clientConfig.Password = ""
		clientConfig.RememberMe = false
	}

	client, err := unifi.NewClient(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	// A no-op for API key auth, which authenticates per request instead.
	err = client.Login()
	if err != nil {
		return nil, err
	}

	fmt.Printf("UniFi Controller Version: %s\n", client.Version())

	router := &UnifiRouter{
		SiteID:     site,
		Client:     client,
		usesAPIKey: usesAPIKey,
	}

	return router, nil
}

// withAuthRetry executes a function, renewing the session and retrying once if
// the router rejects it as unauthenticated.
//
// That recovery only applies to user/pass auth. An API key is sent on every
// request and Login is a no-op for it, so retrying a 401 would re-issue the same
// rejected request; the key itself is wrong, revoked, or lacks permission.
func (router *UnifiRouter) withAuthRetry(ctx context.Context, operation string, fn func() error) error {
	logger := ctrllog.FromContext(ctx)

	err := fn()
	if err == nil {
		return nil
	}

	if serverErr, ok := err.(*unifi.ServerError); ok && serverErr.StatusCode == http.StatusUnauthorized {
		if router.usesAPIKey {
			logger.Error(err, "Router rejected the API key",
				"operation", operation,
				"hint", "check that UNIFI_API_KEY is current and its admin has network write permission")
			return err
		}

		logger.Info("Renewing authentication to router", "operation", operation)
		if loginErr := router.Client.Login(); loginErr == nil {
			err = fn()
		}
	}

	if err != nil {
		logger.Error(err, "Operation failed after authentication retry", "operation", operation)
	}
	return err
}

func (router *UnifiRouter) CheckPort(ctx context.Context, port ports.Spec, protocol string) (*unifi.PortForward, bool, error) {
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
			"protocol", protocol,
		)
		return &unifi.PortForward{}, false, err
	}

	// Process each rule
	for _, portforward := range portforwards {
		rulePorts := ports.ParseOrEmpty(portforward.DstPort)
		if rulePorts.IsEmpty() {
			continue
		}

		// Match both port and protocol to ensure we find the correct rule
		if rulePorts.Equal(port) && strings.EqualFold(portforward.Proto, protocol) {
			logger.V(1).Info("Found matching port forward rule",
				"port", port,
				"protocol", protocol,
				"rule_id", portforward.ID,
				"rule_name", portforward.Name,
				"destination_ip", portforward.Fwd)
			return &portforward, true, nil
		}
	}

	logger.V(1).Info("Port forward rule not found",
		"searched_port", port,
		"protocol", protocol,
		"total_rules_checked", len(portforwards),
		"available_ports", router.getAvailablePorts(portforwards))

	return &unifi.PortForward{}, false, nil
}

func (router *UnifiRouter) AddPort(ctx context.Context, config PortConfig) error {
	logger := ctrllog.FromContext(ctx)

	logger.V(1).Info("Creating new port forward rule",
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

	if config.DstPort.IsEmpty() {
		err := fmt.Errorf("external port spec was empty - I don't want to create such a rule")
		logger.Error(err, "Failed validation: external port spec is empty",
			"config", config,
		)
		return err
	}

	portforward := &unifi.PortForward{
		SiteID:        router.SiteID,
		DestinationIP: "any",
		Enabled:       config.Enabled,
		Fwd:           config.DstIP,
		FwdPort:       config.FwdPort.String(),
		DstPort:       config.DstPort.String(),
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

	logger.V(1).Info("Successfully created port forward rule",
		"dst_port", config.DstPort,
		"rule_name", config.Name,
		"result", result,
	)

	return nil
}

func (router *UnifiRouter) UpdatePort(ctx context.Context, port ports.Spec, config PortConfig) error {
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

	pf, portExists, err := router.CheckPort(ctx, port, config.Protocol)
	if err != nil {
		logger.Error(err, "Failed to check port during update operation",
			"port", port,
		)
		return err
	}

	if !portExists {
		// Rule doesn't exist, log clear error and return for proper handling
		errorMsg := fmt.Sprintf("port forward rule for port %s not found", port)
		logger.Info("Port forward rule not found for update",
			"port", port,
			"config_name", config.Name,
			"error_type", "rule_not_found")
		return errors.New(errorMsg)
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
		DestinationIP: pf.DestinationIP, // Preserve existing source filter
		Enabled:       config.Enabled,
		Fwd:           config.DstIP,
		FwdPort:       config.FwdPort.String(),
		DstPort:       config.DstPort.String(),
		Name:          config.Name,
		PfwdInterface: config.Interface,
		Proto:         config.Protocol,
		Src:           pf.Src, // Preserve existing source filter
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
			"protocol", config.Protocol,
			"rule_id", pf.ID,
			"update_payload", portforward,
		)
		return fmt.Errorf("failed to update port forward rule for port %s (protocol %s): %w", port, config.Protocol, err)
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
	pf, portExists, err := router.CheckPort(ctx, config.DstPort, config.Protocol)
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
