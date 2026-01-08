package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
	"unifi-port-forwarder/cmd/cleaner"
	"unifi-port-forwarder/cmd/service-debugger"
	"unifi-port-forwarder/pkg/config"
	"unifi-port-forwarder/pkg/controller"
	"unifi-port-forwarder/pkg/routers"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var (
	cfg = config.Config{}
)

var rootCmd = &cobra.Command{
	Use:           "unifi-port-forwarder [command]",
	Short:         "Kubernetes controller for automatic router port forwarding",
	Long:          `Automatically configures router port forwarding for Kubernetes LoadBalancer services`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Default to controller when no command is specified
		return runController(cmd, args)
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Load configuration (env vars first, then defaults)
		cfg.Load()

		// Override with CLI flags if they were explicitly set
		if cmd.Flags().Changed("router-ip") {
			cfg.RouterIP, _ = cmd.Flags().GetString("router-ip")
		}
		if cmd.Flags().Changed("username") {
			cfg.Username, _ = cmd.Flags().GetString("username")
		}
		if cmd.Flags().Changed("password") {
			cfg.Password, _ = cmd.Flags().GetString("password")
		}
		if cmd.Flags().Changed("site") {
			cfg.Site, _ = cmd.Flags().GetString("site")
		}
		if cmd.Flags().Changed("api-key") {
			cfg.APIKey, _ = cmd.Flags().GetString("api-key")
		}
		if cmd.Flags().Changed("debug") {
			cfg.Debug, _ = cmd.Flags().GetBool("debug")
		}
		if cmd.Flags().Changed("log-level") {
			cfg.LogLevel, _ = cmd.Flags().GetString("log-level")
		}

		// Validate final configuration
		return cfg.Validate()
	},
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringVarP(&cfg.RouterIP, "router-ip", "r", "192.168.1.1", "UniFi router IP address (env: UNIFI_ROUTER_IP, default: 192.168.1.1)")
	rootCmd.PersistentFlags().StringVarP(&cfg.Username, "username", "u", "admin", "UniFi username (env: UNIFI_USERNAME, default: admin)")
	rootCmd.PersistentFlags().StringVarP(&cfg.Password, "password", "p", "", "UniFi password (env: UNIFI_PASSWORD, required)")
	rootCmd.PersistentFlags().StringVarP(&cfg.Site, "site", "s", "default", "UniFi site name (env: UNIFI_SITE, default: default)")
	rootCmd.PersistentFlags().StringVarP(&cfg.APIKey, "api-key", "k", "", "UniFi API key (env: UNIFI_API_KEY, alternative to username/password)")
	rootCmd.PersistentFlags().BoolVarP(&cfg.Debug, "debug", "d", false, "Enable debug logging (env: DEBUG)")
	rootCmd.PersistentFlags().StringVar(&cfg.LogLevel, "log-level", "info", "Log level: trace, debug, info, warn, error (env: LOG_LEVEL, default: info)")

	// Add subcommands
	rootCmd.AddCommand(controllerCmd)
	rootCmd.AddCommand(serviceDebuggerCmd)
	rootCmd.AddCommand(cleanCmd)

	// Set default command to controller when no command is specified
	rootCmd.SetHelpCommand(&cobra.Command{Hidden: true})
}

func main() {
	if err := Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// controllerCmd runs the port forwarding controller
var controllerCmd = &cobra.Command{
	Use:   "controller",
	Short: "Run the port forwarding controller",
	Long:  `Start the Kubernetes controller that monitors LoadBalancer services and configures router port forwarding rules`,
	RunE:  runController,
}

func init() {
	// Add service-debugger specific flags
	serviceDebuggerCmd.Flags().StringP("namespace", "n", "", "Filter by namespace (empty = all)")
	serviceDebuggerCmd.Flags().StringP("labels", "l", "", "Filter by labels (e.g., app=web)")
	serviceDebuggerCmd.Flags().StringP("log-level", "", "info", "Log level: debug, info, warn, error")
	serviceDebuggerCmd.Flags().StringP("output", "o", "text", "Output format: text, json")
	serviceDebuggerCmd.Flags().IntP("history", "H", 10, "Number of changes to track per service")
	serviceDebuggerCmd.Flags().DurationP("interval", "i", 5*time.Second, "Polling interval for status checks")

	// Add clean-specific flags
	cleanCmd.Flags().StringP("port-mappings", "m", "", "Port mappings to clean (format: 'external-port:dest-ip', comma-separated) [REQUIRED]")
	cleanCmd.Flags().StringP("port-mappings-file", "f", "", "Path to port mappings configuration file (YAML/JSON)")
}

// serviceDebuggerCmd runs the service IP debugger
var serviceDebuggerCmd = &cobra.Command{
	Use:   "service-debugger",
	Short: "Run the service IP debugger",
	Long:  `Monitor Kubernetes services for IP changes and debug LoadBalancer IP issues in LoadBalancer services`,
	RunE:  runServiceDebugger,
}

// cleanCmd runs the port forwarding rule cleaner
var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Run the port forwarding rule cleaner",
	Long:  `Clean up stale port forwarding rules from the router`,
	RunE:  runClean,
}

func runController(cmd *cobra.Command, args []string) error {
	// Setup logging
	logger := logr.FromSlogHandler(slog.Default().Handler())
	ctrllog.SetLogger(logger)

	// Create router
	router, err := routers.CreateUnifiRouter(cfg.Host, cfg.Username, cfg.Password, cfg.Site, cfg.APIKey)
	if err != nil {
		return fmt.Errorf("failed to create router: %w", err)
	}

	// Create manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: runtime.NewScheme(),
		Metrics: server.Options{
			BindAddress: "0", // Disable metrics to avoid port conflicts
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}

	// Setup scheme
	if err := corev1.AddToScheme(mgr.GetScheme()); err != nil {
		return fmt.Errorf("failed to add corev1 to scheme: %w", err)
	}

	// Create reconciler
	reconciler := &controller.PortForwardReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Router: router,
		Config: &cfg,
	}

	// Setup controller
	if err := reconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("failed to setup controller: %w", err)
	}

	// Create and start periodic reconciler (always on)
	reconciler.PeriodicReconciler = controller.NewPeriodicReconciler(
		mgr.GetClient(),
		mgr.GetScheme(),
		router,
		&cfg,
		reconciler.EventPublisher,
		reconciler.Recorder,
	)

	// Start periodic reconciler in background
	go func() {
		logger := logr.FromSlogHandler(slog.Default().Handler()).WithValues("component", "periodic-reconciler-main")
		if err := reconciler.PeriodicReconciler.Start(context.Background()); err != nil {
			logger.Error(err, "Periodic reconciler failed")
		}
	}()

	// Ensure graceful shutdown
	setupGracefulShutdown()

	// Override shutdown function to also stop periodic reconciler
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		fmt.Println("Shutting down gracefully")

		// Stop periodic reconciler
		if reconciler.PeriodicReconciler != nil {
			reconciler.PeriodicReconciler.Stop()
		}

		os.Exit(0)
	}()

	// Ensure graceful shutdown
	setupGracefulShutdown()

	// Override shutdown function to also stop periodic reconciler
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		fmt.Println("Shutting down gracefully")

		// Stop periodic reconciler
		if reconciler.PeriodicReconciler != nil {
			reconciler.PeriodicReconciler.Stop()
		}

		os.Exit(0)
	}()

	fmt.Println("Starting port forwarding controller")
	return mgr.Start(ctrl.SetupSignalHandler())
}

func runClean(cmd *cobra.Command, args []string) error {
	// Get CLI flags
	portMappingsStr, _ := cmd.Flags().GetString("port-mappings")
	portMappingsFile, _ := cmd.Flags().GetString("port-mappings-file")

	var portMaps map[string]string
	var err error

	if portMappingsFile != "" {
		// Load from file
		portMaps, err = loadPortMappingsFromFile(portMappingsFile)
	} else if portMappingsStr != "" {
		// Parse CLI string
		portMaps, err = parsePortMappingsString(portMappingsStr)
	} else {
		return fmt.Errorf("--port-mappings or --port-mappings-file is required")
	}

	if err != nil {
		return fmt.Errorf("failed to parse port mappings: %w", err)
	}

	// Create cleaner config from global config
	cleanConfig := cleaner.Config{
		Host:     cfg.Host,
		Username: cfg.Username,
		Password: cfg.Password,
		Site:     cfg.Site,
		APIKey:   cfg.APIKey,
	}

	return cleaner.Run(cleanConfig, portMaps)
}

func runServiceDebugger(cmd *cobra.Command, args []string) error {
	// Get service-debugger specific flags
	namespace, _ := cmd.Flags().GetString("namespace")
	labels, _ := cmd.Flags().GetString("labels")
	logLevel, _ := cmd.Flags().GetString("log-level")
	output, _ := cmd.Flags().GetString("output")
	history, _ := cmd.Flags().GetInt("history")
	interval, _ := cmd.Flags().GetDuration("interval")

	// Set defaults
	if output == "" {
		output = "text"
	}
	if history == 0 {
		history = 10
	}
	if interval == 0 {
		interval = 5 * time.Second
	}

	config := servicedebugger.ServiceDebuggerConfig{
		Namespace:     namespace,
		LabelSelector: labels,
		LogLevel:      logLevel,
		OutputFormat:  output,
		HistorySize:   history,
		PollInterval:  interval,
	}

	return servicedebugger.Run(config)
}

// parsePortMappingsString parses CLI string format: "83:192.168.27.130,8080:192.168.27.131"
func parsePortMappingsString(mappingsStr string) (map[string]string, error) {
	if mappingsStr == "" {
		return nil, fmt.Errorf("port mappings cannot be empty")
	}

	portMaps := make(map[string]string)
	pairs := strings.Split(mappingsStr, ",")

	for _, pair := range pairs {
		parts := strings.Split(strings.TrimSpace(pair), ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid mapping format: %s (expected format: 'port:ip')", pair)
		}

		port := strings.TrimSpace(parts[0])
		ip := strings.TrimSpace(parts[1])

		// Validate port is numeric
		if _, err := strconv.Atoi(port); err != nil {
			return nil, fmt.Errorf("invalid port number: %s", port)
		}

		// Validate IP format
		if net.ParseIP(ip) == nil {
			return nil, fmt.Errorf("invalid IP address: %s", ip)
		}

		portMaps[port] = ip
	}

	return portMaps, nil
}

// loadPortMappingsFromFile loads port mappings from YAML or JSON file
func loadPortMappingsFromFile(filename string) (map[string]string, error) {
	// Validate filename
	if filename == "" {
		return nil, fmt.Errorf("filename cannot be empty")
	}

	// Security validation - check for path traversal attempts
	cleanPath := filepath.Clean(filename)
	if strings.Contains(cleanPath, "..") {
		return nil, fmt.Errorf("path traversal not allowed: %s", filename)
	}

	// Get current working directory to restrict file access
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %w", err)
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("invalid filename path: %w", err)
	}

	// Ensure the file is within current directory or allowed subdirectories
	if !strings.HasPrefix(absPath, cwd) {
		return nil, fmt.Errorf("access denied - file outside current directory: %s", filename)
	}

	// Additional check: ensure file exists and is readable
	fileInfo, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("file not accessible: %w", err)
	}

	// Ensure it's a regular file, not a directory or special file
	if !fileInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file: %s", filename)
	}

	data, err := os.ReadFile(absPath) // #nosec G304 - validated above for security
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var config struct {
		Mappings []struct {
			ExternalPort  string `yaml:"external-port" json:"external-port"`
			DestinationIP string `yaml:"destination-ip" json:"destination-ip"`
		} `yaml:"mappings" json:"mappings"`
	}

	if strings.HasSuffix(strings.ToLower(filename), ".json") {
		err = json.Unmarshal(data, &config)
	} else {
		// Default to YAML
		err = yaml.Unmarshal(data, &config)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}

	portMaps := make(map[string]string)
	for _, mapping := range config.Mappings {
		// Validate port
		if _, err := strconv.Atoi(mapping.ExternalPort); err != nil {
			return nil, fmt.Errorf("invalid port number: %s", mapping.ExternalPort)
		}

		// Validate IP
		if net.ParseIP(mapping.DestinationIP) == nil {
			return nil, fmt.Errorf("invalid IP address: %s", mapping.DestinationIP)
		}

		portMaps[mapping.ExternalPort] = mapping.DestinationIP
	}

	return portMaps, nil
}

func setupGracefulShutdown() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("Shutting down gracefully")
		os.Exit(0)
	}()
}

func Execute() error {
	return rootCmd.Execute()
}
