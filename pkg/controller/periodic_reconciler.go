package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"unifi-port-forward/pkg/config"
	"unifi-port-forward/pkg/helpers"
	"unifi-port-forward/pkg/routers"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// PeriodicReconciler handles periodic full reconciliation to detect and correct drift
// between Kubernetes Service state and UniFi router port forwarding rules.
type PeriodicReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Router routers.Router
	Config *config.Config

	// Periodic reconciliation specific
	ticker         *time.Ticker
	stopCh         chan struct{}
	eventPublisher *EventPublisher
	recorder       record.EventRecorder

	// Fixed interval - no configuration needed (15 minutes)  ### Fixed interval as specified TODO should be configurable from ENV
	interval time.Duration

	// Concurrency control
	semaphore             chan struct{}
	activeReconciliations sync.WaitGroup
}

// NewPeriodicReconciler creates a new periodic reconciler with fixed 15-minute intervals
func NewPeriodicReconciler(client client.Client, scheme *runtime.Scheme, router routers.Router, config *config.Config, eventPublisher *EventPublisher, recorder record.EventRecorder) *PeriodicReconciler {
	return &PeriodicReconciler{
		Client:         client,
		Scheme:         scheme,
		Router:         router,
		Config:         config,
		eventPublisher: eventPublisher,
		recorder:       recorder,
		interval:       15 * time.Minute, // Fixed interval as specified TODO should be configurable from ENV
		stopCh:         make(chan struct{}),
		semaphore:      make(chan struct{}, 3), // Max 3 concurrent reconciliations
	}
}

// Start begins the periodic reconciliation loop
func (r *PeriodicReconciler) Start(ctx context.Context) error {
	logger := ctrllog.FromContext(ctx).WithValues("component", "periodic-reconciler")
	logger.V(1).Info("Starting periodic reconciler", "interval", r.interval.String())

	r.ticker = time.NewTicker(r.interval)
	defer r.ticker.Stop()

	if err := r.performInitialReconciliation(ctx); err != nil {
		logger.Error(err, "Initial reconciliation failed")
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info("Periodic reconciler stopped due to context cancellation")
			return ctx.Err()
		case <-r.stopCh:
			logger.Info("Periodic reconciler stopped via stop channel")
			return nil
		case <-r.ticker.C:
			if err := r.performPeriodicReconciliation(ctx); err != nil {
				logger.Error(err, "Periodic reconciliation cycle failed")
			}
		}
	}
}

// Stop gracefully shuts down the periodic reconciler
func (r *PeriodicReconciler) Stop() error {
	logger := ctrllog.FromContext(context.Background()).WithValues("component", "periodic-reconciler")

	close(r.stopCh)

	if r.ticker != nil {
		r.ticker.Stop()
	}

	// Wait for all active reconciliations to complete
	r.activeReconciliations.Wait()

	logger.Info("Periodic reconciler stopped successfully")
	return nil
}

// performInitialReconciliation performs the first reconciliation when the controller starts
func (r *PeriodicReconciler) performInitialReconciliation(ctx context.Context) error {
	startTime := time.Now()
	return r.performFullReconciliation(ctx, startTime)
}

// performPeriodicReconciliation executes a full reconciliation cycle
func (r *PeriodicReconciler) performPeriodicReconciliation(ctx context.Context) error {
	startTime := time.Now()

	return r.performFullReconciliation(ctx, startTime)
}

// performFullReconciliation performs the complete reconciliation process
func (r *PeriodicReconciler) performFullReconciliation(ctx context.Context, startTime time.Time) error {
	logger := ctrllog.FromContext(ctx).WithValues("component", "periodic-reconciler")

	allRouterRules, err := r.Router.ListAllPortForwards(ctx)
	if err != nil {
		return fmt.Errorf("failed to list router rules: %w", err)
	}
	logger.V(1).Info("Retrieved router rules", "count", len(allRouterRules))

	managedServices, err := r.getAllManagedServices(ctx)
	if err != nil {
		return fmt.Errorf("failed to get managed services: %w", err)
	}
	logger.V(1).Info("Retrieved managed services", "count", len(managedServices))

	driftDetector := &DriftDetector{Client: r.Client, Router: r.Router}
	driftAnalyses, err := driftDetector.AnalyzeAllServicesDrift(ctx, managedServices, allRouterRules)
	if err != nil {
		return fmt.Errorf("failed to analyze drift: %w", err)
	}

	logger.V(1).Info("Starting periodic reconciliation cycle", "total_services", len(driftAnalyses))

	servicesWithDrift := 0
	correctedRules := 0
	failedOperations := 0

	for _, analysis := range driftAnalyses {
		if analysis.HasDrift {
			service := analysis.Service
			servicesWithDrift++
			logger.Info("Drift detected for service",
				"service", analysis.ServiceName,
				"missing_rules", len(analysis.MissingRules),
				"wrong_rules", len(analysis.WrongRules),
				"extra_rules", len(analysis.ExtraRules))

			if r.eventPublisher != nil {
				r.eventPublisher.PublishDriftDetectedEvent(ctx, service, analysis)
			}

			if err := r.correctServiceDrift(ctx, analysis); err != nil {
				logger.Error(err, "Failed to correct drift for service", "service", analysis.ServiceName)
				failedOperations++

				if r.eventPublisher != nil {
					r.eventPublisher.PublishDriftCorrectionFailedEvent(ctx, service, analysis, err)
				}

				// Publish periodic reconciliation completed event for this service (failure) - ONLY when drift existed
				if r.eventPublisher != nil {
					r.eventPublisher.PublishServicePeriodicReconciliationCompletedEvent(ctx, service, true, 0, 1)
				}
			} else {
				rulesCorrected := len(analysis.MissingRules) + len(analysis.WrongRules) + len(analysis.ExtraRules)
				correctedRules += rulesCorrected

				if r.eventPublisher != nil {
					r.eventPublisher.PublishDriftCorrectedEvent(ctx, service, analysis)
				}

				// Publish periodic reconciliation completed event for this service (success) - ONLY when drift existed
				if r.eventPublisher != nil {
					r.eventPublisher.PublishServicePeriodicReconciliationCompletedEvent(ctx, service, true, rulesCorrected, 0)
				}
			}
		}
	}

	duration := time.Since(startTime)
	logger.Info("Periodic reconciliation completed",
		"total_services", len(managedServices),
		"services_with_drift", servicesWithDrift,
		"corrected_rules", correctedRules,
		"failed_operations", failedOperations,
		"duration", duration.String())

	return nil
}

// correctServiceDrift applies corrections for a service that has drift
func (r *PeriodicReconciler) correctServiceDrift(ctx context.Context, analysis *DriftAnalysis) error {
	_ = ctrllog.FromContext(ctx).WithValues("component", "periodic-reconciler", "service", analysis.ServiceName)

	var operations []PortOperation
	var createOperations []PortOperation

	// Process DELETE operations first to free up ports for CREATE operations
	for _, wrongRule := range analysis.WrongRules {
		// Classify mismatch type for smart operation selection
		if isSafeUpdate(wrongRule.MismatchType) {
			// Safe change: direct update (can be processed immediately)
			operations = append(operations, PortOperation{
				Type:         OpUpdate,
				Config:       wrongRule.Desired,
				ExistingRule: wrongRule.Current,
				Reason:       "drift_wrong_rule_safe",
			})
		} else {
			// Risky change: delete then recreate
			operations = append(operations, PortOperation{
				Type: OpDelete,
				Config: routers.PortConfig{
					// Copy from current rule for deletion
					Name:      wrongRule.Current.Name,
					DstPort:   helpers.ParseIntField(wrongRule.Current.DstPort),
					FwdPort:   helpers.ParseIntField(wrongRule.Current.FwdPort),
					DstIP:     wrongRule.Current.Fwd,
					Protocol:  wrongRule.Current.Proto,
					Enabled:   wrongRule.Current.Enabled,
					Interface: wrongRule.Current.PfwdInterface,
					SrcIP:     wrongRule.Current.Src,
				},
				ExistingRule: wrongRule.Current,
				Reason:       "drift_wrong_rule_delete",
			})

			// Defer CREATE operation until after all DELETEs
			createOperations = append(createOperations, PortOperation{
				Type:   OpCreate,
				Config: wrongRule.Desired,
				Reason: "drift_wrong_rule_create",
			})
		}
	}

	for _, extraRule := range analysis.ExtraRules {
		operations = append(operations, PortOperation{
			Type: OpDelete,
			Config: routers.PortConfig{
				Name:      extraRule.Name,
				DstPort:   helpers.ParseIntField(extraRule.DstPort),
				FwdPort:   helpers.ParseIntField(extraRule.FwdPort),
				DstIP:     extraRule.Fwd,
				Protocol:  extraRule.Proto,
				Enabled:   extraRule.Enabled,
				Interface: extraRule.PfwdInterface,
				SrcIP:     extraRule.Src,
			},
			ExistingRule: extraRule,
			Reason:       "drift_extra_rule",
		})
	}

	// Process CREATE operations last (after all DELETEs to avoid conflicts)
	for _, missingRule := range analysis.MissingRules {
		createOperations = append(createOperations, PortOperation{
			Type:   OpCreate,
			Config: missingRule,
			Reason: "drift_missing_rule",
		})
	}

	// Add all CREATE operations to the end of the operations list
	operations = append(operations, createOperations...)

	result, err := r.executeOperations(ctx, operations)
	if err != nil {
		return fmt.Errorf("failed to execute drift correction operations: %w", err)
	}

	if len(result.Failed) > 0 {
		return fmt.Errorf("%d operations failed during drift correction", len(result.Failed))
	}

	return nil
}

// executeOperations executes port operations with proper error handling
// This reuses the existing operation execution logic from unified_operations.go
func (r *PeriodicReconciler) executeOperations(ctx context.Context, operations []PortOperation) (*OperationResult, error) {
	// Create a temporary reconciler to reuse existing operation execution logic
	tempReconciler := &PortForwardReconciler{
		Client:         r.Client,
		Scheme:         r.Scheme,
		Router:         r.Router,
		Config:         r.Config,
		EventPublisher: r.eventPublisher,
		Recorder:       r.recorder,
	}

	return tempReconciler.executeOperations(ctx, operations)
}

// getAllManagedServices retrieves all Kubernetes services that should be managed by the controller
func (r *PeriodicReconciler) getAllManagedServices(ctx context.Context) ([]*corev1.Service, error) {
	logger := ctrllog.FromContext(ctx).WithValues("component", "periodic-reconciler")

	var services corev1.ServiceList
	if err := r.List(ctx, &services, client.InNamespace("")); err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	var managedServices []*corev1.Service
	for i := range services.Items {
		service := &services.Items[i]

		if r.shouldManageService(service) {
			managedServices = append(managedServices, service)
		}
	}

	logger.V(1).Info("Filtered managed services",
		"total", len(services.Items),
		"managed", len(managedServices))

	return managedServices, nil
}

// shouldManageService checks if a service should be managed by the periodic reconciler
func (r *PeriodicReconciler) shouldManageService(service *corev1.Service) bool {
	annotations := service.GetAnnotations()
	if annotations == nil {
		return false
	}

	_, hasPortAnnotation := annotations[config.FilterAnnotation]
	if !hasPortAnnotation {
		return false
	}

	lbIP := helpers.GetLBIP(service)
	return lbIP != ""
}
