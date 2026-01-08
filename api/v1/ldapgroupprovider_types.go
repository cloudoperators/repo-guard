// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LDAPGroupProviderSpec defines the desired state of LDAPGroupProvider
type LDAPGroupProviderSpec struct {
	Host   string `json:"host,omitempty"`
	BaseDN string `json:"baseDN,omitempty"`
	Secret string `json:"secret,omitempty"`
}

const (
	SECRET_BIND_DN = "bindDN"
	SECRET_BIND_PW = "bindPW"
)

// LDAPGroupProviderStatus defines the observed state of LDAPGroupProvider
type LDAPGroupProviderStatus struct {
	State     LDAPGroupProviderState `json:"state,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Timestamp metav1.Time            `json:"timestamp,omitempty"`
}

type LDAPGroupProviderState string

const (
	LDAPGroupProviderStateRunning LDAPGroupProviderState = "running"
	LDAPGroupProviderStateFailed  LDAPGroupProviderState = "failed"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.state"
// +kubebuilder:printcolumn:name="Last Change",type="date",JSONPath=".status.timestamp"
// LDAPGroupProvider is the Schema for the ldapgroupproviders API
type LDAPGroupProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LDAPGroupProviderSpec   `json:"spec,omitempty"`
	Status LDAPGroupProviderStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// LDAPGroupProviderList contains a list of LDAPGroupProvider
type LDAPGroupProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LDAPGroupProvider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LDAPGroupProvider{}, &LDAPGroupProviderList{})
}
