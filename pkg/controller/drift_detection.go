package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/filipowm/go-unifi/unifi"
	"unifi-port-forwarder/pkg/config"
	"unifi-port-forwarder/pkg/helpers"
	"unifi-port-forwarder/pkg/routers"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// DriftAnalysis contains the analysis of drift for a single service
type DriftAnalysis struct {
	ServiceName  string
	Service      *corev1.Service
	DesiredRules []routers.PortConfig
	CurrentRules []*unifi.PortForward

	// Drift categories
	MissingRules []routers.PortConfig // Need to be created
	WrongRules   []RuleMismatch       // Need to be updated (name wrong, IP wrong, etc.)
	ExtraRules   []*unifi.PortForward // Our rules that shouldn't exist
	HasDrift     bool
}

// RuleMismatch represents a rule that doesn't match desired configuration
type RuleMismatch struct {
	Current      *unifi.PortForward
	Desired      routers.PortConfig
	MismatchType string // "name", "ip", "port", "protocol", "enabled", "ownership"
}

// DriftDetector analyzes drift between desired state and actual router state
type DriftDetector struct {
	client.Client
	Router routers.Router
}

// AnalyzeAllServicesDrift performs drift analysis for all managed services
func (d *DriftDetector) AnalyzeAllServicesDrift(ctx context.Context, services []*corev1.Service, allRouterRules []*unifi.PortForward) ([]*DriftAnalysis, error) {
	logger := ctrllog.FromContext(ctx).WithValues("component", "drift-detector")

	var analyses []*DriftAnalysis

	for _, service := range services {
		logger.V(1).Info("Analyzing drift for service", "service", fmt.Sprintf("%s/%s", service.Namespace, service.Name))

		analysis, err := d.analyzeServiceDrift(ctx, service, allRouterRules)
		if err != nil {
			logger.Error(err, "Failed to analyze drift for service", "service", fmt.Sprintf("%s/%s", service.Namespace, service.Name))
			return nil, fmt.Errorf("failed to analyze drift for service %s/%s: %w", service.Namespace, service.Name, err)
		}

		analyses = append(analyses, analysis)
	}

	return analyses, nil
}

// analyzeServiceDrift performs drift analysis for a single service
func (d *DriftDetector) analyzeServiceDrift(ctx context.Context, service *corev1.Service, allRouterRules []*unifi.PortForward) (*DriftAnalysis, error) {
	analysis := &DriftAnalysis{
		ServiceName:  fmt.Sprintf("%s/%s", service.Namespace, service.Name),
		Service:      service,
		CurrentRules: []*unifi.PortForward{},
		HasDrift:     false,
	}

	// 1. Get desired rules for this service
	desiredRules, err := d.calculateDesiredRulesForService(service)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate desired rules: %w", err)
	}
	analysis.DesiredRules = desiredRules

	// 2. Filter current router rules to only those belonging to this service
	for _, rule := range allRouterRules {
		if strings.HasPrefix(rule.Name, analysis.ServiceName+":") {
			analysis.CurrentRules = append(analysis.CurrentRules, rule)
		}
	}

	// 3. Find rules that match our desired port+protocol but have different names (aggressive ownership)
	d.findMatchingRulesByPortAndProtocol(analysis, allRouterRules)

	// 4. Analyze differences between desired and current rules
	d.analyzeDesiredVsCurrent(analysis)

	return analysis, nil
}

// calculateDesiredRulesForService calculates desired port configurations for a service
func (d *DriftDetector) calculateDesiredRulesForService(service *corev1.Service) ([]routers.PortConfig, error) {
	lbIP := helpers.GetLBIP(service)
	if lbIP == "" {
		return nil, fmt.Errorf("service has no LoadBalancer IP")
	}

	portConfigs, err := helpers.GetPortConfigs(service, lbIP, config.FilterAnnotation)
	if err != nil {
		return nil, fmt.Errorf("failed to get port configurations: %w", err)
	}

	return portConfigs, nil
}

// findMatchingRulesByPortAndProtocol finds router rules that match desired port+protocol
// This implements the aggressive ownership strategy - take ownership of any matching rule regardless of name
func (d *DriftDetector) findMatchingRulesByPortAndProtocol(analysis *DriftAnalysis, allRouterRules []*unifi.PortForward) {
	// Build map of all current router rules by port+protocol for O(1) lookup
	currentMap := make(map[string]*unifi.PortForward)
	for _, rule := range allRouterRules {
		dstPort := d.parseIntField(rule.DstPort)
		key := fmt.Sprintf("%d-%s", dstPort, rule.Proto)
		currentMap[key] = rule
	}

	// Check each desired rule for potential ownership conflicts
	for _, desiredRule := range analysis.DesiredRules {
		key := fmt.Sprintf("%d-%s", desiredRule.DstPort, desiredRule.Protocol)

		if existingRule, exists := currentMap[key]; exists {
			// Found matching port+protocol - check if we need to take ownership
			shouldTakeOwnership := false
			mismatchType := ""

			if !strings.HasPrefix(existingRule.Name, analysis.ServiceName+":") {
				// Rule exists but doesn't belong to this service - take ownership
				shouldTakeOwnership = true
				mismatchType = "ownership"
			} else if existingRule.Name != desiredRule.Name {
				// Rule belongs to us but has wrong name
				shouldTakeOwnership = true
				mismatchType = "name"
			} else if existingRule.Fwd != desiredRule.DstIP {
				// Rule belongs to us but has wrong destination IP
				shouldTakeOwnership = true
				mismatchType = "ip"
			} else if existingRule.Enabled != desiredRule.Enabled {
				// Rule belongs to us but wrong enabled state
				shouldTakeOwnership = true
				mismatchType = "enabled"
			}

			if shouldTakeOwnership {
				analysis.WrongRules = append(analysis.WrongRules, RuleMismatch{
					Current:      existingRule,
					Desired:      desiredRule,
					MismatchType: mismatchType,
				})
				analysis.HasDrift = true
			}
		}
	}
}

// analyzeDesiredVsCurrent analyzes differences between desired and current service rules
func (d *DriftDetector) analyzeDesiredVsCurrent(analysis *DriftAnalysis) {
	// Build map of desired rules by port+protocol
	desiredMap := make(map[string]routers.PortConfig)
	for _, rule := range analysis.DesiredRules {
		key := fmt.Sprintf("%d-%s", rule.DstPort, rule.Protocol)
		desiredMap[key] = rule
	}

	// Build map of current rules by port+protocol (only those belonging to this service)
	currentMap := make(map[string]*unifi.PortForward)
	for _, rule := range analysis.CurrentRules {
		dstPort := d.parseIntField(rule.DstPort)
		key := fmt.Sprintf("%d-%s", dstPort, rule.Proto)
		currentMap[key] = rule
	}

	// Find missing rules (exist in desired but not current)
	for portKey, desiredRule := range desiredMap {
		if _, exists := currentMap[portKey]; !exists {
			analysis.MissingRules = append(analysis.MissingRules, desiredRule)
			analysis.HasDrift = true
		}
	}

	// Find extra rules (exist in current but not desired)
	for portKey, currentRule := range currentMap {
		if _, exists := desiredMap[portKey]; !exists {
			analysis.ExtraRules = append(analysis.ExtraRules, currentRule)
			analysis.HasDrift = true
		}
	}
}

// parseIntField safely parses a string field to int
func (d *DriftDetector) parseIntField(field string) int {
	if field == "" {
		return 0
	}
	if result, err := strconv.Atoi(field); err == nil {
		return result
	}
	return 0
}
