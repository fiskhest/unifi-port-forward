package destination

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// nodeClient serves a fixed node list. Only List is implemented - the resolver
// uses nothing else, and a stub keeps the controller-runtime fake client (and
// the dependency it drags in) out of the build.
type nodeClient struct {
	client.Client
	nodes   []*corev1.Node
	listErr error
}

func (c *nodeClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if c.listErr != nil {
		return c.listErr
	}
	nodeList, ok := list.(*corev1.NodeList)
	if !ok {
		return fmt.Errorf("nodeClient only lists nodes, got %T", list)
	}
	nodeList.Items = nil
	for _, n := range c.nodes {
		nodeList.Items = append(nodeList.Items, *n)
	}
	return nil
}

func newFakeClient(nodes ...*corev1.Node) client.Client {
	return &nodeClient{nodes: nodes}
}

func node(name, internalIP string, ready bool, labels map[string]string) *corev1.Node {
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}

	n := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: status}},
		},
	}
	if internalIP != "" {
		n.Status.Addresses = []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: internalIP}}
	}
	return n
}

func service(name string, serviceType corev1.ServiceType) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       corev1.ServiceSpec{Type: serviceType},
	}
}

func TestForServiceLoadBalancer(t *testing.T) {
	svc := service("web", corev1.ServiceTypeLoadBalancer)
	svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "192.168.1.50"}}

	resolver := &Resolver{}
	dest, err := resolver.ForService(context.Background(), svc)
	if err != nil {
		t.Fatalf("ForService returned error: %v", err)
	}
	if dest.IP != "192.168.1.50" {
		t.Errorf("IP = %q, want 192.168.1.50", dest.IP)
	}
	if dest.Source != SourceLoadBalancer {
		t.Errorf("Source = %q, want %q", dest.Source, SourceLoadBalancer)
	}
}

func TestForServiceLoadBalancerWithoutIngress(t *testing.T) {
	resolver := &Resolver{}
	if _, err := resolver.ForService(context.Background(), service("web", corev1.ServiceTypeLoadBalancer)); err == nil {
		t.Error("expected an error for a LoadBalancer with no ingress IP")
	}
}

func TestForServiceAnnotationWins(t *testing.T) {
	svc := service("web", corev1.ServiceTypeLoadBalancer)
	svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "192.168.1.50"}}
	svc.Annotations = map[string]string{IPAnnotation: "10.0.0.9"}

	resolver := &Resolver{}
	dest, err := resolver.ForService(context.Background(), svc)
	if err != nil {
		t.Fatalf("ForService returned error: %v", err)
	}
	if dest.IP != "10.0.0.9" || dest.Source != SourceAnnotation {
		t.Errorf("got %q from %q, want 10.0.0.9 from annotation", dest.IP, dest.Source)
	}
}

func TestForServiceIngressBeatsUnsetType(t *testing.T) {
	// spec.type is always set by a real API server, but an assigned ingress IP is
	// a routable address regardless of what the type field says.
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"}}
	svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "192.168.1.50"}}

	resolver := &Resolver{}
	dest, err := resolver.ForService(context.Background(), svc)
	if err != nil {
		t.Fatalf("ForService returned error: %v", err)
	}
	if dest.IP != "192.168.1.50" {
		t.Errorf("IP = %q, want 192.168.1.50", dest.IP)
	}
}

func TestForServiceClusterIPUnsupported(t *testing.T) {
	resolver := &Resolver{}
	_, err := resolver.ForService(context.Background(), service("web", corev1.ServiceTypeClusterIP))
	if err == nil {
		t.Fatal("expected ClusterIP to be rejected")
	}
	if !IsUnsupportedType(err) {
		t.Errorf("expected an unsupported-type error, got %v", err)
	}

	var unsupported *ErrUnsupportedType
	if !errors.As(err, &unsupported) || unsupported.Type != corev1.ServiceTypeClusterIP {
		t.Errorf("expected ErrUnsupportedType for ClusterIP, got %v", err)
	}
}

func TestForServiceExternalNameUnsupported(t *testing.T) {
	resolver := &Resolver{}
	_, err := resolver.ForService(context.Background(), service("web", corev1.ServiceTypeExternalName))
	if !IsUnsupportedType(err) {
		t.Errorf("expected an unsupported-type error, got %v", err)
	}
}

func TestForServiceNodePortPicksLowestNamedReadyNode(t *testing.T) {
	c := newFakeClient(
		node("node-c", "192.168.1.23", true, nil),
		node("node-a", "192.168.1.21", true, nil),
		node("node-b", "192.168.1.22", true, nil),
	)

	resolver := &Resolver{Client: c}
	dest, err := resolver.ForService(context.Background(), service("web", corev1.ServiceTypeNodePort))
	if err != nil {
		t.Fatalf("ForService returned error: %v", err)
	}
	if dest.IP != "192.168.1.21" {
		t.Errorf("IP = %q, want 192.168.1.21 (node-a)", dest.IP)
	}
	if dest.NodeName != "node-a" {
		t.Errorf("NodeName = %q, want node-a", dest.NodeName)
	}
	if dest.Source != SourceNode {
		t.Errorf("Source = %q, want %q", dest.Source, SourceNode)
	}
}

func TestForServiceNodePortSkipsIneligibleNodes(t *testing.T) {
	c := newFakeClient(
		node("node-a", "192.168.1.21", false, nil), // not Ready
		node("node-b", "192.168.1.22", true, map[string]string{corev1.LabelNodeExcludeBalancers: ""}),
		node("node-c", "", true, nil), // no address
		node("node-d", "192.168.1.24", true, nil),
	)

	resolver := &Resolver{Client: c}
	dest, err := resolver.ForService(context.Background(), service("web", corev1.ServiceTypeNodePort))
	if err != nil {
		t.Fatalf("ForService returned error: %v", err)
	}
	if dest.IP != "192.168.1.24" {
		t.Errorf("IP = %q, want 192.168.1.24 (node-d, the only eligible node)", dest.IP)
	}
}

func TestForServiceNodePortNoEligibleNodes(t *testing.T) {
	c := newFakeClient(node("node-a", "192.168.1.21", false, nil))

	resolver := &Resolver{Client: c}
	if _, err := resolver.ForService(context.Background(), service("web", corev1.ServiceTypeNodePort)); err == nil {
		t.Error("expected an error when no node is eligible")
	}
}

func TestForServiceNodePortPrefersExternalIPsOverNodes(t *testing.T) {
	c := newFakeClient(node("node-a", "192.168.1.21", true, nil))

	svc := service("web", corev1.ServiceTypeNodePort)
	svc.Spec.ExternalIPs = []string{"192.168.1.99"}

	resolver := &Resolver{Client: c}
	dest, err := resolver.ForService(context.Background(), svc)
	if err != nil {
		t.Fatalf("ForService returned error: %v", err)
	}
	if dest.IP != "192.168.1.99" || dest.Source != SourceExternalIP {
		t.Errorf("got %q from %q, want 192.168.1.99 from externalIP", dest.IP, dest.Source)
	}
}

func TestForServiceNodePortListFailureMentionsRemedies(t *testing.T) {
	c := &nodeClient{listErr: errors.New("nodes is forbidden")}

	resolver := &Resolver{Client: c}
	_, err := resolver.ForService(context.Background(), service("web", corev1.ServiceTypeNodePort))
	if err == nil {
		t.Fatal("expected an error when nodes cannot be listed")
	}

	// The usual cause is missing RBAC, so the message has to name both ways out.
	for _, want := range []string{"nodes", IPAnnotation} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestForServiceNodePortWithoutClient(t *testing.T) {
	resolver := &Resolver{}
	_, err := resolver.ForService(context.Background(), service("web", corev1.ServiceTypeNodePort))
	if err == nil {
		t.Fatal("expected an error when no client is available for node discovery")
	}
	if IsUnsupportedType(err) {
		t.Error("a missing client is a transient problem, not an unsupported type")
	}
}

func TestNodeIPPrefersInternal(t *testing.T) {
	n := &corev1.Node{
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeExternalIP, Address: "203.0.113.5"},
				{Type: corev1.NodeInternalIP, Address: "192.168.1.21"},
			},
		},
	}
	if got := nodeIP(n); got != "192.168.1.21" {
		t.Errorf("nodeIP = %q, want the internal address 192.168.1.21", got)
	}

	external := &corev1.Node{
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeExternalIP, Address: "203.0.113.5"}},
		},
	}
	if got := nodeIP(external); got != "203.0.113.5" {
		t.Errorf("nodeIP = %q, want the external address as fallback", got)
	}
}

func TestDeclaredIP(t *testing.T) {
	withIngress := service("web", corev1.ServiceTypeLoadBalancer)
	withIngress.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "192.168.1.50"}}

	annotated := service("web", corev1.ServiceTypeNodePort)
	annotated.Annotations = map[string]string{IPAnnotation: "10.0.0.9"}

	withExternal := service("web", corev1.ServiceTypeNodePort)
	withExternal.Spec.ExternalIPs = []string{"192.168.1.99"}

	tests := []struct {
		name string
		svc  *corev1.Service
		want string
	}{
		{name: "loadbalancer ingress", svc: withIngress, want: "192.168.1.50"},
		{name: "annotation", svc: annotated, want: "10.0.0.9"},
		{name: "external ip", svc: withExternal, want: "192.168.1.99"},
		{name: "nodeport relying on discovery declares nothing", svc: service("web", corev1.ServiceTypeNodePort), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeclaredIP(tt.svc); got != tt.want {
				t.Errorf("DeclaredIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsSupportedType(t *testing.T) {
	supported := []corev1.ServiceType{corev1.ServiceTypeLoadBalancer, corev1.ServiceTypeNodePort}
	for _, serviceType := range supported {
		if !IsSupportedType(serviceType) {
			t.Errorf("%s should be supported", serviceType)
		}
	}

	unsupported := []corev1.ServiceType{corev1.ServiceTypeClusterIP, corev1.ServiceTypeExternalName}
	for _, serviceType := range unsupported {
		if IsSupportedType(serviceType) {
			t.Errorf("%s should not be supported", serviceType)
		}
	}
}
