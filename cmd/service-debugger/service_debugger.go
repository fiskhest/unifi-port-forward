package servicedebugger

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// ServiceState tracks the state of a service over time
type ServiceState struct {
	Name      string     `json:"name"`
	Namespace string     `json:"namespace"`
	IPs       []string   `json:"ips"`
	LastSeen  time.Time  `json:"last_seen"`
	Changes   []IPChange `json:"changes"`
}

// IPChange tracks IP changes for a service
type IPChange struct {
	Timestamp       time.Time `json:"timestamp"`
	OldIPs          []string  `json:"old_ips"`
	NewIPs          []string  `json:"new_ips"`
	ChangeType      string    `json:"change_type"` // "created", "updated", "deleted", "ip_changed"
	IPType          string    `json:"ip_type"`     // "loadbalancer", "multiple", "unknown"
	NumIngress      int       `json:"num_ingress"`
	AnnotationValue string    `json:"annotation_value"`
	Namespace       string    `json:"namespace"`
	Name            string    `json:"name"`
}

// ServiceDebugger monitors service changes and tracks IP transitions
type ServiceDebugger struct {
	client.Client
	Scheme *runtime.Scheme
	Config ServiceDebuggerConfig
}

// ServiceDebuggerConfig holds configuration for the debugger
type ServiceDebuggerConfig struct {
	Namespace     string
	LabelSelector string
	LogLevel      string
	OutputFormat  string
	HistorySize   int
	PollInterval  time.Duration
}

// serviceStates tracks all services being monitored
var serviceStates = make(map[string]*ServiceState)

// Run starts the service debugger
func Run(config ServiceDebuggerConfig) error {
	log.Printf("Starting Kubernetes Service IP Debugger (namespace=%s, labels=%s, output=%s)",
		config.Namespace, config.LabelSelector, config.OutputFormat)

	// Setup controller-runtime logging
	logger := logr.FromSlogHandler(slog.Default().Handler())
	ctrllog.SetLogger(logger)

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

	// Create debugger
	d := &ServiceDebugger{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Config: config,
	}

	// Setup controller
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Named("service-debugger").
		Complete(d); err != nil {
		return fmt.Errorf("failed to setup controller: %w", err)
	}

	// Setup graceful shutdown
	setupGracefulShutdown()

	// Start status checker goroutine
	go d.startStatusChecker()

	log.Println("Starting service debugger")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("failed to start manager: %w", err)
	}

	return nil
}

// Reconcile implements the reconciliation logic for Service resources
func (d *ServiceDebugger) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.Log.WithValues("namespace", req.Namespace, "name", req.Name)

	// Apply namespace filter
	if d.Config.Namespace != "" && req.Namespace != d.Config.Namespace {
		return ctrl.Result{}, nil
	}

	// Fetch the Service instance
	service := &corev1.Service{}
	if err := d.Get(ctx, req.NamespacedName, service); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to get service")
			return ctrl.Result{}, err
		}

		// Service deleted - handle deletion
		d.handleServiceDeletion(req.Namespace, req.Name)
		return ctrl.Result{}, nil
	}

	// Apply label filter if specified
	if d.Config.LabelSelector != "" {
		labelSelector, err := metav1.ParseToLabelSelector(d.Config.LabelSelector)
		if err != nil {
			logger.Error(err, "Invalid label selector", "selector", d.Config.LabelSelector)
			return ctrl.Result{}, err
		}

		selector, err := metav1.LabelSelectorAsSelector(labelSelector)
		if err != nil {
			logger.Error(err, "Invalid label selector conversion", "selector", d.Config.LabelSelector)
			return ctrl.Result{}, err
		}

		if !selector.Matches(labels.Set(service.Labels)) {
			return ctrl.Result{}, nil
		}
	}

	// Handle service update/creation
	d.handleServiceChange(service)

	return ctrl.Result{}, nil
}

// handleServiceChange processes service creation and updates
func (d *ServiceDebugger) handleServiceChange(service *corev1.Service) {
	serviceKey := fmt.Sprintf("%s/%s", service.Namespace, service.Name)
	currentTime := time.Now()

	// Extract current IPs
	currentIPs := d.extractIPs(service)
	ipType := d.classifyIPs(currentIPs)
	numIngress := len(service.Status.LoadBalancer.Ingress)
	annotationValue := d.hasPortForwardAnnotation(service)

	// Get or create service state
	state, exists := serviceStates[serviceKey]
	if !exists {
		// New service
		state = &ServiceState{
			Name:      service.Name,
			Namespace: service.Namespace,
			IPs:       currentIPs,
			LastSeen:  currentTime,
			Changes:   make([]IPChange, 0),
		}
		serviceStates[serviceKey] = state

		// Log creation
		change := IPChange{
			Timestamp:       currentTime,
			OldIPs:          []string{},
			NewIPs:          currentIPs,
			ChangeType:      "created",
			IPType:          ipType,
			NumIngress:      numIngress,
			AnnotationValue: annotationValue,
			Namespace:       service.Namespace,
			Name:            service.Name,
		}
		state.Changes = append(state.Changes, change)

		d.logChange(change)
		return
	}

	// Check for changes
	if !d.ipSlicesEqual(state.IPs, currentIPs) {
		// IP change detected
		change := IPChange{
			Timestamp:       currentTime,
			OldIPs:          state.IPs,
			NewIPs:          currentIPs,
			ChangeType:      "ip_changed",
			IPType:          ipType,
			NumIngress:      numIngress,
			AnnotationValue: annotationValue,
			Namespace:       service.Namespace,
			Name:            service.Name,
		}

		state.IPs = currentIPs
		state.LastSeen = currentTime
		state.Changes = append(state.Changes, change)

		// Trim history if needed
		if len(state.Changes) > d.Config.HistorySize {
			state.Changes = state.Changes[1:]
		}

		d.logChange(change)
	} else {
		// Service updated but no IP change
		state.LastSeen = currentTime

		// Check if annotation changed
		oldAnnotationValue := ""
		if len(state.Changes) > 0 {
			oldAnnotationValue = state.Changes[len(state.Changes)-1].AnnotationValue
		}
		annotationChanged := oldAnnotationValue != annotationValue

		// Log annotation changes or all updates in debug mode
		if annotationChanged || d.Config.LogLevel == "debug" {
			changeType := "updated"
			if annotationChanged {
				changeType = "annotation_changed"
			}

			change := IPChange{
				Timestamp:       currentTime,
				OldIPs:          state.IPs,
				NewIPs:          currentIPs,
				ChangeType:      changeType,
				IPType:          ipType,
				NumIngress:      numIngress,
				AnnotationValue: annotationValue,
				Namespace:       service.Namespace,
				Name:            service.Name,
			}
			d.logChange(change)
		}
	}
}

// handleServiceDeletion processes service deletion
func (d *ServiceDebugger) handleServiceDeletion(namespace, name string) {
	serviceKey := fmt.Sprintf("%s/%s", namespace, name)

	if state, exists := serviceStates[serviceKey]; exists {
		change := IPChange{
			Timestamp:       time.Now(),
			OldIPs:          state.IPs,
			NewIPs:          []string{},
			ChangeType:      "deleted",
			IPType:          d.classifyIPs(state.IPs),
			NumIngress:      0,
			AnnotationValue: "",
			Namespace:       namespace,
			Name:            name,
		}

		d.logChange(change)
		delete(serviceStates, serviceKey)
	}
}

// extractIPs extracts IPs from service LoadBalancer status
func (d *ServiceDebugger) extractIPs(service *corev1.Service) []string {
	var ips []string

	if len(service.Status.LoadBalancer.Ingress) > 0 {
		for _, ingress := range service.Status.LoadBalancer.Ingress {
			if ingress.IP != "" {
				ips = append(ips, ingress.IP)
			}
		}
	}

	return ips
}

// classifyIPs classifies IPs as loadbalancer, multiple, or unknown
func (d *ServiceDebugger) classifyIPs(ips []string) string {
	if len(ips) == 0 {
		return "unknown"
	}

	if len(ips) > 1 {
		return "multiple"
	}

	return "loadbalancer"
}

// hasPortForwardAnnotation gets the port forwarding annotation value from service
func (d *ServiceDebugger) hasPortForwardAnnotation(service *corev1.Service) string {
	if service.Annotations == nil {
		return ""
	}
	return service.Annotations["kube-port-forward-controller/ports"]
}

// ipSlicesEqual checks if two string slices are equal
func (d *ServiceDebugger) ipSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	aMap := make(map[string]bool)
	for _, ip := range a {
		aMap[ip] = true
	}

	for _, ip := range b {
		if !aMap[ip] {
			return false
		}
	}

	return true
}

// logChange outputs the change in the configured format
func (d *ServiceDebugger) logChange(change IPChange) {
	switch d.Config.OutputFormat {
	case "json":
		d.logChangeJSON(change)
	default:
		d.logChangeText(change)
	}
}

// logChangeText outputs change in human-readable text format
func (d *ServiceDebugger) logChangeText(change IPChange) {
	timestamp := change.Timestamp.Format("2006-01-02T15:04:05Z")

	var eventType string
	var icon string

	switch change.ChangeType {
	case "created":
		eventType = "CREATED"
		icon = "🟢"
	case "deleted":
		eventType = "DELETED"
		icon = "🔴"
	case "ip_changed":
		eventType = "IP_CHANGED"
		icon = "🔄"
	case "updated":
		eventType = "UPDATED"
		icon = "📝"
	default:
		eventType = strings.ToUpper(change.ChangeType)
		icon = "❓"
	}

	fmt.Printf("%s [%s] %s %s/%s\n", icon, timestamp, eventType, change.Namespace, change.Name)

	if change.ChangeType == "ip_changed" {
		fmt.Printf("   IP_CHANGE: %v -> %v\n", change.OldIPs, change.NewIPs)
		fmt.Printf("   IP_TYPE: %s\n", change.IPType)
	} else if len(change.NewIPs) > 0 {
		fmt.Printf("   IPs: %v (type: %s)\n", change.NewIPs, change.IPType)
	}

	fmt.Printf("   LB_STATUS: %d ingress entries\n", change.NumIngress)
	fmt.Printf("   ANNOTATIONS: kube-port-forward-controller/ports=%s\n", change.AnnotationValue)

	// Add warnings for potential issues
	if change.IPType == "multiple" {
		fmt.Printf("   ⚠️  WARNING: Multiple IPs detected - may cause port forwarding issues\n")
	}

	fmt.Println()
}

// logChangeJSON outputs change in JSON format
func (d *ServiceDebugger) logChangeJSON(change IPChange) {
	data, err := json.Marshal(change)
	if err != nil {
		fmt.Printf("Failed to marshal change to JSON: %v\n", err)
		return
	}
	fmt.Println(string(data))
}

// startStatusChecker periodically checks service status for changes that might not trigger events
func (d *ServiceDebugger) startStatusChecker() {
	ticker := time.NewTicker(d.Config.PollInterval)
	defer ticker.Stop()

	for range ticker.C {
		d.checkAllServices()
	}
}

// checkAllServices checks all services for status changes
func (d *ServiceDebugger) checkAllServices() {
	services := &corev1.ServiceList{}

	listOpts := []client.ListOption{}
	if d.Config.Namespace != "" {
		listOpts = append(listOpts, client.InNamespace(d.Config.Namespace))
	}
	if d.Config.LabelSelector != "" {
		labelSelector, err := metav1.ParseToLabelSelector(d.Config.LabelSelector)
		if err != nil {
			log.Printf("Invalid label selector: %v", err)
			return
		}
		selector, err := metav1.LabelSelectorAsSelector(labelSelector)
		if err != nil {
			log.Printf("Invalid label selector conversion: %v", err)
			return
		}
		listOpts = append(listOpts, client.MatchingLabelsSelector{Selector: selector})
	}

	if err := d.List(context.Background(), services, listOpts...); err != nil {
		log.Printf("Failed to list services: %v", err)
		return
	}

	for _, service := range services.Items {
		serviceKey := fmt.Sprintf("%s/%s", service.Namespace, service.Name)
		if state, exists := serviceStates[serviceKey]; exists {
			// Check for status changes that might not have triggered events
			currentIPs := d.extractIPs(&service)
			if !d.ipSlicesEqual(state.IPs, currentIPs) {
				log.Printf("Status change detected: service=%s, old_ips=%v, new_ips=%v",
					serviceKey, state.IPs, currentIPs)
				d.handleServiceChange(&service)
			}
		}
	}
}

// setupGracefulShutdown sets up signal handling
func setupGracefulShutdown() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Received shutdown signal, gracefully stopping...")

		// Print summary before exiting
		printSummary()
		os.Exit(0)
	}()
}

// printSummary prints a summary of all tracked services
func printSummary() {
	fmt.Println("\n=== SERVICE DEBUGGER SUMMARY ===")

	for key, state := range serviceStates {
		fmt.Printf("\nService: %s\n", key)
		fmt.Printf("  Current IPs: %v\n", state.IPs)
		fmt.Printf("  Last Seen: %s\n", state.LastSeen.Format("2006-01-02T15:04:05Z"))
		fmt.Printf("  Total Changes: %d\n", len(state.Changes))

		if len(state.Changes) > 0 {
			fmt.Printf("  Recent Changes:\n")
			for i, change := range state.Changes {
				if i >= 5 { // Show last 5 changes
					break
				}
				fmt.Printf("    %s: %s (%v -> %v)\n",
					change.Timestamp.Format("15:04:05"),
					change.ChangeType,
					change.OldIPs,
					change.NewIPs)
			}
		}
	}

	fmt.Println("\n=== END SUMMARY ===")
}
