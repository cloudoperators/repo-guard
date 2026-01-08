// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

/*
Copyright 2023 cc.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GithubSpec defines the desired state of Github
type GithubSpec struct {
	WebURL          string `json:"webURL,omitempty"`
	V3APIURL        string `json:"v3APIURL,omitempty"`
	IntegrationID   int64  `json:"integrationID,omitempty"`
	ClientUserAgent string `json:"clientUserAgent,omitempty"`
	Secret          string `json:"secret,omitempty"`
}

const (
	SECRET_CLIENT_ID_KEY     = "clientID"
	SECRET_CLIENT_SECRET_KEY = "clientSecret"
	SECRET_PRIVATE_KEY_KEY   = "privateKey"
)

// GithubStatus defines the observed state of Github
type GithubStatus struct {
	State     GithubState `json:"state,omitempty"`
	Error     string      `json:"error,omitempty"`
	Timestamp metav1.Time `json:"timestamp,omitempty"`
}

type GithubState string

const (
	GithubStateRunning GithubState = "running"
	GithubStateFailed  GithubState = "failed"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.state"
// +kubebuilder:printcolumn:name="Last Change",type="date",JSONPath=".status.timestamp"
type Github struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GithubSpec   `json:"spec,omitempty"`
	Status GithubStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GithubList contains a list of Github
type GithubList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Github `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Github{}, &GithubList{})
}
