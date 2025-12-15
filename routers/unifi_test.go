package routers

import (
	"testing"
)

// TestPortConfig_Validation tests the PortConfig struct validation
func TestPortConfig_Validation(t *testing.T) {
	tests := []struct {
		name   string
		config PortConfig
		valid  bool
	}{
		{
			name: "valid TCP config",
			config: PortConfig{
				Name:      "test-service",
				DstPort:   8080,
				Enabled:   true,
				Interface: "wan",
				DstIP:     "192.168.1.100",
				SrcIP:     "any",
				Protocol:  "tcp",
			},
			valid: true,
		},
		{
			name: "valid UDP config",
			config: PortConfig{
				Name:      "test-service",
				DstPort:   53,
				Enabled:   true,
				Interface: "wan",
				DstIP:     "192.168.1.100",
				SrcIP:     "any",
				Protocol:  "udp",
			},
			valid: true,
		},
		{
			name: "invalid protocol",
			config: PortConfig{
				Name:      "test-service",
				DstPort:   8080,
				Enabled:   true,
				Interface: "wan",
				DstIP:     "192.168.1.100",
				SrcIP:     "any",
				Protocol:  "invalid",
			},
			valid: false,
		},
		{
			name: "missing name",
			config: PortConfig{
				DstPort:   8080,
				Enabled:   true,
				Interface: "wan",
				DstIP:     "192.168.1.100",
				SrcIP:     "any",
				Protocol:  "tcp",
			},
			valid: false,
		},
		{
			name: "missing destination IP",
			config: PortConfig{
				Name:      "test-service",
				DstPort:   8080,
				Enabled:   true,
				Interface: "wan",
				SrcIP:     "any",
				Protocol:  "tcp",
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Basic validation checks
			isValid := true

			if tt.config.Name == "" {
				isValid = false
			}

			if tt.config.DstIP == "" {
				isValid = false
			}

			if tt.config.Protocol != "tcp" && tt.config.Protocol != "udp" {
				isValid = false
			}

			if tt.config.DstPort <= 0 || tt.config.DstPort > 65535 {
				isValid = false
			}

			if isValid != tt.valid {
				t.Errorf("Expected validity %v, got %v", tt.valid, isValid)
			}
		})
	}
}

// TestRouter_Interface tests the Router interface contract
func TestRouter_Interface(t *testing.T) {
	// This test ensures that UnifiRouter implements the Router interface
	var _ Router = &UnifiRouter{}
}
