package v1alpha1

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestExternalPortSpec(t *testing.T) {
	tests := []struct {
		name    string
		value   intstr.IntOrString
		want    string
		wantErr bool
	}{
		{name: "bare number", value: intstr.FromInt32(8080), want: "8080"},
		{name: "numeric string", value: intstr.FromString("8080"), want: "8080"},
		{name: "range", value: intstr.FromString("8000-8100"), want: "8000-8100"},
		{name: "list", value: intstr.FromString("80,443,8000-8100"), want: "80,443,8000-8100"},
		{name: "normalized list", value: intstr.FromString("443,80"), want: "80,443"},

		{name: "zero", value: intstr.FromInt32(0), wantErr: true},
		{name: "above maximum", value: intstr.FromInt32(65536), wantErr: true},
		{name: "empty string", value: intstr.FromString(""), wantErr: true},
		{name: "not a port", value: intstr.FromString("http"), wantErr: true},
		{name: "inverted range", value: intstr.FromString("8100-8000"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := PortForwardRuleSpec{ExternalPort: tt.value}
			got, err := spec.ExternalPortSpec()

			if tt.wantErr {
				if err == nil {
					t.Fatalf("ExternalPortSpec() = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExternalPortSpec() returned error: %v", err)
			}
			if got.String() != tt.want {
				t.Errorf("ExternalPortSpec() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDestinationPortSpecUnsetIsEmpty(t *testing.T) {
	spec := PortForwardRuleSpec{ExternalPort: intstr.FromInt32(8080)}

	got, err := spec.DestinationPortSpec()
	if err != nil {
		t.Fatalf("DestinationPortSpec() returned error: %v", err)
	}
	if !got.IsEmpty() {
		t.Errorf("DestinationPortSpec() = %q, want empty", got)
	}
}

func TestValidateCreateWithPortRanges(t *testing.T) {
	tests := []struct {
		name            string
		externalPort    intstr.IntOrString
		destinationPort *intstr.IntOrString
		expectError     bool
	}{
		{
			name:            "range forwarded to a matching range",
			externalPort:    intstr.FromString("8000-8100"),
			destinationPort: strPortPtr("9000-9100"),
		},
		{
			name:            "range collapsed onto a single port",
			externalPort:    intstr.FromString("8000-8100"),
			destinationPort: portPtr(9000),
		},
		{
			name:            "list collapsed onto a single port",
			externalPort:    intstr.FromString("80,443"),
			destinationPort: portPtr(8443),
		},
		{
			name:            "counts must line up",
			externalPort:    intstr.FromString("8000-8100"),
			destinationPort: strPortPtr("9000-9050"),
			expectError:     true,
		},
		{
			name:            "external range with a smaller destination list",
			externalPort:    intstr.FromString("8000-8002"),
			destinationPort: strPortPtr("9000,9001"),
			expectError:     true,
		},
		{
			name:            "unparseable external port",
			externalPort:    intstr.FromString("8000-"),
			destinationPort: portPtr(9000),
			expectError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &PortForwardRule{
				Spec: PortForwardRuleSpec{
					ExternalPort:    tt.externalPort,
					Protocol:        "tcp",
					Priority:        100,
					ConflictPolicy:  "warn",
					DestinationIP:   stringPtr("192.168.1.100"),
					DestinationPort: tt.destinationPort,
				},
			}

			errs := rule.ValidateCreate()
			hasError := len(errs) > 0

			if hasError != tt.expectError {
				t.Errorf("ValidateCreate() errors = %v, expected error: %v", errs, tt.expectError)
			}
			if tt.expectError && hasError && errs[0].Type != field.ErrorTypeInvalid {
				t.Errorf("expected an Invalid error, got %s", errs[0].Type)
			}
		})
	}
}

func TestValidateCrossNamespaceConflictDetectsOverlap(t *testing.T) {
	existing := PortForwardRule{
		Spec: PortForwardRuleSpec{ExternalPort: intstr.FromString("8000-8100"), Protocol: "tcp"},
	}
	existing.Namespace = "default"
	existing.Name = "existing"

	overlapping := &PortForwardRule{
		Spec: PortForwardRuleSpec{ExternalPort: intstr.FromInt32(8050), Protocol: "tcp"},
	}
	overlapping.Namespace = "default"
	overlapping.Name = "overlapping"

	existingPorts, err := existing.Spec.ExternalPortSpec()
	if err != nil {
		t.Fatalf("existing rule spec did not parse: %v", err)
	}
	overlappingPorts, err := overlapping.Spec.ExternalPortSpec()
	if err != nil {
		t.Fatalf("overlapping rule spec did not parse: %v", err)
	}

	if !existingPorts.Overlaps(overlappingPorts) {
		t.Errorf("%s should overlap %s", existingPorts, overlappingPorts)
	}
}

func strPortPtr(spec string) *intstr.IntOrString {
	value := intstr.FromString(spec)
	return &value
}
