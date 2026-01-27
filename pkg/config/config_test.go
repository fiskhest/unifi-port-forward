package config

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			config: &Config{
				RouterIP:     "192.168.1.1",
				Password:     "password123",
				Site:         "default",
				SyncInterval: 15 * time.Minute,
			},
			expectError: false,
		},
		{
			name: "empty router IP",
			config: &Config{
				RouterIP:     "",
				Password:     "password123",
				Site:         "default",
				SyncInterval: 15 * time.Minute,
			},
			expectError: true,
			errorMsg:    "router IP cannot be empty",
		},
		{
			name: "invalid IP format",
			config: &Config{
				RouterIP:     "invalid-ip",
				Password:     "password123",
				Site:         "default",
				SyncInterval: 15 * time.Minute,
			},
			expectError: true,
			errorMsg:    "invalid router IP format",
		},
		{
			name: "missing both password and API key",
			config: &Config{
				RouterIP:     "192.168.1.1",
				Password:     "",
				APIKey:       "",
				Site:         "default",
				SyncInterval: 15 * time.Minute,
			},
			expectError: true,
			errorMsg:    "either password or API key must be provided",
		},
		{
			name: "empty site",
			config: &Config{
				RouterIP:     "192.168.1.1",
				Password:     "password123",
				Site:         "",
				SyncInterval: 15 * time.Minute,
			},
			expectError: true,
			errorMsg:    "site cannot be empty",
		},
		{
			name: "sync interval too short",
			config: &Config{
				RouterIP:     "192.168.1.1",
				Password:     "password123",
				Site:         "default",
				SyncInterval: 2 * time.Minute,
			},
			expectError: true,
			errorMsg:    "sync interval cannot happen more often than every five minutes",
		},
		{
			name: "valid with API key instead of password",
			config: &Config{
				RouterIP:     "192.168.1.1",
				APIKey:       "api-key-123",
				Site:         "default",
				SyncInterval: 15 * time.Minute,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			} else if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			} else if tt.expectError && err != nil && tt.errorMsg != "" {
				if !contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errorMsg, err)
				}
			}
		})
	}
}

func TestConfig_SetDerivedValues(t *testing.T) {
	config := &Config{
		RouterIP: "192.168.1.1",
	}

	config.SetDerivedValues()

	expectedHost := "https://192.168.1.1"
	if config.Host != expectedHost {
		t.Errorf("Expected Host to be '%s', got '%s'", expectedHost, config.Host)
	}
}

func TestConfig_ToURL(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		expectedURL string
	}{
		{
			name: "valid config with derived host",
			config: &Config{
				RouterIP: "192.168.1.1",
			},
			expectError: false,
			expectedURL: "https://192.168.1.1",
		},
		{
			name: "config without host",
			config: &Config{
				Host: "",
			},
			expectError: true,
		},
		{
			name: "config with existing host",
			config: &Config{
				Host: "https://unifi.example.com",
			},
			expectError: false,
			expectedURL: "https://unifi.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.config.Host == "" && tt.config.RouterIP != "" {
				tt.config.SetDerivedValues()
			}

			url, err := tt.config.ToURL()

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			} else if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			} else if !tt.expectError && err == nil && url.String() != tt.expectedURL {
				t.Errorf("Expected URL '%s', got '%s'", tt.expectedURL, url.String())
			}
		})
	}
}

func TestConfig_SetDefaults(t *testing.T) {
	config := &Config{}
	config.SetDefaults()

	if config.RouterIP != "192.168.1.1" {
		t.Errorf("Expected default RouterIP '192.168.1.1', got '%s'", config.RouterIP)
	}
	if config.Username != "admin" {
		t.Errorf("Expected default Username 'admin', got '%s'", config.Username)
	}
	if config.Site != "default" {
		t.Errorf("Expected default Site 'default', got '%s'", config.Site)
	}
	if config.SyncInterval != 15*time.Minute {
		t.Errorf("Expected default SyncInterval '15m', got '%v'", config.SyncInterval)
	}
}

func TestConfig_InitFromEnv(t *testing.T) {
	// Store original env vars
	origRouterIP := os.Getenv("UNIFI_ROUTER_IP")
	origUsername := os.Getenv("UNIFI_USERNAME")
	origPassword := os.Getenv("UNIFI_PASSWORD")
	origSite := os.Getenv("UNIFI_SITE")
	origAPIKey := os.Getenv("UNIFI_API_KEY")
	origSyncInterval := os.Getenv("UNIFI_SYNC_INTERVAL")
	origDebug := os.Getenv("DEBUG")

	// Helper function to set environment variables with error checking
	setEnv := func(key, value string) {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("Failed to set env var %s=%s: %v", key, value, err)
		}
	}

	// Clean up after test
	defer func() {
		setEnv("UNIFI_ROUTER_IP", origRouterIP)
		setEnv("UNIFI_USERNAME", origUsername)
		setEnv("UNIFI_PASSWORD", origPassword)
		setEnv("UNIFI_SITE", origSite)
		setEnv("UNIFI_API_KEY", origAPIKey)
		setEnv("UNIFI_SYNC_INTERVAL", origSyncInterval)
		setEnv("DEBUG", origDebug)
	}()

	// Set test env vars
	setEnv("UNIFI_ROUTER_IP", "192.168.100.1")
	setEnv("UNIFI_USERNAME", "testuser")
	setEnv("UNIFI_PASSWORD", "testpass")
	setEnv("UNIFI_SITE", "testsite")
	setEnv("UNIFI_API_KEY", "test-api-key")
	setEnv("UNIFI_SYNC_INTERVAL", "20m")
	setEnv("DEBUG", "true")

	config := &Config{}
	InitFromEnv(config)

	if config.RouterIP != "192.168.100.1" {
		t.Errorf("Expected RouterIP from env '192.168.100.1', got '%s'", config.RouterIP)
	}
	if config.Username != "testuser" {
		t.Errorf("Expected Username from env 'testuser', got '%s'", config.Username)
	}
	if config.Password != "testpass" {
		t.Errorf("Expected Password from env 'testpass', got '%s'", config.Password)
	}
	if config.Site != "testsite" {
		t.Errorf("Expected Site from env 'testsite', got '%s'", config.Site)
	}
	if config.APIKey != "test-api-key" {
		t.Errorf("Expected APIKey from env 'test-api-key', got '%s'", config.APIKey)
	}
	if config.SyncInterval != 20*time.Minute {
		t.Errorf("Expected SyncInterval from env '20m', got '%v'", config.SyncInterval)
	}
	if !config.Debug {
		t.Errorf("Expected Debug from env 'true', got '%v'", config.Debug)
	}
}

func TestConfig_Load(t *testing.T) {
	config := &Config{}
	config.Load()

	// Should have defaults if no env vars set
	if config.RouterIP == "" {
		t.Errorf("Expected RouterIP to be set after Load")
	}
	if config.Host == "" {
		t.Errorf("Expected Host to be derived after Load")
	}
}

func TestValidateIP(t *testing.T) {
	tests := []struct {
		input    string
		expected error
	}{
		{"192.168.1.1", nil},
		{"10.0.0.1", nil},
		{"127.0.0.1", nil},
		{"", func() error { return fmt.Errorf("empty string") }()},
		{"invalid-ip", func() error { return fmt.Errorf("invalid IP address format") }()},
		{"256.256.256.256", func() error { return fmt.Errorf("invalid IP address format") }()},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := validateIP(tt.input)

			if tt.expected == nil && err != nil {
				t.Errorf("Expected no error for '%s', got: %v", tt.input, err)
			} else if tt.expected != nil && err == nil {
				t.Errorf("Expected error for '%s', got none", tt.input)
			} else if tt.expected != nil && err != nil {
				if !contains(err.Error(), tt.expected.Error()) {
					t.Errorf("Expected error containing '%s', got: %v", tt.expected.Error(), err)
				}
			}
		})
	}
}

func TestConstants(t *testing.T) {
	expectedConstants := map[string]string{
		"FilterAnnotation":          "unifi-port-forward.fiskhe.st/mapping",
		"FinalizerLabel":            "unifi-port-forward.fiskhe.st/router-rule-protection",
		"CleanupStatusAnnotation":   "unifi-port-forward.fiskhe.st/cleanup-status",
		"CleanupAttemptsAnnotation": "unifi-port-forward.fiskhe.st/cleanup-attempts",
		"PortForwardRulesCRDName":   "portforwardrules.unifi-port-forward.fiskhe.st",
	}

	if FilterAnnotation != expectedConstants["FilterAnnotation"] {
		t.Errorf("Expected FilterAnnotation '%s', got '%s'", expectedConstants["FilterAnnotation"], FilterAnnotation)
	}
	if FinalizerLabel != expectedConstants["FinalizerLabel"] {
		t.Errorf("Expected FinalizerLabel '%s', got '%s'", expectedConstants["FinalizerLabel"], FinalizerLabel)
	}
	if CleanupStatusAnnotation != expectedConstants["CleanupStatusAnnotation"] {
		t.Errorf("Expected CleanupStatusAnnotation '%s', got '%s'", expectedConstants["CleanupStatusAnnotation"], CleanupStatusAnnotation)
	}
	if CleanupAttemptsAnnotation != expectedConstants["CleanupAttemptsAnnotation"] {
		t.Errorf("Expected CleanupAttemptsAnnotation '%s', got '%s'", expectedConstants["CleanupAttemptsAnnotation"], CleanupAttemptsAnnotation)
	}
	if PortForwardRulesCRDName != expectedConstants["PortForwardRulesCRDName"] {
		t.Errorf("Expected PortForwardRulesCRDName '%s', got '%s'", expectedConstants["PortForwardRulesCRDName"], PortForwardRulesCRDName)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr ||
		len(s) > len(substr) && s[len(s)-len(substr):] == substr ||
		len(s) > len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
