package config

import (
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Annotations and Labels that we are owners of
const (
	FilterAnnotation          = "unifi-port-forward.fiskhe.st/mapping"
	FinalizerLabel            = "unifi-port-forward.fiskhe.st/router-rule-protection"
	CleanupStatusAnnotation   = "unifi-port-forward.fiskhe.st/cleanup-status"
	CleanupAttemptsAnnotation = "unifi-port-forward.fiskhe.st/cleanup-attempts"
	PortForwardRulesCRDName   = "portforwardrules.unifi-port-forward.fiskhe.st"
)

// Config holds the controller's runtime configuration.
//
// The env tags document which variable feeds each field; loading itself is done
// by hand in InitFromEnv. Defaults live in SetDefaults and nowhere else - an
// earlier `default:` tag here claimed a different router IP than SetDefaults
// actually applied, which was invisible because nothing ever parsed the tags.
type Config struct {
	// UniFi Connection Settings
	RouterIP string `env:"UNIFI_ROUTER_IP" json:"routerIp"`
	Username string `env:"UNIFI_USERNAME" json:"username"`
	// Password and APIKey are json:"-" so that dumping a Config for diagnostics
	// cannot leak a credential.
	Password string `env:"UNIFI_PASSWORD" json:"-"`
	Site     string `env:"UNIFI_SITE" json:"site"`
	APIKey   string `env:"UNIFI_API_KEY" json:"-"`

	// Application Settings
	Debug        bool          `env:"DEBUG" json:"debug"`
	SyncInterval time.Duration `env:"UNIFI_SYNC_INTERVAL" json:"syncInterval"`

	// Finalizer settings
	FinalizerMaxRetries    int           `env:"FINALIZER_MAX_RETRIES" json:"finalizerMaxRetries"`
	FinalizerRetryInterval time.Duration `env:"FINALIZER_RETRY_INTERVAL" json:"finalizerRetryInterval"`

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

	// Validate sync interval
	if c.SyncInterval < 5*time.Minute {
		errors = append(errors, "sync interval cannot happen more often than every five minutes")
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
	if envRouterIP := os.Getenv("UNIFI_ROUTER_IP"); envRouterIP != "" {
		cfg.RouterIP = envRouterIP
	}
	if envUsername := os.Getenv("UNIFI_USERNAME"); envUsername != "" {
		cfg.Username = envUsername
	}
	if envPassword := os.Getenv("UNIFI_PASSWORD"); envPassword != "" {
		cfg.Password = envPassword
	}
	if envSite := os.Getenv("UNIFI_SITE"); envSite != "" {
		cfg.Site = envSite
	}
	if envAPIKey := os.Getenv("UNIFI_API_KEY"); envAPIKey != "" {
		cfg.APIKey = envAPIKey
	}
	if envSyncInterval := os.Getenv("UNIFI_SYNC_INTERVAL"); envSyncInterval != "" {
		syncInterval, err := time.ParseDuration(envSyncInterval)
		if err != nil {
			log.Fatal(err)
		}
		cfg.SyncInterval = syncInterval
	}
	if envDebug := os.Getenv("DEBUG"); envDebug != "" {
		// Parse the value rather than testing it for emptiness: the latter made
		// DEBUG=False enable debug logging, which is exactly what the shipped
		// manifest set.
		//
		// An unparseable value warns rather than exiting. Verbosity is not worth
		// refusing to start over, unlike the sync interval below it.
		if debug, err := strconv.ParseBool(envDebug); err == nil {
			cfg.Debug = debug
		} else {
			log.Printf("ignoring DEBUG=%q: want a boolean such as true or false", envDebug)
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
	if c.SyncInterval == 0 {
		c.SyncInterval = 15 * time.Minute
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
