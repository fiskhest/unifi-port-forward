package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/filipowm/go-unifi/unifi"
	"unifi-port-forward/pkg/config"
	"unifi-port-forward/pkg/helpers"
	"unifi-port-forward/pkg/routers"

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
	processedRules := d.findMatchingRulesByPortAndProtocol(analysis, allRouterRules)

	// 4. Analyze differences between desired and current rules
	// Skip rules already processed in findMatchingRulesByPortAndProtocol to avoid duplicate classifications
	d.analyzeDesiredVsCurrent(analysis, processedRules)

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
// Returns list of processed rule IDs to avoid duplicate classifications in analyzeDesiredVsCurrent
func (d *DriftDetector) findMatchingRulesByPortAndProtocol(analysis *DriftAnalysis, allRouterRules []*unifi.PortForward) map[string]bool {
	processedRules := make(map[string]bool)

	// Build two maps for comprehensive rule matching:
	// 1. exactMatchMap: by dstPort+fwdPort+protocol for exact matches
	// 2. dstPortOnlyMap: by dstPort+protocol for detecting FwdPort mismatches
	exactMatchMap := make(map[string]*unifi.PortForward)
	dstPortOnlyMap := make(map[string][]*unifi.PortForward) // one dstPort can have multiple rules with different fwdPorts

	for _, rule := range allRouterRules {
		dstPort := helpers.ParseIntField(rule.DstPort)
		fwdPort := helpers.ParseIntField(rule.FwdPort)
		exactKey := fmt.Sprintf("%d-%d-%s", dstPort, fwdPort, rule.Proto)
		exactMatchMap[exactKey] = rule

		dstPortOnlyKey := fmt.Sprintf("%d-%s", dstPort, rule.Proto)
		dstPortOnlyMap[dstPortOnlyKey] = append(dstPortOnlyMap[dstPortOnlyKey], rule)
	}

	// Check each desired rule for potential ownership conflicts
	for _, desiredRule := range analysis.DesiredRules {
		exactKey := fmt.Sprintf("%d-%d-%s", desiredRule.DstPort, desiredRule.FwdPort, desiredRule.Protocol)
		dstPortOnlyKey := fmt.Sprintf("%d-%s", desiredRule.DstPort, desiredRule.Protocol)

		// First check for exact match (full dstPort+fwdPort+protocol match)
		if existingRule, exists := exactMatchMap[exactKey]; exists {
			// Found exact match - check if we need to take ownership
			shouldTakeOwnership := false
			mismatchType := ""

			if !strings.HasPrefix(existingRule.Name, analysis.ServiceName+":") {
				shouldTakeOwnership = true
				mismatchType = "ownership"
			} else if existingRule.Name != desiredRule.Name {
				shouldTakeOwnership = true
				mismatchType = "name"
			} else if existingRule.Fwd != desiredRule.DstIP {
				shouldTakeOwnership = true
				mismatchType = "ip"
			} else if existingRule.Enabled != desiredRule.Enabled {
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

				// Mark this rule as processed to avoid duplicate classification
				if existingRule.ID != "" {
					processedRules[existingRule.ID] = true
				}
			}
		} else {
			// No exact match found - check if there are rules with same dstPort+protocol but different fwdPort
			if matchingRules, exists := dstPortOnlyMap[dstPortOnlyKey]; exists {
				for _, existingRule := range matchingRules {
					// This rule matches dstPort+protocol but has different fwdPort
					// This is the case where user manually changed FwdPort on router
					shouldTakeOwnership := false
					mismatchType := ""

					if !strings.HasPrefix(existingRule.Name, analysis.ServiceName+":") {
						shouldTakeOwnership = true
						mismatchType = "ownership"
					} else if existingRule.Name != desiredRule.Name {
						shouldTakeOwnership = true
						mismatchType = "name"
					} else if existingRule.Fwd != desiredRule.DstIP {
						shouldTakeOwnership = true
						mismatchType = "ip"
					} else if existingRule.Enabled != desiredRule.Enabled {
						shouldTakeOwnership = true
						mismatchType = "enabled"
					} else if existingRule.FwdPort != strconv.Itoa(desiredRule.FwdPort) {
						// FwdPort mismatch - don't classify as WrongRule, let it be handled as Extra+Missing
						// This allows proper DELETE+CREATE flow instead of WrongRule processing
						continue
					}

					if shouldTakeOwnership {
						analysis.WrongRules = append(analysis.WrongRules, RuleMismatch{
							Current:      existingRule,
							Desired:      desiredRule,
							MismatchType: mismatchType,
						})
						analysis.HasDrift = true

						// Mark this rule as processed to avoid duplicate classification
						if existingRule.ID != "" {
							processedRules[existingRule.ID] = true
						}
					}
				}
			}
		}
	}

	return processedRules
}

// analyzeDesiredVsCurrent analyzes differences between desired and current service rules
func (d *DriftDetector) analyzeDesiredVsCurrent(analysis *DriftAnalysis, processedRules map[string]bool) {
	// Build map of desired rules by port+forwardport+protocol
	// UniFi port forward rules are uniquely identified by DstPort+FwdPort+Protocol combination
	desiredMap := make(map[string]routers.PortConfig)
	for _, rule := range analysis.DesiredRules {
		key := fmt.Sprintf("%d-%d-%s", rule.DstPort, rule.FwdPort, rule.Protocol)
		desiredMap[key] = rule
	}

	// Build map of current rules by port+forwardport+protocol (only those belonging to this service)
	// Skip rules already processed in findMatchingRulesByPortAndProtocol to avoid duplicate classifications
	// Note: analysis.CurrentRules already filtered by service name in analyzeServiceDrift step 2
	currentMap := make(map[string]*unifi.PortForward)
	for _, rule := range analysis.CurrentRules {
		// Skip rules that were already processed by findMatchingRulesByPortAndProtocol
		if rule.ID != "" && processedRules[rule.ID] {
			continue
		}

		dstPort := helpers.ParseIntField(rule.DstPort)
		fwdPort := helpers.ParseIntField(rule.FwdPort)
		key := fmt.Sprintf("%d-%d-%s", dstPort, fwdPort, rule.Proto)
		currentMap[key] = rule
	}

	// Find missing rules (exist in desired but not current)
	for key, desiredRule := range desiredMap {
		if _, exists := currentMap[key]; !exists {
			analysis.MissingRules = append(analysis.MissingRules, desiredRule)
			analysis.HasDrift = true
		}
	}

	// Find extra rules (exist in current but not desired)
	for key, currentRule := range currentMap {
		if _, exists := desiredMap[key]; !exists {
			analysis.ExtraRules = append(analysis.ExtraRules, currentRule)
			analysis.HasDrift = true
		}
	}
}
