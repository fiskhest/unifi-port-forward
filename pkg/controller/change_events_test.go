package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPublishPortForwardTakenOwnershipEvent(t *testing.T) {
	// Create event publisher with nil recorder (will just log)
	eventPublisher := NewEventPublisher(nil, nil, nil)

	// Create test service
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
	}

	changeContext := &ChangeContext{
		ServiceKey:       "default/test-service",
		ServiceNamespace: "default",
		ServiceName:      "test-service",
	}

	// Call method - should not panic and should handle nil recorder gracefully
	eventPublisher.PublishPortForwardTakenOwnershipEvent(
		context.Background(),
		service,
		changeContext,
		"qbittorrent",              // oldRuleName
		"default/test-service:tcp", // newRuleName
		6881,                       // externalPort
		"tcp",                      // protocol
	)

	// Test passes if no panic occurs
	t.Log("Ownership event published successfully with nil recorder")
}
