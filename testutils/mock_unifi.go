package testutils

import (
	"context"
	"fmt"
	"sync"

	"github.com/filipowm/go-unifi/unifi"
)

// MockUniFiClient implements a mock UniFi client for testing
type MockUniFiClient struct {
	PortForwards []unifi.PortForward
	mu           sync.RWMutex
	LoginCalled  bool
	version      string
}

// NewMockUniFiClient creates a new mock UniFi client
func NewMockUniFiClient() *MockUniFiClient {
	return &MockUniFiClient{
		PortForwards: make([]unifi.PortForward, 0),
		version:      "8.0.24",
	}
}

// Login simulates login
func (m *MockUniFiClient) Login() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LoginCalled = true
	return nil
}

// Version returns the mock controller version
func (m *MockUniFiClient) Version() string {
	return m.version
}

// ListPortForward returns the list of port forward rules
func (m *MockUniFiClient) ListPortForward(ctx context.Context, siteID string) ([]unifi.PortForward, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]unifi.PortForward, len(m.PortForwards))
	copy(result, m.PortForwards)
	return result, nil
}

// CreatePortForward creates a new port forward rule
func (m *MockUniFiClient) CreatePortForward(ctx context.Context, siteID string, pf *unifi.PortForward) (*unifi.PortForward, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if port already exists
	for _, existing := range m.PortForwards {
		if existing.FwdPort == pf.FwdPort {
			return nil, fmt.Errorf("port %s already exists", pf.FwdPort)
		}
	}

	// Create new rule with generated ID
	newPF := *pf
	newPF.ID = fmt.Sprintf("mock-id-%d", len(m.PortForwards)+1)
	newPF.SiteID = siteID

	m.PortForwards = append(m.PortForwards, newPF)
	return &newPF, nil
}

// DeletePortForward deletes a port forward rule
func (m *MockUniFiClient) DeletePortForward(ctx context.Context, siteID, ruleID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, pf := range m.PortForwards {
		if pf.ID == ruleID {
			m.PortForwards = append(m.PortForwards[:i], m.PortForwards[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("port forward rule with ID %s not found", ruleID)
}

// AddPortForward adds a port forward rule for testing
func (m *MockUniFiClient) AddPortForward(pf unifi.PortForward) {
	m.mu.Lock()
	defer m.mu.Unlock()

	pf.ID = fmt.Sprintf("mock-id-%d", len(m.PortForwards)+1)
	m.PortForwards = append(m.PortForwards, pf)
}

// ClearPortForwards clears all port forward rules
func (m *MockUniFiClient) ClearPortForwards() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.PortForwards = make([]unifi.PortForward, 0)
}

// GetPortForwardCount returns the number of port forward rules
func (m *MockUniFiClient) GetPortForwardCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.PortForwards)
}

// HasPortForward checks if a port forward rule exists
func (m *MockUniFiClient) HasPortForward(port string, dstIP string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, pf := range m.PortForwards {
		if pf.FwdPort == port && pf.Fwd == dstIP {
			return true
		}
	}
	return false
}
