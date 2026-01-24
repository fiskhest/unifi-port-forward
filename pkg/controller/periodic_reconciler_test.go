package controller

import (
	"testing"
	"time"

	"unifi-port-forward/pkg/config"
	"unifi-port-forward/pkg/helpers"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
)

func TestNewPeriodicReconciler(t *testing.T) {
	// Create test dependencies
	scheme := runtime.NewScheme()
	config := &config.Config{}

	// Initialize config with default values
	config.Load()

	mockRecorder := record.NewFakeRecorder(10)

	// Create a minimal event publisher for testing
	eventPublisher := NewEventPublisher(nil, mockRecorder, scheme)

	// Test creation
	reconciler := NewPeriodicReconciler(nil, scheme, nil, config, eventPublisher, mockRecorder)

	// Basic assertions
	if reconciler == nil {
		t.Fatal("Expected reconciler to not be nil")
		return
	}

	if reconciler.interval != 15*time.Minute {
		t.Errorf("Expected interval to be 15 minutes, got %v", reconciler.interval)
	}
	if reconciler.stopCh == nil {
		t.Error("Expected stopCh to be initialized")
	}
	if reconciler.semaphore == nil {
		t.Error("Expected semaphore to be initialized")
	}
}

func TestPeriodicReconciler_shouldManageService(t *testing.T) {
	// Test setup
	scheme := runtime.NewScheme()
	config := &config.Config{}
	mockRecorder := record.NewFakeRecorder(10)
	eventPublisher := NewEventPublisher(nil, mockRecorder, scheme)

	reconciler := NewPeriodicReconciler(nil, scheme, nil, config, eventPublisher, mockRecorder)

	tests := []struct {
		name     string
		service  *corev1.Service
		expected bool
	}{
		{
			name:     "service without annotations should not be managed",
			service:  &corev1.Service{},
			expected: false,
		},
		{
			name: "service with correct annotation and LoadBalancer IP should be managed",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"unifi-port-forward.fiskhe.st/mapping": "8080:80",
					},
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP: "192.168.1.100",
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "service with annotation but no LoadBalancer IP should not be managed",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"unifi-port-forward.fiskhe.st/mapping": "8080:80",
					},
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.shouldManageService(tt.service)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestParseIntField(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"80", 80},
		{"443", 443},
		{"", 0},
		{"invalid", 0},
		{"-1", 0}, // ParseIntField returns 0 for invalid numbers
		{"1000", 1000},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := helpers.ParseIntField(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestPeriodicReconciler_StartStop(t *testing.T) {
	// Test setup
	scheme := runtime.NewScheme()
	config := &config.Config{}
	mockRecorder := record.NewFakeRecorder(10)
	eventPublisher := NewEventPublisher(nil, mockRecorder, scheme)

	reconciler := NewPeriodicReconciler(nil, scheme, nil, config, eventPublisher, mockRecorder)

	// Test that Stop() doesn't panic and clean up resources
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Stop() panicked: %v", r)
		}
	}()

	// Call Stop() should not panic
	err := reconciler.Stop()
	if err != nil {
		t.Errorf("Expected no error on Stop(), got %v", err)
	}

	// Note: We can't easily test the stopCh closure without race conditions
	// The important thing is that Stop() doesn't panic
}

func TestPeriodicReconciler_isSafeUpdate(t *testing.T) {
	// Test setup
	scheme := runtime.NewScheme()
	config := &config.Config{}
	mockRecorder := record.NewFakeRecorder(10)
	eventPublisher := NewEventPublisher(nil, mockRecorder, scheme)

	_ = NewPeriodicReconciler(nil, scheme, nil, config, eventPublisher, mockRecorder)

	tests := []struct {
		mismatchType string
		expected     bool
	}{
		{"name", true},      // Safe update
		{"ip", true},        // Safe update
		{"enabled", true},   // Safe update
		{"ownership", true}, // Safe update
		{"fwdport", false},  // Risky - needs delete+create
		{"port", false},     // Risky - needs delete+create
		{"protocol", false}, // Risky - needs delete+create
		{"unknown", false},  // Unknown - treat as risky
	}

	for _, tt := range tests {
		t.Run(tt.mismatchType, func(t *testing.T) {
			result := isSafeUpdate(tt.mismatchType)
			if result != tt.expected {
				t.Errorf("Expected %v for mismatch type '%s', got %v", tt.expected, tt.mismatchType, result)
			}
		})
	}
}
