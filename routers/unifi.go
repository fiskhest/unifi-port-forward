package routers

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"github.com/filipowm/go-unifi/unifi"
)

type UnifiRouter struct {
	SiteID string
	Client unifi.Client
}

func CreateUnifiRouter(baseurl, username, password, site string) (*UnifiRouter, error) {
	// Using API Key (recommended, requires UniFi Controller 9.0.108+)
	// client, err := unifi.NewClient(&unifi.ClientConfig{
	// 	URL:    baseurl,
	// 	APIKey: password,
	// })

	client, err := unifi.NewClient(&unifi.ClientConfig{
		URL:       baseurl,
		User:      username,
		Password:  password,
		VerifySSL: false,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	err = client.Login()
	if err != nil {
		return nil, err
	}
	defer client.Logout()

	log.Printf("UniFi Controller Version: %s\n", client.Version())

	router := &UnifiRouter{
		SiteID: site,
		Client: client,
	}

	return router, nil
}

// returns pf, exists
// func getPortForwardRuleExists(client unifi.Client, site string, port int) (unifi.PortForward, bool) {
// 	portforwards, err := client.ListPortForward(context.TODO(), site)
// 	if err != nil {
// 		log.Fatalf("failed to list port forward rules: %v", err)
// 	}

// 	sPort := strconv.Itoa(port)
// 	for _, portforward := range portforwards {
// 		if portforward.FwdPort == sPort {
// 			fmt.Printf("Port Forwarding rule already exists: Port %s ID %s\n", sPort, portforward.ID)
// 			return portforward, true
// 		}
// 	}
// 	return unifi.PortForward{}, false
// }

func (router *UnifiRouter) CheckPort(ctx context.Context, port int) (*unifi.PortForward, bool, error) {
	portforwards, err := router.Client.ListPortForward(ctx, router.SiteID)
	if err != nil {
		return &unifi.PortForward{}, false, err
	}

	for _, portforward := range portforwards {
		portNum, err := strconv.Atoi(portforward.FwdPort)
		if err != nil {
			return &unifi.PortForward{}, false, err
		}
		if portNum == port {
			return &portforward, true, nil
		}
	}
	return &unifi.PortForward{}, false, nil
}

func (router *UnifiRouter) AddPort(ctx context.Context, config PortConfig) error {
	// TODO: test if config.DstIP is empty and error out (do not add rule)
	if config.DstIP == "" {
		return fmt.Errorf("forward IP was empty - I don't want to create such a rule")
	}

	portforward := &unifi.PortForward{
		SiteID:        router.SiteID,
		Enabled:       config.Enabled,
		Fwd:           config.DstIP,
		FwdPort:       strconv.Itoa(config.FwdPort),
		DstPort:       strconv.Itoa(config.DstPort),
		Name:          config.Name,
		PfwdInterface: config.Interface,
		Proto:         config.Protocol,
		Src:           "any",
	}

	_, err := router.Client.CreatePortForward(context.TODO(), router.SiteID, portforward)
	if err != nil {
		return err
	}
	return nil
}

func (router *UnifiRouter) UpdatePort(ctx context.Context, port int, config PortConfig) error {
	pf, portExists, err := router.CheckPort(ctx, port)
	if err != nil {
		return err
	}

	if portExists {
		portforward := &unifi.PortForward{
			ID:            pf.ID,
			SiteID:        router.SiteID,
			Enabled:       config.Enabled,
			Fwd:           config.DstIP,
			FwdPort:       strconv.Itoa(config.FwdPort),
			DstPort:       strconv.Itoa(config.DstPort),
			Name:          config.Name,
			PfwdInterface: config.Interface,
			Proto:         config.Protocol,
			Src:           "any",
		}

		_, err := router.Client.UpdatePortForward(ctx, router.SiteID, portforward)
		if err != nil {
			return err
		}

	}
	return nil
}

func (router *UnifiRouter) RemovePort(ctx context.Context, config PortConfig) error {
	pf, portExists, err := router.CheckPort(ctx, config.DstPort)
	if err != nil {
		return err
	}

	if portExists {
		if err := router.Client.DeletePortForward(ctx, router.SiteID, pf.ID); err != nil {
			return fmt.Errorf("deleting port-forward rule %s: %v", pf.ID, err)
		}
	}
	return nil
}
