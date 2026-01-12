// Package v1alpha1 contains API Schema definitions for the port-forwarder v1alpha1 API group
// +kubebuilder:object:generate=true
// +groupName=port-forwarder.unifi.com
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PortForwardRuleSpec defines the desired state of PortForwardRule
type PortForwardRuleSpec struct {
	// ExternalPort is the WAN port to forward
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:required
	ExternalPort int `json:"externalPort"`

	// Protocol specifies the forwarding protocol
	// +kubebuilder:validation:Enum=tcp;udp;both
	// +kubebuilder:default=tcp
	Protocol string `json:"protocol,omitempty"`

	// ServiceRef references a Service for destination (mutually exclusive with DestinationIP)
	ServiceRef *ServiceReference `json:"serviceRef,omitempty"`

	// DestinationIP is the target IP address (mutually exclusive with ServiceRef)
	// +kubebuilder:validation:Format=ipv4
	DestinationIP *string `json:"destinationIP,omitempty"`

	// DestinationPort is the target port (required if DestinationIP is set)
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	DestinationPort *int `json:"destinationPort,omitempty"`

	// Enabled controls whether this rule is active
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// Description provides a human-readable description
	// +kubebuilder:validation:MaxLength=256
	Description string `json:"description,omitempty"`

	// SourceIPRestriction limits source IP access (empty means no restriction)
	// +kubebuilder:validation:Format=ipv4
	SourceIPRestriction *string `json:"sourceIPRestriction,omitempty"`

	// Interface specifies the network interface
	// +kubebuilder:default=wan
	Interface string `json:"interface,omitempty"`

	// Priority determines rule precedence (higher number = higher priority)
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	// +kubebuilder:default=100
	Priority int `json:"priority,omitempty"`

	// ConflictPolicy determines how to handle port conflicts
	// +kubebuilder:validation:Enum=warn;error;ignore
	// +kubebuilder:default=warn
	ConflictPolicy string `json:"conflictPolicy,omitempty"`

	// LogEnabled enables logging for this rule
	// +kubebuilder:default=false
	LogEnabled bool `json:"logEnabled,omitempty"`
}

// Phase constants
const (
	PhasePending = "Pending"
	PhaseActive  = "Active"
	PhaseFailed  = "Failed"
	PhaseUnknown = "Unknown"
)

// ServiceReference references a Kubernetes Service
type ServiceReference struct {
	// Name is the Service name (required)
	// +kubebuilder:required
	Name string `json:"name"`

	// Namespace is the Service namespace (defaults to rule namespace)
	Namespace *string `json:"namespace,omitempty"`

	// Port is the service port name or number (required)
	// +kubebuilder:required
	Port string `json:"port"`
}

// PortForwardRuleStatus defines the observed state of PortForwardRule
type PortForwardRuleStatus struct {
	// Phase is the current phase of the rule
	// +kubebuilder:validation:Enum=Pending;Active;Failed;Unknown
	Phase string `json:"phase,omitempty"`

	// ObservedGeneration is the generation observed by the controller
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// LastAppliedTime is when the rule was last applied
	LastAppliedTime *metav1.Time `json:"lastAppliedTime,omitempty"`

	// RouterRuleID is the ID of the rule on the router
	RouterRuleID string `json:"routerRuleID,omitempty"`

	// ServiceStatus contains service-specific status
	ServiceStatus *ServiceStatus `json:"serviceStatus,omitempty"`

	// Conditions represent the latest available observations of the rule's state
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Conflicts with other port forwarding rules
	Conflicts []PortConflict `json:"conflicts,omitempty"`

	// ErrorInfo contains error details when phase is Failed
	ErrorInfo *ErrorInfo `json:"errorInfo,omitempty"`
}

// ServiceStatus contains status information about the referenced service
type ServiceStatus struct {
	// Name is the service name
	Name string `json:"name,omitempty"`

	// Namespace is the service namespace
	Namespace string `json:"namespace,omitempty"`

	// LoadBalancerIP is the service's LoadBalancer IP
	LoadBalancerIP string `json:"loadBalancerIP,omitempty"`

	// ServicePort is the resolved service port number
	ServicePort int32 `json:"servicePort,omitempty"`

	// ServicePortName is the resolved service port name
	ServicePortName string `json:"servicePortName,omitempty"`
}

// PortConflict represents a conflict with another port forwarding rule
type PortConflict struct {
	// ConflictingNamespace is the namespace of the conflicting rule
	ConflictingNamespace string `json:"conflictingNamespace,omitempty"`

	// ConflictingResource is the name of the conflicting resource
	ConflictingResource string `json:"conflictingResource,omitempty"`

	// ConflictType is the type of conflict
	// +kubebuilder:validation:Enum=PortConflict;ServiceConflict;IPConflict
	ConflictType string `json:"conflictType,omitempty"`

	// Description describes the conflict
	Description string `json:"description,omitempty"`

	// Severity is the conflict severity
	// +kubebuilder:validation:Enum=Warning;Error
	Severity string `json:"severity,omitempty"`

	// Timestamp when the conflict was detected
	Timestamp *metav1.Time `json:"timestamp,omitempty"`
}

// ErrorInfo contains error details
type ErrorInfo struct {
	// Code is the error code
	Code string `json:"code,omitempty"`

	// Message is the error message
	Message string `json:"message,omitempty"`

	// LastFailureTime is when the error occurred
	LastFailureTime *metav1.Time `json:"lastFailureTime,omitempty"`

	// RetryCount is the number of retry attempts
	RetryCount int `json:"retryCount,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Namespaced
//+kubebuilder:printcolumn:name="External Port",type="integer",JSONPath=".spec.externalPort"
//+kubebuilder:printcolumn:name="Protocol",type="string",JSONPath=".spec.protocol"
//+kubebuilder:printcolumn:name="Service",type="string",JSONPath=".spec.serviceRef.name"
//+kubebuilder:printcolumn:name="Enabled",type="boolean",JSONPath=".spec.enabled"
//+kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PortForwardRule is the Schema for the portforwardrules API
type PortForwardRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PortForwardRuleSpec   `json:"spec,omitempty"`
	Status PortForwardRuleStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true
//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PortForwardRuleList contains a list of PortForwardRule
type PortForwardRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PortForwardRule `json:"items"`
}
