package controller

import (
	"context"
	"testing"
	"time"

	"github.com/filipowm/go-unifi/unifi"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"

	"unifi-port-forward/pkg/api/v1alpha1"
	"unifi-port-forward/pkg/config"
	"unifi-port-forward/testutils"
)

func TestPortForwardRuleDeletion_SimpleTest(t *testing.T) {
	// Create test environment
	mockRouter := testutils.NewMockRouter()

	// Clear any existing state to ensure test isolation
	mockRouter.ClearAllPortForwards()
	mockRouter.ResetOperationCounts()

	// Create scheme for controller runtime
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add core v1 to scheme: %v", err)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add v1alpha1 to scheme: %v", err)
	}

	// Create a simple fake client that supports PortForwardRule
	fakeClient := testutils.NewFakeKubernetesClient(t, scheme)

	// Create PortForwardRule controller
	controller := &PortForwardRuleReconciler{
		Client:   fakeClient,
		Router:   mockRouter,
		Scheme:   scheme,
		Config:   &config.Config{Debug: true},
		Recorder: &record.FakeRecorder{},
	}

	// Create a PortForwardRule with router rule ID and finalizer
	rule := &v1alpha1.PortForwardRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-rule",
			Namespace:         "default",
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
			Finalizers:        []string{config.FinalizerLabel},
		},
		Spec: v1alpha1.PortForwardRuleSpec{
			ExternalPort:    8080,
			Protocol:        "tcp",
			Interface:       "wan",
			Enabled:         true,
			DestinationIP:   stringPtr("192.168.1.100"),
			DestinationPort: intPtr(80),
		},
		Status: v1alpha1.PortForwardRuleStatus{
			RouterRuleID: "default/test-rule:8080",
		},
	}

	// Setup mock router to return a matching rule
	mockPortForward := &unifi.PortForward{
		ID:      "abc123",
		Name:    "default/test-rule:8080",
		DstPort: "8080",
		Proto:   "tcp",
		Fwd:     "192.168.1.100",
		Enabled: true,
	}
	mockRouter.SetMockPortForward(mockPortForward)

	// Instead of creating in fake client, test the deletion method directly
	// by testing the scenario where rule exists and needs deletion
	ctx := context.Background()

	// Test the deletion handling using existing rule (simulating rule exists in K8s)
	// We'll test the new deleteRouterRuleByID method directly
	err := controller.deleteRouterRuleByID(ctx, rule)
	if err != nil {
		t.Fatalf("Expected no error from deleteRouterRuleByID, got: %v", err)
	}

	// Verify DeletePortForwardByID was called on the router
	if !mockRouter.DeletePortForwardByIDCalled {
		t.Fatal("❌ DeletePortForwardByID was not called on router")
	}

	if mockRouter.LastDeletedRuleID != "abc123" {
		t.Fatalf("❌ Expected DeletePortForwardByID to be called with 'abc123', got: %s", mockRouter.LastDeletedRuleID)
	}

	// Test case where rule doesn't exist on router
	mockRouter.ResetOperationCounts()
	mockRouter.SetMockPortForward(nil) // No matching rule

	t.Logf("Before deletion - DeletePortForwardByIDCalled: %v", mockRouter.DeletePortForwardByIDCalled)

	err = controller.deleteRouterRuleByID(ctx, rule)
	if err != nil {
		t.Fatalf("Expected no error when router rule not found, got: %v", err)
	}

	t.Logf("After deletion - DeletePortForwardByIDCalled: %v", mockRouter.DeletePortForwardByIDCalled)

	// Verify DeletePortForwardByID was NOT called since rule doesn't exist
	if mockRouter.DeletePortForwardByIDCalled {
		t.Fatal("❌ DeletePortForwardByID should not be called when router rule doesn't exist")
	}

	t.Log("✅ PortForwardRule deletion test passed!")
}

// Helper functions for creating pointers to primitives
func stringPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}
