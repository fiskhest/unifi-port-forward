package controller

import (
	"testing"
)

func TestParseGatewayMappingAnnotation(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string // "PROTOCOL:portspec" per mapping, in order
		wantErr bool
	}{
		{
			name:  "single mapping",
			input: "TCP:80",
			want:  []string{"TCP:80"},
		},
		{
			name:  "several mappings",
			input: "TCP:80,UDP:53",
			want:  []string{"TCP:80", "UDP:53"},
		},
		{
			name:  "range",
			input: "UDP:50000-50100",
			want:  []string{"UDP:50000-50100"},
		},
		{
			name:  "repeated protocol yields one mapping per entry",
			input: "TCP:80,TCP:443",
			want:  []string{"TCP:80", "TCP:443"},
		},
		{
			name:  "lowercase protocol is normalized",
			input: "tcp:8000-8100",
			want:  []string{"TCP:8000-8100"},
		},
		{
			name:  "whitespace tolerated",
			input: " TCP : 80 , UDP : 53 ",
			want:  []string{"TCP:80", "UDP:53"},
		},

		{name: "empty", input: "", wantErr: true},
		{name: "missing port", input: "TCP", wantErr: true},
		{name: "port out of range", input: "TCP:70000", wantErr: true},
		{name: "inverted range", input: "TCP:8100-8000", wantErr: true},
		{name: "not a port", input: "TCP:http", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mappings, err := parseGatewayMappingAnnotation(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseGatewayMappingAnnotation(%q) = %+v, want error", tt.input, mappings)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseGatewayMappingAnnotation(%q) returned error: %v", tt.input, err)
			}

			if len(mappings) != len(tt.want) {
				t.Fatalf("got %d mappings, want %d: %+v", len(mappings), len(tt.want), mappings)
			}
			for i, mapping := range mappings {
				got := mapping.Protocol + ":" + mapping.Ports.String()
				if got != tt.want[i] {
					t.Errorf("mapping %d = %q, want %q", i, got, tt.want[i])
				}
			}
		})
	}
}
