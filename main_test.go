package main

import (
	"testing"

	"unifi-port-forward/pkg/config"
)

// TestRouterIPFlagReachesDialledAddress pins the wiring between --router-ip and
// the address the controller actually connects to.
//
// PersistentPreRunE calls cfg.Load(), which derives cfg.Host from the
// environment, and only then applies flag overrides to cfg.RouterIP. Without a
// second SetDerivedValues the flag updates RouterIP while Host - the value
// handed to CreateUnifiRouter - still points at the previous address.
func TestRouterIPFlagReachesDialledAddress(t *testing.T) {
	t.Setenv("UNIFI_ROUTER_IP", "192.168.1.1")
	t.Setenv("UNIFI_API_KEY", "an-api-key")

	cfg = config.Config{}
	t.Cleanup(func() { cfg = config.Config{} })

	// ParseFlags rather than PersistentFlags().Set: cobra only merges persistent
	// flags into cmd.Flags() while parsing, and PersistentPreRunE reads
	// cmd.Flags().Changed, so setting the flag directly would go unnoticed.
	if err := rootCmd.ParseFlags([]string{"--router-ip", "10.9.9.9"}); err != nil {
		t.Fatalf("failed to parse --router-ip: %v", err)
	}
	t.Cleanup(func() {
		if f := rootCmd.Flags().Lookup("router-ip"); f != nil {
			f.Changed = false
		}
	})

	if err := rootCmd.PersistentPreRunE(rootCmd, nil); err != nil {
		t.Fatalf("PersistentPreRunE returned error: %v", err)
	}

	if cfg.RouterIP != "10.9.9.9" {
		t.Errorf("RouterIP = %q, want 10.9.9.9", cfg.RouterIP)
	}
	if cfg.Host != "https://10.9.9.9" {
		t.Errorf("Host = %q, want https://10.9.9.9 - the flag has to reach the dialled address", cfg.Host)
	}
}

// TestRouterIPFromEnvReachesDialledAddress covers the same path with no flag set,
// which is how the Kubernetes deployment configures the controller.
func TestRouterIPFromEnvReachesDialledAddress(t *testing.T) {
	t.Setenv("UNIFI_ROUTER_IP", "172.16.0.1")
	t.Setenv("UNIFI_API_KEY", "an-api-key")

	cfg = config.Config{}
	t.Cleanup(func() { cfg = config.Config{} })

	if err := rootCmd.PersistentPreRunE(rootCmd, nil); err != nil {
		t.Fatalf("PersistentPreRunE returned error: %v", err)
	}

	if cfg.Host != "https://172.16.0.1" {
		t.Errorf("Host = %q, want https://172.16.0.1", cfg.Host)
	}
}

// TestFlagsBeatEnvironment locks in the intended precedence: flag, then
// environment, then default. While the flags were bound to cfg fields with
// StringVarP, cfg.Load() overwrote each parsed flag value with the environment
// one and the environment silently won every time.
func TestFlagsBeatEnvironment(t *testing.T) {
	t.Setenv("UNIFI_ROUTER_IP", "192.168.1.1")
	t.Setenv("UNIFI_SITE", "env-site")
	t.Setenv("UNIFI_API_KEY", "env-key")

	cfg = config.Config{}
	t.Cleanup(func() { cfg = config.Config{} })

	if err := rootCmd.ParseFlags([]string{"--site", "flag-site", "--api-key", "flag-key"}); err != nil {
		t.Fatalf("failed to parse flags: %v", err)
	}
	t.Cleanup(func() {
		for _, name := range []string{"site", "api-key"} {
			if f := rootCmd.Flags().Lookup(name); f != nil {
				f.Changed = false
			}
		}
	})

	if err := rootCmd.PersistentPreRunE(rootCmd, nil); err != nil {
		t.Fatalf("PersistentPreRunE returned error: %v", err)
	}

	if cfg.Site != "flag-site" {
		t.Errorf("Site = %q, want flag-site - the flag must beat UNIFI_SITE", cfg.Site)
	}
	if cfg.APIKey != "flag-key" {
		t.Errorf("APIKey = %q, want flag-key - the flag must beat UNIFI_API_KEY", cfg.APIKey)
	}
}

// TestAPIKeyAloneIsSufficient records that no username or password is needed
// alongside an API key: CreateUnifiRouter clears both, because the client
// validates them as excluded_with=APIKey and would otherwise refuse to build.
func TestAPIKeyAloneIsSufficient(t *testing.T) {
	t.Setenv("UNIFI_ROUTER_IP", "192.168.1.1")
	t.Setenv("UNIFI_API_KEY", "an-api-key")
	t.Setenv("UNIFI_PASSWORD", "")
	t.Setenv("UNIFI_USERNAME", "")

	cfg = config.Config{}
	t.Cleanup(func() { cfg = config.Config{} })

	if err := rootCmd.PersistentPreRunE(rootCmd, nil); err != nil {
		t.Fatalf("an API key with no password should be accepted, got: %v", err)
	}
}
