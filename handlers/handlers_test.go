package handlers

import (
	"testing"
)

func TestNewServiceHandler(t *testing.T) {
	// Test that NewServiceHandler returns a non-nil handler
	handler := NewServiceHandler(nil, nil, "test", "test")
	if handler == nil {
		t.Error("NewServiceHandler returned nil")
	}

	// Test that it implements the interface
	var _ ServiceHandler = handler
}
