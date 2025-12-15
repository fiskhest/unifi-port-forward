package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/filipowm/go-unifi/unifi"
)

func main() {
	var (
		routerIP = os.Getenv("UNIFI_ROUTER_IP")
		username = os.Getenv("UNIFI_USERNAME")
		password = os.Getenv("UNIFI_PASSWORD")
		site     = os.Getenv("UNIFI_SITE")
	)


    if routerIP == "" {
        routerIP = "192.168.27.1"
    }

    host := fmt.Sprintf("https://%s", routerIP)
    if username == "" {
        username = "kube-port-forward-controller"
    }

    if site == "" {
        site = "default"
    }

	// 1. Initialize UniFi client
	cfg := &unifi.ClientConfig{
		URL:       host,
		User:      username,
		Password:  password,
		VerifySSL: false,
	}

	client, err := unifi.NewClient(cfg)
	if err != nil {
		log.Fatalf("failed to create UniFi client: %v", err)
	}

	ctx := context.Background()
	fmt.Println("Logged in, UniFi version:", client.Version())

	// 2. Rules to delete
	portMaps := map[string]string{
		"81": "192.168.27.130",
	}

	portforwards, err := client.ListPortForward(ctx, site)
	if err != nil {
		log.Fatalf("failed to list port forward rules: %v", err)
	}

	// 3. Delete each rule
	for DstPort, DstIP := range portMaps {
		for _, portforward := range portforwards {
			fmt.Println(portforward)
			if portforward.FwdPort == DstPort && portforward.Fwd == DstIP {
				fmt.Println("port matched")
				err := client.DeletePortForward(ctx, site, portforward.ID)
				if err != nil {
					log.Printf("failed to delete rule ID %s: %v", portforward.ID, err)
				} else {
					fmt.Printf("deleted port-forward rule ID %s successfully\n", portforward.ID)
				}
			}
		}
	}
}
