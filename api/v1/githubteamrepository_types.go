// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

/*
Copyright 2023 cc.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GithubTeamRepositorySpec defines the exceptional additions to default organization team & repo assignments
type GithubTeamRepositorySpec struct {
	Github       string               `json:"github,omitempty"`
	Organization string               `json:"organization,omitempty"`
	Team         string               `json:"team,omitempty"`
	Repository   []string             `json:"repository,omitempty"`
	Permission   GithubTeamPermission `json:"permission,omitempty"`
}

// GithubTeamRepositoryStatus defines the observed state of GithubTeamRepository
type GithubTeamRepositoryStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Github",type="string",JSONPath=".spec.github"
// +kubebuilder:printcolumn:name="Organization",type="string",JSONPath=".spec.organization"
// +kubebuilder:printcolumn:name="Team",type="string",JSONPath=".spec.team"
type GithubTeamRepository struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GithubTeamRepositorySpec   `json:"spec,omitempty"`
	Status GithubTeamRepositoryStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GithubTeamRepositoryList contains a list of GithubTeamRepository
type GithubTeamRepositoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GithubTeamRepository `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GithubTeamRepository{}, &GithubTeamRepositoryList{})
}
