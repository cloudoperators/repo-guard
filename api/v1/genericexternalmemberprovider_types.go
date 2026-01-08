// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GenericExternalMemberProviderSpec contains HTTP configuration for generic providers
// Secret may contain username/password or token.
type GenericExternalMemberProviderSpec struct {
	Endpoint          string `json:"endpoint,omitempty"`
	Secret            string `json:"secret,omitempty"`
	ResultsField      string `json:"resultsField,omitempty"`
	IDField           string `json:"idField,omitempty"`
	Paginated         bool   `json:"paginated,omitempty"`
	TotalPagesField   string `json:"totalPagesField,omitempty"`
	PageParam         string `json:"pageParam,omitempty"`
	TestConnectionURL string `json:"testConnectionURL,omitempty"`
}

type GenericExternalMemberProviderStatus struct {
	State     ExternalMemberProviderState `json:"state,omitempty"`
	Error     string                      `json:"error,omitempty"`
	Timestamp metav1.Time                 `json:"timestamp,omitempty"`
}

type ExternalMemberProviderState string

const (
	ExternalMemberProviderStateRunning ExternalMemberProviderState = "running"
	ExternalMemberProviderStateFailed  ExternalMemberProviderState = "failed"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.state"
// +kubebuilder:printcolumn:name="Last Change",type="date",JSONPath=".status.timestamp"
// GenericExternalMemberProvider is the Schema for HTTP based external member providers
type GenericExternalMemberProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GenericExternalMemberProviderSpec   `json:"spec,omitempty"`
	Status GenericExternalMemberProviderStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

type GenericExternalMemberProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GenericExternalMemberProvider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GenericExternalMemberProvider{}, &GenericExternalMemberProviderList{})
}

const (
	SECRET_USERNAME_KEY = "username"
	SECRET_PASSWORD_KEY = "password"
)
