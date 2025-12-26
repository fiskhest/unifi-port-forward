package testutils

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/filipowm/go-unifi/unifi"
	"kube-router-port-forward/routers"
)

// MockRouter implements routers.Router interface for testing
type MockRouter struct {
	mu           sync.RWMutex
	PortForwards []unifi.PortForward
	shouldFail   bool
	failCount    int
	callCount    map[string]int
}

// NewMockRouter creates a new mock router
func NewMockRouter() *MockRouter {
	return &MockRouter{
		PortForwards: make([]unifi.PortForward, 0),
		shouldFail:   false,
		failCount:    0,
		callCount:    make(map[string]int),
	}
}

// SetFailure controls whether the mock should fail
func (r *MockRouter) SetFailure(shouldFail bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.shouldFail = shouldFail
}

// GetCallCount returns how many times a method was called
func (r *MockRouter) GetCallCount(method string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.callCount[method]
}

// GetFailCount returns how many failures occurred
func (r *MockRouter) GetFailCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.failCount
}

// ResetCallCounts resets all call counters
func (r *MockRouter) ResetCallCounts() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callCount = make(map[string]int)
	r.failCount = 0
}

// AddPort implements routers.Router.AddPort
func (r *MockRouter) AddPort(ctx context.Context, config routers.PortConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callCount["AddPort"]++

	if r.shouldFail {
		r.failCount++
		return fmt.Errorf("simulated AddPort failure")
	}

	// Convert to unifi.PortForward format for internal storage
	pf := unifi.PortForward{
		ID:            fmt.Sprintf("mock-id-%d", len(r.PortForwards)+1),
		Name:          config.Name,
		DestinationIP: config.DstIP,
		DstPort:       strconv.Itoa(config.DstPort),
		FwdPort:       strconv.Itoa(config.FwdPort),
		Proto:         config.Protocol,
		Enabled:       config.Enabled,
		PfwdInterface: config.Interface,
		Src:           config.SrcIP,
	}

	// Check if port already exists
	for _, existing := range r.PortForwards {
		if existing.DstPort == strconv.Itoa(config.DstPort) && existing.DestinationIP == config.DstIP {
			return fmt.Errorf("port %d to %s already exists", config.DstPort, config.DstIP)
		}
	}

	// Add new rule
	r.PortForwards = append(r.PortForwards, pf)
	return nil
}

// CheckPort implements routers.Router.CheckPort
func (r *MockRouter) CheckPort(ctx context.Context, port int) (*unifi.PortForward, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.callCount["CheckPort"]++

	if r.shouldFail {
		r.failCount++
		return nil, false, fmt.Errorf("simulated CheckPort failure")
	}

	portStr := strconv.Itoa(port)

	for _, pf := range r.PortForwards {
		if pf.DstPort == portStr {
			return &pf, true, nil
		}
	}

	return nil, false, nil
}

// RemovePort implements routers.Router.RemovePort
func (r *MockRouter) RemovePort(ctx context.Context, config routers.PortConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callCount["RemovePort"]++

	if r.shouldFail {
		r.failCount++
		return fmt.Errorf("simulated RemovePort failure")
	}

	portStr := strconv.Itoa(config.DstPort)
	for i, pf := range r.PortForwards {
		if pf.DstPort == portStr && pf.DestinationIP == config.DstIP {
			// Remove the matching rule
			r.PortForwards = append(r.PortForwards[:i], r.PortForwards[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("port %d to %s not found", config.DstPort, config.DstIP)
}

// UpdatePort implements routers.Router.UpdatePort
func (r *MockRouter) UpdatePort(ctx context.Context, port int, config routers.PortConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callCount["UpdatePort"]++

	if r.shouldFail {
		r.failCount++
		return fmt.Errorf("simulated UpdatePort failure")
	}

	portStr := strconv.Itoa(port)
	for i, pf := range r.PortForwards {
		if pf.DstPort == portStr && pf.DestinationIP == config.DstIP {
			// Update existing rule
			r.PortForwards[i] = unifi.PortForward{
				ID:            pf.ID,
				Name:          config.Name,
				DestinationIP: config.DstIP,
				DstPort:       strconv.Itoa(config.DstPort),
				FwdPort:       strconv.Itoa(config.FwdPort),
				Proto:         config.Protocol,
				Enabled:       config.Enabled,
				PfwdInterface: config.Interface,
				Src:           config.SrcIP,
			}
			return nil
		}
	}

	return fmt.Errorf("port %d to %s not found", port, config.DstIP)
}

// ListAllPortForwards implements routers.Router.ListAllPortForwards
func (r *MockRouter) ListAllPortForwards(ctx context.Context) ([]*unifi.PortForward, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.callCount["ListAllPortForwards"]++

	if r.shouldFail {
		r.failCount++
		return nil, fmt.Errorf("simulated ListAllPortForwards failure")
	}

	result := make([]*unifi.PortForward, len(r.PortForwards))
	for i := range r.PortForwards {
		result[i] = &r.PortForwards[i]
	}
	return result, nil
}

// ResetCounters resets call and failure counters
func (r *MockRouter) ResetCounters() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callCount = make(map[string]int)
	r.failCount = 0
}

// ClearPortForwards clears all port forward rules
func (r *MockRouter) ClearPortForwards() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.PortForwards = make([]unifi.PortForward, 0)
}

// GetPortForwardCount returns the number of port forward rules
func (r *MockRouter) GetPortForwardCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.PortForwards)
}

// HasPortForward checks if a port forward rule exists
func (r *MockRouter) HasPortForward(port, dstIP string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.callCount["HasPortForward"]++

	for _, pf := range r.PortForwards {
		if pf.DstPort == port && pf.DestinationIP == dstIP {
			return true
		}
	}
	return false
}

// AddPortForwardRule adds a port forward rule directly (for testing)
func (r *MockRouter) AddPortForwardRule(pf unifi.PortForward) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.PortForwards = append(r.PortForwards, pf)
}

// GetPortForwardRules returns a copy of all port forward rules
func (r *MockRouter) GetPortForwardRules() []unifi.PortForward {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]unifi.PortForward, len(r.PortForwards))
	copy(result, r.PortForwards)
	return result
}
