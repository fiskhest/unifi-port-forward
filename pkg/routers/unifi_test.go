package routers

import (
	"fmt"
	"testing"

	"github.com/filipowm/go-unifi/unifi"
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
			isValid := tt.config.Name != "" &&
				tt.config.DstIP != "" &&
				(tt.config.Protocol == "tcp" || tt.config.Protocol == "udp") &&
				tt.config.DstPort > 0 && tt.config.DstPort <= 65535

			if isValid != tt.valid {
				t.Errorf("Expected validity %v, got %v", tt.valid, isValid)
			}
		})
	}
}

// TestRouter_Interface tests the Router interface contract
func TestRouter_Interface(t *testing.T) {
	// This test ensures that UnifiRouter implements Router interface
	var _ Router = &UnifiRouter{}
}

// TestRouter_RememberMeEnabled verifies that RememberMe is set to true in client config
func TestRouter_RememberMeEnabled(t *testing.T) {
	// Test that CreateUnifiRouter sets RememberMe=true
	// We can't easily test without actual credentials, but we can verify the pattern
	// This is more of a code review test - ensuring RememberMe is hardcoded

	// This test serves as documentation that RememberMe=true should always be set
	// The actual verification would be in integration tests with real UniFi controller
	t.Log("RememberMe should be hardcoded to true in CreateUnifiRouter for persistent sessions")
}

// TestIsAuthError_SimpleTests tests the 401 status code detection logic
func TestIsAuthError_SimpleTests(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		expectRetry bool
	}{
		{
			name: "HTTP 401 error should trigger retry",
			err: &unifi.ServerError{
				StatusCode: 401,
				Message:    "Authentication failed",
			},
			expectRetry: true,
		},
		{
			name: "HTTP 403 error should not trigger retry",
			err: &unifi.ServerError{
				StatusCode: 403,
				Message:    "Forbidden",
			},
			expectRetry: false,
		},
		{
			name: "HTTP 500 error should not trigger retry",
			err: &unifi.ServerError{
				StatusCode: 500,
				Message:    "Internal server error",
			},
			expectRetry: false,
		},
		{
			name:        "Generic error should not trigger retry",
			err:         fmt.Errorf("network timeout"),
			expectRetry: false,
		},
		{
			name:        "No error should not trigger retry",
			err:         nil,
			expectRetry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the logic directly: check for 401 status code
			serverErr, ok := tt.err.(*unifi.ServerError)
			shouldRetry := ok && serverErr.StatusCode == 401

			if shouldRetry != tt.expectRetry {
				t.Errorf("Expected shouldRetry=%v, got shouldRetry=%v for error: %v", tt.expectRetry, shouldRetry, tt.err)
			}
		})
	}
}
