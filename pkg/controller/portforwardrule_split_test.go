package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"unifi-port-forward/pkg/api/v1alpha1"
	"unifi-port-forward/pkg/destination"
	"unifi-port-forward/pkg/ports"
)

// dualProtocolNodePortService builds a NodePort Service exposing one port over
// both protocols, the shape Kubernetes produces for a TCP+UDP service. The two
// entries share a service port and differ only in protocol and nodePort.
func dualProtocolNodePortService(tcpNodePort, udpNodePort int32) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "game",
			Namespace:   "default",
			Annotations: map[string]string{destination.IPAnnotation: "192.168.1.8"},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
			Ports: []corev1.ServicePort{
				{Name: "game-tcp", Port: 7777, NodePort: tcpNodePort, Protocol: corev1.ProtocolTCP},
				{Name: "game-udp", Port: 7777, NodePort: udpNodePort, Protocol: corev1.ProtocolUDP},
			},
		},
	}
}

func bothRule(servicePortName string) *v1alpha1.PortForwardRule {
	return &v1alpha1.PortForwardRule{
		ObjectMeta: metav1.ObjectMeta{Name: "game-rule", Namespace: "default"},
		Spec: v1alpha1.PortForwardRuleSpec{
			ExternalPort: intstr.FromInt32(27777),
			Protocol:     protocolBoth,
			Interface:    "wan",
			Enabled:      true,
			ServiceRef:   &v1alpha1.ServiceReference{Name: "game", Port: servicePortName},
		},
	}
}

// A "both" rule cannot be served by one router rule once the two protocols sit
// on different nodePorts, because a UniFi rule carries a single fwd_port.
func TestGetServiceDestinationsSplitsDivergingNodePorts(t *testing.T) {
	for _, referenced := range []string{"game-tcp", "game-udp"} {
		t.Run("referencing "+referenced, func(t *testing.T) {
			reconciler, _ := newRuleReconciler(t, dualProtocolNodePortService(31000, 31001))

			bindings, err := reconciler.getServiceDestinations(
				context.Background(), bothRule(referenced), ports.FromPort(27777))
			if err != nil {
				t.Fatalf("getServiceDestinations returned error: %v", err)
			}

			if len(bindings) != 2 {
				t.Fatalf("got %d bindings, want 2: %+v", len(bindings), bindings)
			}

			// Ordering is fixed at tcp then udp so the generated rule names are
			// stable across reconciles.
			if bindings[0].Protocol != protocolTCP || bindings[0].Ports.String() != "31000" {
				t.Errorf("binding[0] = %s/%s, want tcp/31000", bindings[0].Protocol, bindings[0].Ports)
			}
			if bindings[1].Protocol != protocolUDP || bindings[1].Ports.String() != "31001" {
				t.Errorf("binding[1] = %s/%s, want udp/31001", bindings[1].Protocol, bindings[1].Ports)
			}
			for _, binding := range bindings {
				if binding.IP != "192.168.1.8" {
					t.Errorf("binding IP = %q, want 192.168.1.8", binding.IP)
				}
			}
		})
	}
}

// Explicitly pinning both entries to one nodePort is legal, and a single "both"
// rule already describes it, so that case must not split.
func TestGetServiceDestinationsKeepsSharedNodePortWhole(t *testing.T) {
	reconciler, _ := newRuleReconciler(t, dualProtocolNodePortService(31000, 31000))

	bindings, err := reconciler.getServiceDestinations(
		context.Background(), bothRule("game-tcp"), ports.FromPort(27777))
	if err != nil {
		t.Fatalf("getServiceDestinations returned error: %v", err)
	}

	if len(bindings) != 1 {
		t.Fatalf("got %d bindings, want 1: %+v", len(bindings), bindings)
	}
	if bindings[0].Protocol != protocolBoth {
		t.Errorf("binding protocol = %q, want both", bindings[0].Protocol)
	}
	if bindings[0].Ports.String() != "31000" {
		t.Errorf("binding ports = %q, want 31000", bindings[0].Ports)
	}
}

// A LoadBalancer service forwards to the service port, which is the same number
// for both protocols, so there is nothing to split.
func TestGetServiceDestinationsKeepsLoadBalancerWhole(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "game", Namespace: "default"},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{
				{Name: "game-tcp", Port: 7777, Protocol: corev1.ProtocolTCP},
				{Name: "game-udp", Port: 7777, Protocol: corev1.ProtocolUDP},
			},
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{IP: "192.168.1.50"}},
			},
		},
	}
	reconciler, _ := newRuleReconciler(t, service)

	bindings, err := reconciler.getServiceDestinations(
		context.Background(), bothRule("game-tcp"), ports.FromPort(27777))
	if err != nil {
		t.Fatalf("getServiceDestinations returned error: %v", err)
	}

	if len(bindings) != 1 {
		t.Fatalf("got %d bindings, want 1: %+v", len(bindings), bindings)
	}
	if bindings[0].Protocol != protocolBoth || bindings[0].Ports.String() != "7777" {
		t.Errorf("binding = %s/%s, want both/7777", bindings[0].Protocol, bindings[0].Ports)
	}
}

// An unallocated nodePort on either side has to fail rather than forward one
// protocol to port 0.
func TestGetServiceDestinationsRejectsUnallocatedSibling(t *testing.T) {
	reconciler, _ := newRuleReconciler(t, dualProtocolNodePortService(31000, 0))

	if _, err := reconciler.getServiceDestinations(
		context.Background(), bothRule("game-tcp"), ports.FromPort(27777)); err == nil {
		t.Error("expected an error when the udp sibling has no nodePort allocated yet")
	}
}

func TestReconcileSplitsBothIntoTwoRouterRules(t *testing.T) {
	reconciler, mockRouter := newRuleReconciler(t, dualProtocolNodePortService(31000, 31001))
	rule := bothRule("game-tcp")

	if err := reconciler.reconcilePortForwardRule(context.Background(), rule); err != nil {
		t.Fatalf("reconcilePortForwardRule returned error: %v", err)
	}

	created := mockRouter.GetPortForwardRules()
	if len(created) != 2 {
		t.Fatalf("expected 2 router rules, got %d: %+v", len(created), created)
	}

	byProtocol := map[string]string{}
	for _, pf := range created {
		byProtocol[pf.Proto] = pf.FwdPort
		// Both halves share an external port, so the name has to carry the
		// protocol or the second would overwrite the first on the router.
		want := "default/game-rule:27777/" + pf.Proto
		if pf.Name != want {
			t.Errorf("router rule name = %q, want %q", pf.Name, want)
		}
		if pf.DstPort != "27777" {
			t.Errorf("router DstPort = %q, want 27777", pf.DstPort)
		}
	}

	if byProtocol[protocolTCP] != "31000" {
		t.Errorf("tcp rule FwdPort = %q, want 31000", byProtocol[protocolTCP])
	}
	if byProtocol[protocolUDP] != "31001" {
		t.Errorf("udp rule FwdPort = %q, want 31001", byProtocol[protocolUDP])
	}

	if rule.Status.RouterRuleID != "default/game-rule:27777/tcp,default/game-rule:27777/udp" {
		t.Errorf("status RouterRuleID = %q, want both rule IDs", rule.Status.RouterRuleID)
	}
	if rule.Status.ServiceStatus == nil {
		t.Fatal("expected service status to be populated")
	}
	if rule.Status.ServiceStatus.ServicePorts != "31000/tcp,31001/udp" {
		t.Errorf("status ServicePorts = %q, want 31000/tcp,31001/udp", rule.Status.ServiceStatus.ServicePorts)
	}
}

// Deleting a split rule has to clean up both halves; leaving one behind would
// keep forwarding one protocol to a nodePort that no longer exists.
func TestDeleteRouterRuleRemovesBothHalvesOfSplitRule(t *testing.T) {
	reconciler, mockRouter := newRuleReconciler(t, dualProtocolNodePortService(31000, 31001))
	rule := bothRule("game-tcp")

	if err := reconciler.reconcilePortForwardRule(context.Background(), rule); err != nil {
		t.Fatalf("reconcilePortForwardRule returned error: %v", err)
	}
	if got := len(mockRouter.GetPortForwardRules()); got != 2 {
		t.Fatalf("setup: expected 2 router rules, got %d", got)
	}

	if err := reconciler.deleteRouterRuleByID(context.Background(), rule); err != nil {
		t.Fatalf("deleteRouterRuleByID returned error: %v", err)
	}

	if remaining := mockRouter.GetPortForwardRules(); len(remaining) != 0 {
		t.Errorf("expected both router rules removed, %d left: %+v", len(remaining), remaining)
	}
}

// The sweep must not delete a rule on the same port and protocol that belongs to
// a different resource.
func TestDeleteRouterRuleLeavesRulesItDoesNotOwn(t *testing.T) {
	reconciler, mockRouter := newRuleReconciler(t, dualProtocolNodePortService(31000, 31001))

	if err := reconciler.reconcilePortForwardRule(context.Background(), bothRule("game-tcp")); err != nil {
		t.Fatalf("reconcilePortForwardRule returned error: %v", err)
	}

	other := bothRule("game-tcp")
	other.Name = "someone-elses-rule"

	if err := reconciler.deleteRouterRuleByID(context.Background(), other); err != nil {
		t.Fatalf("deleteRouterRuleByID returned error: %v", err)
	}

	if remaining := mockRouter.GetPortForwardRules(); len(remaining) != 2 {
		t.Errorf("expected the original 2 rules untouched, got %d: %+v", len(remaining), remaining)
	}
}
