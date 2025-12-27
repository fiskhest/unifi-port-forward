package testutils

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/filipowm/go-unifi/unifi"
	"kube-router-port-forward/pkg/routers"
)

// MockRouter implements routers.Router interface for testing
type MockRouter struct {
	mu                sync.RWMutex
	PortForwards      []unifi.PortForward
	shouldFail        bool
	failCount         int
	callCount         map[string]int
	simulatedFailures map[string]bool
}

// NewMockRouter creates a new mock router
func NewMockRouter() *MockRouter {
	return &MockRouter{
		PortForwards:      make([]unifi.PortForward, 0),
		shouldFail:        false,
		failCount:         0,
		callCount:         make(map[string]int),
		simulatedFailures: make(map[string]bool),
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

	if r.shouldFail || r.ShouldOperationFail("AddPort") {
		r.failCount++
		return fmt.Errorf("simulated AddPort failure")
	}

	// Convert to unifi.PortForward format for internal storage
	pf := unifi.PortForward{
		ID:            fmt.Sprintf("mock-id-%d", len(r.PortForwards)+1),
		Name:          config.Name,
		DestinationIP: "any",
		DstPort:       strconv.Itoa(config.DstPort),
		Fwd:           config.DstIP,
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

	if r.shouldFail || r.ShouldOperationFail("RemovePort") {
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

	if r.shouldFail || r.ShouldOperationFail("UpdatePort") {
		r.failCount++
		return fmt.Errorf("simulated UpdatePort failure")
	}

	portStr := strconv.Itoa(port)
	for i, pf := range r.PortForwards {
		if pf.DstPort == portStr {
			// Update existing rule (match by port only, since we might be changing IP)
			r.PortForwards[i] = unifi.PortForward{
				ID:            pf.ID,
				Name:          config.Name,
				DestinationIP: "any",
				DstPort:       strconv.Itoa(config.DstPort),
				Fwd:           config.DstIP,
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

	if r.shouldFail || r.ShouldOperationFail("ListAllPortForwards") {
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

// GetPortForwardRuleByName finds a port forward rule by its name
func (r *MockRouter) GetPortForwardRuleByName(name string) *unifi.PortForward {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.callCount["GetPortForwardRuleByName"]++

	for _, pf := range r.PortForwards {
		if pf.Name == name {
			return &pf
		}
	}
	return nil
}

// ClearPortForwardsByPrefix removes all port forward rules whose names start with the given prefix
func (r *MockRouter) ClearPortForwardsByPrefix(prefix string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callCount["ClearPortForwardsByPrefix"]++

	originalCount := len(r.PortForwards)
	var filtered []unifi.PortForward

	for _, pf := range r.PortForwards {
		if !strings.HasPrefix(pf.Name, prefix) {
			filtered = append(filtered, pf)
		}
	}

	r.PortForwards = filtered
	removedCount := originalCount - len(r.PortForwards)
	return removedCount
}

// GetPortForwardNames returns a list of all port forward rule names
func (r *MockRouter) GetPortForwardNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.callCount["GetPortForwardNames"]++

	names := make([]string, len(r.PortForwards))
	for i, pf := range r.PortForwards {
		names[i] = pf.Name
	}
	return names
}

// ClearAllPortForwards removes all port forward rules
func (r *MockRouter) ClearAllPortForwards() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callCount["ClearAllPortForwards"]++
	r.PortForwards = make([]unifi.PortForward, 0)
}

// SetSimulatedFailure controls failure for specific operations
func (r *MockRouter) SetSimulatedFailure(operation string, shouldFail bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callCount["SetSimulatedFailure"]++
	if r.simulatedFailures == nil {
		r.simulatedFailures = make(map[string]bool)
	}
	r.simulatedFailures[operation] = shouldFail
}

// ShouldOperationFail checks if an operation should fail
func (r *MockRouter) ShouldOperationFail(operation string) bool {
	r.callCount["ShouldOperationFail"]++

	if r.simulatedFailures == nil {
		return false
	}
	return r.simulatedFailures[operation]
}

// GetOperationCounts returns a copy of operation counts
func (r *MockRouter) GetOperationCounts() map[string]int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.callCount["GetOperationCounts"]++

	result := make(map[string]int)
	for k, v := range r.callCount {
		result[k] = v
	}
	return result
}

// ResetOperationCounts resets all operation counters
func (r *MockRouter) ResetOperationCounts() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callCount = make(map[string]int)
	r.simulatedFailures = make(map[string]bool)
}
