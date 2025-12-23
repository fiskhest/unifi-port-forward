package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// FilterAnnotation is the annotation key for enabling port forwarding
const (
	FilterAnnotation    = "kube-port-forward-controller/ports"
	FinalizerAnnotation = "kube-port-forward-controller/finalizer"
)

// Config holds the application configuration
type Config struct {
	// UniFi Connection Settings
	RouterIP string `env:"UNIFI_ROUTER_IP" default:"192.168.27.1" json:"routerIp"`
	Username string `env:"UNIFI_USERNAME" default:"admin" json:"username"`
	Password string `env:"UNIFI_PASSWORD" required:"true" json:"password"`
	Site     string `env:"UNIFI_SITE" default:"default" json:"site"`
	APIKey   string `env:"UNIFI_API_KEY" json:"apiKey"`

	// Application Settings
	Debug    bool   `env:"DEBUG" default:"false" json:"debug"`
	LogLevel string `env:"LOG_LEVEL" default:"info" json:"logLevel"`

	// Runtime values (derived from settings)
	Host string `json:"-"`
}

// Validate performs basic validation of the configuration
func (c *Config) Validate() error {
	var errors []string

	// Validate router IP
	if c.RouterIP == "" {
		errors = append(errors, "router IP cannot be empty")
	} else if err := validateIP(c.RouterIP); err != nil {
		errors = append(errors, fmt.Sprintf("invalid router IP format: %v", err))
	}

	// Validate authentication
	if c.Password == "" && c.APIKey == "" {
		errors = append(errors, "either password or API key must be provided")
	}

	// Validate site
	if c.Site == "" {
		errors = append(errors, "site cannot be empty")
	}

	if len(errors) > 0 {
		return fmt.Errorf("configuration validation failed: %s", strings.Join(errors, "; "))
	}

	return nil
}

// SetDerivedValues calculates derived values from the configuration
func (c *Config) SetDerivedValues() {
	// Parse router URL from IP
	baseURL := url.URL{
		Host:   c.RouterIP,
		Scheme: "https",
	}
	c.Host = baseURL.String()
}

// ToURL returns the properly formatted UniFi controller URL
func (c *Config) ToURL() (*url.URL, error) {
	if c.Host == "" {
		return nil, fmt.Errorf("router IP not configured")
	}

	return url.Parse(c.Host)
}

// validateIP performs IP address validation using Go's net package
func validateIP(ip string) error {
	if ip == "" {
		return fmt.Errorf("empty string")
	}

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return fmt.Errorf("invalid IP address format")
	}

	return nil
}

// InitFromEnv initializes config from environment variables
func InitFromEnv(cfg *Config) {
	if cfg.RouterIP == "" {
		cfg.RouterIP = os.Getenv("UNIFI_ROUTER_IP")
	}
	if cfg.Username == "" {
		cfg.Username = os.Getenv("UNIFI_USERNAME")
	}
	if cfg.Password == "" {
		cfg.Password = os.Getenv("UNIFI_PASSWORD")
	}
	if cfg.Site == "" {
		cfg.Site = os.Getenv("UNIFI_SITE")
	}
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("UNIFI_API_KEY")
	}
	if !cfg.Debug {
		cfg.Debug = os.Getenv("DEBUG") != ""
	}
	if cfg.LogLevel == "" {
		if level := os.Getenv("LOG_LEVEL"); level != "" {
			cfg.LogLevel = level
		}
	}
}

// SetDefaults sets the default values for configuration
func (c *Config) SetDefaults() {
	if c.RouterIP == "" {
		c.RouterIP = "192.168.1.1"
	}
	if c.Username == "" {
		c.Username = "admin"
	}
	if c.Site == "" {
		c.Site = "default"
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
}

// Load loads configuration from environment variables and applies defaults
func (c *Config) Load() {
	// Load from environment variables first (for CLI flag defaults)
	InitFromEnv(c)

	// Apply defaults if still empty
	c.SetDefaults()

	// Set derived values
	c.SetDerivedValues()
}
