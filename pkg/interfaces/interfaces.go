package interfaces

import (
	"context"
	"sync"

	"github.com/filipowm/go-unifi/unifi"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"unifi-port-forward/pkg/routers"
)

// Router defines the interface for router operations
type Router interface {
	ListAllPortForwards(ctx context.Context) ([]*unifi.PortForward, error)
	AddPort(ctx context.Context, config routers.PortConfig) error
	UpdatePort(ctx context.Context, externalPort int, config routers.PortConfig) error
	RemovePort(ctx context.Context, config routers.PortConfig) error
	DeletePortForwardByID(ctx context.Context, ruleID string) error
	CheckPort(ctx context.Context, port int, protocol string) (*unifi.PortForward, bool, error)
}

// PortTracker defines the interface for port conflict tracking
type PortTracker interface {
	CheckConflict(externalPort int, serviceKey string) error
	MarkUsed(externalPort int, serviceKey string)
	UnmarkUsed(externalPort int)
	UnmarkForService(serviceKey string)
	Clear()
	GetUsedPorts() map[int]string
	GetMutex() *sync.RWMutex
}

// KubernetesClient defines a subset of controller-runtime client interface
// used by the controller for testing and implementation
type KubernetesClient interface {
	Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error
	Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error
	Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error
	Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error
}

// Reconciler defines the interface for reconciliation logic
type Reconciler interface {
	Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error)
}

// EventRecorder defines the interface for recording Kubernetes events
type EventRecorder interface {
	Event(object runtime.Object, eventtype, reason, message string)
	Eventf(object runtime.Object, eventtype, reason, message string, args ...interface{})
}
