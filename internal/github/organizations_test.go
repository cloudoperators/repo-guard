// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	gogithub "github.com/google/go-github/v85/github"
)

// newTestOrganizationProvider creates a DefaultOrganizationProvider backed by a
// fake HTTP server. The caller receives both the provider and the mux to
// register route handlers before calling provider methods.
func newTestOrganizationProvider(t *testing.T) (*DefaultOrganizationProvider, *http.ServeMux) {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	httpClient := srv.Client()
	client, err := gogithub.NewClient(httpClient).WithAuthToken("test-token").WithEnterpriseURLs(srv.URL+"/", srv.URL+"/")
	if err != nil {
		t.Fatalf("create github client: %v", err)
	}

	provider := &DefaultOrganizationProvider{
		organizationService: *client.Organizations,
		usersService:        *client.Users,
		organization:        "test-org",
	}
	return provider, mux
}

func TestPendingAdminMembers_404TreatedAsEmpty(t *testing.T) {
	provider, mux := newTestOrganizationProvider(t)

	mux.HandleFunc("/api/v3/orgs/test-org/invitations", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprintln(w, `{"message":"Not Found"}`)
	})

	members, err := provider.pendingAdminMembers(t.Context())
	if err != nil {
		t.Fatalf("expected no error for 404, got: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("expected empty members for 404, got %d", len(members))
	}
}

func TestPendingAdminMembers_ReturnsAdminInvitees(t *testing.T) {
	provider, mux := newTestOrganizationProvider(t)

	mux.HandleFunc("/api/v3/orgs/test-org/invitations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `[
			{"login": "alice", "role": "admin"},
			{"login": "bob",   "role": "member"}
		]`)
	})
	mux.HandleFunc("/api/v3/users/alice", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"login": "alice", "id": 42}`)
	})

	members, err := provider.pendingAdminMembers(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 admin invitee, got %d", len(members))
	}
	if members[0].Login != "alice" || members[0].UID != 42 {
		t.Errorf("unexpected member: %+v", members[0])
	}
}

func TestPendingAdminMembers_PropagatesNon404Error(t *testing.T) {
	provider, mux := newTestOrganizationProvider(t)

	mux.HandleFunc("/api/v3/orgs/test-org/invitations", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, `{"message":"Internal Server Error"}`)
	})

	_, err := provider.pendingAdminMembers(t.Context())
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
}
