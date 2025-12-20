package routers

import (
	"fmt"
	"strings"
)

// ProtocolNormalizer handles protocol string normalization and validation
type ProtocolNormalizer struct {
	aliases map[string]string
}

// NewProtocolNormalizer creates a new protocol normalizer with standard aliases
func NewProtocolNormalizer() *ProtocolNormalizer {
	aliases := map[string]string{
		// Common aliases and case variations
		"TCP":     "tcp",
		"tcp":     "tcp",
		"UDP":     "udp",
		"udp":     "udp",
		"TCP_UDP": "tcp_udp",
		"tcp_udp": "tcp_udp",
		"tcp-udp": "tcp_udp",
		"TCP/UDP": "tcp_udp",
		"TCP-UDP": "tcp_udp",
		"tcp/udp": "tcp_udp",

		// Additional variations that might appear
		"TCPv4":    "tcp",
		"UDPv4":    "udp",
		"IPv4-TCP": "tcp",
		"IPv4-UDP": "udp",
	}

	return &ProtocolNormalizer{
		aliases: aliases,
	}
}

// NormalizeProtocol normalizes a protocol string to standard format
func (n *ProtocolNormalizer) NormalizeProtocol(protocol string) (string, error) {
	if protocol == "" {
		return "", &ProtocolValidationError{
			Protocol:     protocol,
			ValidOptions: []string{"tcp", "udp", "tcp_udp"},
			Suggestions:  []string{"Use 'tcp', 'udp', or 'tcp_udp'"},
			Message:      "protocol cannot be empty",
		}
	}

	// Clean up the protocol string
	normalized := strings.TrimSpace(protocol)
	normalized = strings.ToUpper(normalized)

	// Replace common separators with underscore
	normalized = strings.ReplaceAll(normalized, "/", "_")
	normalized = strings.ReplaceAll(normalized, "-", "_")

	// Check if it's a known alias
	if alias, exists := n.aliases[normalized]; exists {
		return alias, nil
	}

	// If no alias found, check if it's already a valid protocol
	if isValidProtocol(normalized) {
		return strings.ToLower(normalized), nil
	}

	return "", &ProtocolValidationError{
		Protocol:     protocol,
		ValidOptions: []string{"tcp", "udp", "tcp_udp"},
		Suggestions:  generateProtocolSuggestions(protocol),
		Message:      fmt.Sprintf("protocol '%s' is not supported", protocol),
	}
}

// NormalizeProtocolWithLogging normalizes a protocol and provides helpful logging
func (n *ProtocolNormalizer) NormalizeProtocolWithLogging(originalProtocol string) (string, []string) {
	var suggestions []string
	var normalized string

	if originalProtocol == "" {
		suggestions = []string{"Protocol cannot be empty. Use 'tcp', 'udp', or 'tcp_udp'"}
		return "", suggestions
	}

	normalized, err := n.NormalizeProtocol(originalProtocol)
	if err != nil {
		if validationErr, ok := err.(*ProtocolValidationError); ok {
			suggestions = validationErr.Suggestions
		} else {
			suggestions = []string{err.Error()}
		}
		return "", suggestions
	}

	// If we normalized from something else, log it
	cleanOriginal := strings.TrimSpace(strings.ToUpper(originalProtocol))
	if cleanOriginal != normalized {
		suggestions = append(suggestions,
			fmt.Sprintf("Protocol '%s' normalized to '%s'", originalProtocol, normalized))
	}

	return normalized, suggestions
}

// AreProtocolsCompatible checks if two protocols are compatible for port forward rules
func (n *ProtocolNormalizer) AreProtocolsCompatible(proto1, proto2 string) bool {
	norm1, err1 := n.NormalizeProtocol(proto1)
	norm2, err2 := n.NormalizeProtocol(proto2)

	if err1 != nil || err2 != nil {
		return false
	}

	// Exact match
	if norm1 == norm2 {
		return true
	}

	// tcp_udp is compatible with both tcp and udp
	if norm1 == "tcp_udp" && (norm2 == "tcp" || norm2 == "udp") {
		return true
	}
	if norm2 == "tcp_udp" && (norm1 == "tcp" || norm1 == "udp") {
		return true
	}

	return false
}

// GetProtocolVariations returns all possible variations of a normalized protocol
func (n *ProtocolNormalizer) GetProtocolVariations(normalizedProtocol string) []string {
	if !isValidProtocol(normalizedProtocol) {
		return []string{}
	}

	var variations []string

	// Add the normalized form
	variations = append(variations, normalizedProtocol)

	// Add common variations
	switch normalizedProtocol {
	case "tcp":
		variations = append(variations, "TCP", "tcp")
	case "udp":
		variations = append(variations, "UDP", "udp")
	case "tcp_udp":
		variations = append(variations, "TCP_UDP", "tcp_udp", "TCP-UDP", "tcp-udp", "TCP/UDP", "tcp/udp")
	}

	return variations
}

// isValidProtocol checks if a protocol string is a valid normalized protocol
func isValidProtocol(protocol string) bool {
	validProtocols := []string{"tcp", "udp", "tcp_udp"}

	for _, valid := range validProtocols {
		if protocol == valid {
			return true
		}
	}

	return false
}

// generateProtocolSuggestions creates helpful suggestions for invalid protocols
func generateProtocolSuggestions(invalidProtocol string) []string {
	invalidUpper := strings.ToUpper(invalidProtocol)

	// Check for common typos
	suggestions := []string{
		"Use 'tcp' for TCP traffic",
		"Use 'udp' for UDP traffic",
		"Use 'tcp_udp' for both TCP and UDP traffic",
	}

	// Specific suggestions based on what was entered
	if strings.Contains(invalidUpper, "TCP") && strings.Contains(invalidUpper, "UDP") {
		suggestions = append(suggestions, "For both TCP and UDP, use 'tcp_udp'")
	} else if strings.Contains(invalidUpper, "TCP") {
		suggestions = append(suggestions, "For TCP traffic, use 'tcp'")
	} else if strings.Contains(invalidUpper, "UDP") {
		suggestions = append(suggestions, "For UDP traffic, use 'udp'")
	}

	if strings.Contains(invalidProtocol, "/") {
		suggestions = append(suggestions, "Use underscore '_' instead of slash '/'")
	}

	if strings.Contains(invalidProtocol, "-") {
		suggestions = append(suggestions, "Use underscore '_' instead of dash '-'")
	}

	return suggestions
}

// ProtocolValidationError represents a protocol validation error
type ProtocolValidationError struct {
	Protocol     string   `json:"protocol"`
	ValidOptions []string `json:"valid_options"`
	Suggestions  []string `json:"suggestions"`
	Message      string   `json:"message"`
}

// Error implements the error interface
func (e *ProtocolValidationError) Error() string {
	return e.Message
}

// DetailedMessage returns a more detailed error message
func (e *ProtocolValidationError) DetailedMessage() string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("Protocol '%s' is invalid. %s", e.Protocol, e.Message))

	builder.WriteString("\nValid options:")
	for _, option := range e.ValidOptions {
		builder.WriteString(fmt.Sprintf("\n  - %s", option))
	}

	if len(e.Suggestions) > 0 {
		builder.WriteString("\nSuggestions:")
		for _, suggestion := range e.Suggestions {
			builder.WriteString(fmt.Sprintf("\n  - %s", suggestion))
		}
	}

	return builder.String()
}
