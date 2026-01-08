package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type StaticGroup struct {
	Group   string   `json:"group,omitempty"`
	Members []string `json:"members,omitempty"`
}

type StaticMemberProviderSpec struct {
	Groups []StaticGroup `json:"groups,omitempty"`
}

type StaticMemberProviderStatus struct {
	State     ExternalMemberProviderState `json:"state,omitempty"`
	Error     string                      `json:"error,omitempty"`
	Timestamp metav1.Time                 `json:"timestamp,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// StaticMemberProvider provides static members by group
type StaticMemberProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StaticMemberProviderSpec   `json:"spec,omitempty"`
	Status StaticMemberProviderStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

type StaticMemberProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StaticMemberProvider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&StaticMemberProvider{}, &StaticMemberProviderList{})
}
