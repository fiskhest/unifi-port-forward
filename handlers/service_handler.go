package handlers

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"kube-router-port-forward/routers"

	"github.com/filipowm/go-unifi/unifi"
	v1 "k8s.io/api/core/v1"
)

// ServiceHandler defines the interface for handling Kubernetes service events
type ServiceHandler interface {
	OnAdd(obj any)
	OnUpdate(oldObj, newObj any)
	OnDelete(obj any)
}

// serviceHandler implements ServiceHandler with shared state and dependencies
type serviceHandler struct {
	router           routers.Router
	client           unifi.Client
	site             string
	filterAnnotation string
	ctx              context.Context

	// Debounce map to prevent rapid-fire updates
	updateDebounce map[string]time.Time
	debounceMutex  sync.RWMutex
	debounceDelay  time.Duration
}

// NewServiceHandler creates a new ServiceHandler with dependencies
func NewServiceHandler(router routers.Router, client unifi.Client, site, filterAnnotation string) ServiceHandler {
	return &serviceHandler{
		router:           router,
		client:           client,
		site:             site,
		filterAnnotation: filterAnnotation,
		ctx:              context.Background(),
		updateDebounce:   make(map[string]time.Time),
		debounceMutex:    sync.RWMutex{},
		debounceDelay:    5 * time.Second,
	}
}

// OnAdd handles service addition events
func (h *serviceHandler) OnAdd(obj any) {
	service := obj.(*v1.Service)
	h.handleAdd(service)
}

// OnUpdate handles service update events
func (h *serviceHandler) OnUpdate(oldObj, newObj any) {
	oldService := oldObj.(*v1.Service)
	newService := newObj.(*v1.Service)
	h.handleUpdate(oldService, newService)
}

// OnDelete handles service deletion events
func (h *serviceHandler) OnDelete(obj any) {
	service := obj.(*v1.Service)
	h.handleDelete(service)
}

// getServiceKey creates a unique key for a service
func (h *serviceHandler) getServiceKey(service *v1.Service) string {
	return fmt.Sprintf("%s/%s", service.Namespace, service.Name)
}

// shouldSkipUpdate checks if an update should be skipped based on debounce logic
func (h *serviceHandler) shouldSkipUpdate(serviceKey string, oldLBIP, newLBIP string) bool {
	// Skip updates if both IPs are node IPs (transient)
	if oldLBIP != "" && newLBIP != "" && isNodeIP(oldLBIP) && isNodeIP(newLBIP) {
		fmt.Printf("DEBUG: Skipping update - both IPs are node IPs (transient)\n")
		return true
	}

	// Debounce rapid updates for actual IP changes
	h.debounceMutex.RLock()
	lastUpdate, exists := h.updateDebounce[serviceKey]
	h.debounceMutex.RUnlock()

	if exists && time.Since(lastUpdate) < h.debounceDelay && oldLBIP != newLBIP {
		fmt.Printf("DEBUG: Debouncing IP change update for %s (last update: %v ago)\n", serviceKey, time.Since(lastUpdate))
		return true
	}

	return false
}

// updateDebounceTimestamp updates the debounce timestamp for a service
func (h *serviceHandler) updateDebounceTimestamp(serviceKey string) {
	h.debounceMutex.Lock()
	h.updateDebounce[serviceKey] = time.Now()
	h.debounceMutex.Unlock()
}

// logError logs errors with context
func (h *serviceHandler) logError(message string, err error, service *v1.Service) {
	if service != nil {
		log.Printf("%s for service %s/%s: %v\n", message, service.Namespace, service.Name, err)
	} else {
		log.Printf("%s: %v\n", message, err)
	}
}
