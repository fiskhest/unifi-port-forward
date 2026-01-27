package v1alpha1

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestPortForwardRule_ValidateCreate(t *testing.T) {
	tests := []struct {
		name        string
		rule        *PortForwardRule
		expectError bool
		errorType   field.ErrorType
	}{
		{
			name: "valid rule with service reference",
			rule: &PortForwardRule{
				Spec: PortForwardRuleSpec{
					ExternalPort:   8080,
					Protocol:       "tcp",
					Priority:       100,
					ConflictPolicy: "warn",
					ServiceRef: &ServiceReference{
						Name: "test-service",
						Port: "http",
					},
				},
			},
			expectError: false,
		},
		{
			name: "valid rule with destination IP",
			rule: &PortForwardRule{
				Spec: PortForwardRuleSpec{
					ExternalPort:    8080,
					Protocol:        "tcp",
					Priority:        100,
					ConflictPolicy:  "warn",
					DestinationIP:   stringPtr("192.168.1.100"),
					DestinationPort: intPtr(80),
				},
			},
			expectError: false,
		},
		{
			name: "invalid external port too low",
			rule: &PortForwardRule{
				Spec: PortForwardRuleSpec{
					ExternalPort:   0,
					Protocol:       "tcp",
					Priority:       100,
					ConflictPolicy: "warn",
					ServiceRef: &ServiceReference{
						Name: "test-service",
						Port: "http",
					},
				},
			},
			expectError: true,
			errorType:   field.ErrorTypeInvalid,
		},
		{
			name: "invalid external port too high",
			rule: &PortForwardRule{
				Spec: PortForwardRuleSpec{
					ExternalPort:   65536,
					Protocol:       "tcp",
					Priority:       100,
					ConflictPolicy: "warn",
					ServiceRef: &ServiceReference{
						Name: "test-service",
						Port: "http",
					},
				},
			},
			expectError: true,
			errorType:   field.ErrorTypeInvalid,
		},
		{
			name: "invalid protocol",
			rule: &PortForwardRule{
				Spec: PortForwardRuleSpec{
					ExternalPort:   8080,
					Protocol:       "invalid",
					Priority:       100,
					ConflictPolicy: "warn",
					ServiceRef: &ServiceReference{
						Name: "test-service",
						Port: "http",
					},
				},
			},
			expectError: true,
			errorType:   field.ErrorTypeNotSupported,
		},
		{
			name: "invalid destination IP",
			rule: &PortForwardRule{
				Spec: PortForwardRuleSpec{
					ExternalPort:    8080,
					Protocol:        "tcp",
					Priority:        100,
					ConflictPolicy:  "warn",
					DestinationIP:   stringPtr("invalid-ip"),
					DestinationPort: intPtr(80),
				},
			},
			expectError: true,
			errorType:   field.ErrorTypeInvalid,
		},
		{
			name: "priority too low",
			rule: &PortForwardRule{
				Spec: PortForwardRuleSpec{
					ExternalPort:   8080,
					Protocol:       "tcp",
					Priority:       -1,
					ConflictPolicy: "warn",
					ServiceRef: &ServiceReference{
						Name: "test-service",
						Port: "http",
					},
				},
			},
			expectError: true,
			errorType:   field.ErrorTypeInvalid,
		},
		{
			name: "priority too high",
			rule: &PortForwardRule{
				Spec: PortForwardRuleSpec{
					ExternalPort:   8080,
					Protocol:       "tcp",
					Priority:       1001,
					ConflictPolicy: "warn",
					ServiceRef: &ServiceReference{
						Name: "test-service",
						Port: "http",
					},
				},
			},
			expectError: true,
			errorType:   field.ErrorTypeInvalid,
		},
		{
			name: "both service ref and destination IP",
			rule: &PortForwardRule{
				Spec: PortForwardRuleSpec{
					ExternalPort:   8080,
					Protocol:       "tcp",
					Priority:       100,
					ConflictPolicy: "warn",
					ServiceRef: &ServiceReference{
						Name: "test-service",
						Port: "http",
					},
					DestinationIP: stringPtr("192.168.1.100"),
				},
			},
			expectError: true,
			errorType:   field.ErrorTypeForbidden,
		},
		{
			name: "neither service ref nor destination IP",
			rule: &PortForwardRule{
				Spec: PortForwardRuleSpec{
					ExternalPort:   8080,
					Protocol:       "tcp",
					Priority:       100,
					ConflictPolicy: "warn",
				},
			},
			expectError: true,
			errorType:   field.ErrorTypeRequired,
		},
		{
			name: "destination IP without destination port",
			rule: &PortForwardRule{
				Spec: PortForwardRuleSpec{
					ExternalPort:   8080,
					Protocol:       "tcp",
					Priority:       100,
					ConflictPolicy: "warn",
					DestinationIP:  stringPtr("192.168.1.100"),
				},
			},
			expectError: true,
			errorType:   field.ErrorTypeRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.rule.ValidateCreate()

			if tt.expectError && len(errs) == 0 {
				t.Errorf("Expected validation errors but got none")
			} else if !tt.expectError && len(errs) > 0 {
				t.Errorf("Expected no validation errors but got: %v", errs)
			} else if tt.expectError && len(errs) > 0 {
				// Check if expected error type is present
				found := false
				for _, err := range errs {
					if err.Type == tt.errorType {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected error type %v but got: %v", tt.errorType, errs)
				}
			}
		})
	}
}

func TestPortForwardRule_ValidateUpdate(t *testing.T) {
	oldRule := &PortForwardRule{
		Spec: PortForwardRuleSpec{
			ExternalPort:   8080,
			Protocol:       "tcp",
			Priority:       100,
			ConflictPolicy: "warn",
			ServiceRef: &ServiceReference{
				Name: "old-service",
				Port: "http",
			},
		},
	}

	newRule := &PortForwardRule{
		Spec: PortForwardRuleSpec{
			ExternalPort:   0, // Invalid to trigger validation
			Protocol:       "udp",
			Priority:       200,
			ConflictPolicy: "error",
			ServiceRef: &ServiceReference{
				Name: "new-service",
				Port: "http",
			},
		},
	}

	errs := newRule.ValidateUpdate(oldRule)

	// Should still validate new rule's spec and will fail due to invalid external port
	if len(errs) == 0 {
		t.Errorf("Expected validation errors for invalid rule but got none")
	}
}

func TestPortForwardRule_ValidateDelete(t *testing.T) {
	rule := &PortForwardRule{
		Spec: PortForwardRuleSpec{
			ExternalPort:   8080,
			Protocol:       "tcp",
			Priority:       100,
			ConflictPolicy: "warn",
			ServiceRef: &ServiceReference{
				Name: "test-service",
				Port: "http",
			},
		},
	}

	errs := rule.ValidateDelete()

	// Delete validation should always pass
	if len(errs) != 0 {
		t.Errorf("Expected no validation errors on delete but got: %v", errs)
	}
}

func TestIsValidDNSName(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"valid-name", true},
		{"valid-service-1", true},
		{"a", true},
		{"test-service-123", true},
		{"", false},
		{"Invalid-Name", false}, // uppercase
		{"invalid name", false}, // space
		{"invalid.name", false}, // dot
		{"invalid_name", false}, // underscore
		{"-invalid", false},     // starts with dash
		{"invalid-", false},     // ends with dash
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isValidDNSName(tt.input)
			if result != tt.expected {
				t.Errorf("isValidDNSName(%q) = %v; expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestValidateServiceRef(t *testing.T) {
	tests := []struct {
		name        string
		serviceRef  *ServiceReference
		expectError bool
		errorCount  int
	}{
		{
			name: "valid service ref",
			serviceRef: &ServiceReference{
				Name: "valid-service",
				Port: "http",
			},
			expectError: false,
		},
		{
			name: "valid service ref with namespace",
			serviceRef: &ServiceReference{
				Name:      "valid-service",
				Namespace: stringPtr("valid-namespace"),
				Port:      "http",
			},
			expectError: false,
		},
		{
			name: "invalid service name",
			serviceRef: &ServiceReference{
				Name: "Invalid-Name",
				Port: "http",
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "empty service name",
			serviceRef: &ServiceReference{
				Name: "",
				Port: "http",
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "empty port",
			serviceRef: &ServiceReference{
				Name: "valid-service",
				Port: "",
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "invalid namespace",
			serviceRef: &ServiceReference{
				Name:      "valid-service",
				Namespace: stringPtr("Invalid-Namespace"),
				Port:      "http",
			},
			expectError: true,
			errorCount:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &PortForwardRule{
				Spec: PortForwardRuleSpec{
					ExternalPort:   8080,
					Protocol:       "tcp",
					Priority:       100,
					ConflictPolicy: "warn",
					ServiceRef:     tt.serviceRef,
				},
			}

			errs := rule.validateServiceRef(field.NewPath("spec").Child("serviceRef"))

			if tt.expectError && len(errs) == 0 {
				t.Errorf("Expected validation errors but got none")
			} else if !tt.expectError && len(errs) > 0 {
				t.Errorf("Expected no validation errors but got: %v", errs)
			} else if tt.expectError && len(errs) != tt.errorCount {
				t.Errorf("Expected %d validation errors but got %d: %v", tt.errorCount, len(errs), errs)
			}
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		slice  []string
		item   string
		result bool
	}{
		{[]string{"tcp", "udp", "both"}, "tcp", true},
		{[]string{"tcp", "udp", "both"}, "udp", true},
		{[]string{"tcp", "udp", "both"}, "both", true},
		{[]string{"tcp", "udp", "both"}, "http", false},
		{[]string{}, "tcp", false},
		{[]string{"tcp"}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.item, func(t *testing.T) {
			result := contains(tt.slice, tt.item)
			if result != tt.result {
				t.Errorf("contains(%v, %q) = %v; expected %v", tt.slice, tt.item, result, tt.result)
			}
		})
	}
}

func TestParseServiceAnnotation(t *testing.T) {
	tests := []struct {
		annotation    string
		expectedPort  int
		expectedProto string
	}{
		{"80:tcp", 80, "tcp"},
		{"8080:udp", 8080, "udp"},
		{"443:tcp", 443, "tcp"},
		{"80", 80, "tcp"},     // default protocol
		{"8080", 8080, "tcp"}, // default protocol
		{"invalid", 0, ""},    // invalid format
	}

	for _, tt := range tests {
		t.Run(tt.annotation, func(t *testing.T) {
			port, proto := parseServiceAnnotation(tt.annotation)
			if port != tt.expectedPort {
				t.Errorf("parseServiceAnnotation(%q) port = %d; expected %d", tt.annotation, port, tt.expectedPort)
			}
			if proto != tt.expectedProto {
				t.Errorf("parseServiceAnnotation(%q) proto = %q; expected %q", tt.annotation, proto, tt.expectedProto)
			}
		})
	}
}

func TestValidateMutuallyExclusiveFields(t *testing.T) {
	tests := []struct {
		name        string
		rule        *PortForwardRule
		expectError bool
		errorCount  int
	}{
		{
			name: "service ref only",
			rule: &PortForwardRule{
				Spec: PortForwardRuleSpec{
					ExternalPort:   8080,
					Protocol:       "tcp",
					Priority:       100,
					ConflictPolicy: "warn",
					ServiceRef: &ServiceReference{
						Name: "test-service",
						Port: "http",
					},
				},
			},
			expectError: false,
		},
		{
			name: "destination IP with port",
			rule: &PortForwardRule{
				Spec: PortForwardRuleSpec{
					ExternalPort:    8080,
					Protocol:        "tcp",
					Priority:        100,
					ConflictPolicy:  "warn",
					DestinationIP:   stringPtr("192.168.1.100"),
					DestinationPort: intPtr(80),
				},
			},
			expectError: false,
		},
		{
			name: "both service ref and destination IP",
			rule: &PortForwardRule{
				Spec: PortForwardRuleSpec{
					ExternalPort:   8080,
					Protocol:       "tcp",
					Priority:       100,
					ConflictPolicy: "warn",
					ServiceRef: &ServiceReference{
						Name: "test-service",
						Port: "http",
					},
					DestinationIP: stringPtr("192.168.1.100"),
				},
			},
			expectError: true,
			errorCount:  2, // forbidden destinationIP + required destinationPort
		},
		{
			name: "neither service ref nor destination IP",
			rule: &PortForwardRule{
				Spec: PortForwardRuleSpec{
					ExternalPort:   8080,
					Protocol:       "tcp",
					Priority:       100,
					ConflictPolicy: "warn",
				},
			},
			expectError: true,
			errorCount:  1, // required serviceRef or destinationIP
		},
		{
			name: "destination IP without port",
			rule: &PortForwardRule{
				Spec: PortForwardRuleSpec{
					ExternalPort:   8080,
					Protocol:       "tcp",
					Priority:       100,
					ConflictPolicy: "warn",
					DestinationIP:  stringPtr("192.168.1.100"),
				},
			},
			expectError: true,
			errorCount:  1, // required destinationPort
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.rule.validateMutuallyExclusiveFields()

			if tt.expectError && len(errs) == 0 {
				t.Errorf("Expected validation errors but got none")
			} else if !tt.expectError && len(errs) > 0 {
				t.Errorf("Expected no validation errors but got: %v", errs)
			} else if tt.expectError && len(errs) != tt.errorCount {
				t.Errorf("Expected %d validation errors but got %d: %v", tt.errorCount, len(errs), errs)
			}
		})
	}
}

func TestPortForwardRule_ValidateCrossNamespacePortConflict(t *testing.T) {
	// This test requires a fake client to work properly
	// For now, we'll test that the method doesn't panic with nil input
	rule := &PortForwardRule{
		Spec: PortForwardRuleSpec{
			ExternalPort:   8080,
			Protocol:       "tcp",
			Priority:       100,
			ConflictPolicy: "warn",
		},
	}

	// Test with nil client - should not panic and return empty errors
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ValidateCrossNamespacePortConflict panicked with nil client: %v", r)
		}
	}()

	validationErrs := rule.ValidateCrossNamespacePortConflict(context.Background(), nil)

	if len(validationErrs) != 0 {
		t.Errorf("Expected no errors when client is nil, got: %v", validationErrs)
	}
}

func TestPortForwardRule_ValidateServiceExists(t *testing.T) {
	rule := &PortForwardRule{
		Spec: PortForwardRuleSpec{
			ServiceRef: &ServiceReference{
				Name: "test-service",
				Port: "http",
			},
		},
	}

	// Test with nil client - should not panic and return empty errors
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ValidateServiceExists panicked with nil client: %v", r)
		}
	}()

	validationErrs := rule.ValidateServiceExists(context.Background(), nil)

	if len(validationErrs) != 0 {
		t.Errorf("Expected no errors when client is nil, got: %v", validationErrs)
	}

	// Test with nil serviceRef
	rule.Spec.ServiceRef = nil
	validationErrs = rule.ValidateServiceExists(context.Background(), nil)

	// Should return empty error list when serviceRef is nil
	if len(validationErrs) != 0 {
		t.Errorf("Expected no errors when serviceRef is nil, got: %v", validationErrs)
	}
}

// Helper functions for tests
func stringPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}
