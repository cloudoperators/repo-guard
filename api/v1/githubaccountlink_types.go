package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type GithubAccountLinkSpec struct {
	GreenhouseUserID string `json:"userID,omitempty"`
	GithubUserID     string `json:"githubUserID,omitempty"`
	Github           string `json:"github,omitempty"`
}

type GithubAccountLinkStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Github",type="string",JSONPath=".spec.github"
// +kubebuilder:printcolumn:name="User",type="string",JSONPath=".spec.userID"
// +kubebuilder:printcolumn:name="Github ID",type="string",JSONPath=".spec.githubUserID"
type GithubAccountLink struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GithubAccountLinkSpec   `json:"spec,omitempty"`
	Status GithubAccountLinkStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GithubAccountLinkList contains a list of GithubAccountLink
type GithubAccountLinkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GithubAccountLink `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GithubAccountLink{}, &GithubAccountLinkList{})
}

// Email verification nnotations. When set on GithubAccountLink, controller checks whether the linked GitHub user  has a verified email address under the specified domain and stores the result.
// Example:
//
//	{
//	  "<org>": { "domain": "example.com", "enabled": true, "ttl": "1h" },
//	  ...
//	}
//
// Results format:
//
//	{
//	  "<org>": { "domain": "example.com", "verified": true, "timestamp": "RFC3339" },
//	  ...
//	}
const GITHUB_ACCOUNT_LINK_EMAIL_CHECK_CONFIG = "githubguard.sap/email-check-config"
const GITHUB_ACCOUNT_LINK_EMAIL_CHECK_RESULTS = "githubguard.sap/email-check-results"
const GITHUB_ACCOUNT_LINK_CHECK_EMAIL_TIMESTAMP = "githubguard.sap/check-email-timestamp"
const GITHUB_ACCOUNT_LINK_CHECK_EMAIL_TTL = "githubguard.sap/check-email-ttl"
