// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	greenhousesapv1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	"github.com/joho/godotenv"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TEST_ENV is loaded once in suite_test.go (BeforeSuite).
var TEST_ENV map[string]string

var (
	GITHUB_INTEGRATION_ID  int64
	GITHUB_INSTALLATION_ID int64

	// TestOperatorNamespace is where operator-managed secrets live in tests.
	// Defaults to NAMESPACE if OPERATOR_NAMESPACE is not set.
	TestOperatorNamespace string
)

var (
	githubCom       *repoguardsapv1.Github
	githubComSecret *v1.Secret

	githubTeamOwnerTest                                     *repoguardsapv1.GithubTeam
	greenhouseTeamOwnerTest                                 *greenhousesapv1alpha1.Team
	githubOrganizationGreenhouseSandboxForOrganizationTests *repoguardsapv1.GithubOrganization
	githubAccountLinkOrgOwner                               *repoguardsapv1.GithubAccountLink
	githubAccountLinkForExternalMemberProviderLDAP          *repoguardsapv1.GithubAccountLink
	githubTeamTestWithExternalMemberProviderLDAP            *repoguardsapv1.GithubTeam
	ldapGroupProvider                                       *repoguardsapv1.LDAPGroupProvider
	ldapGroupProviderSecret                                 *v1.Secret
	githubOrganizationGreenhouseSandboxForTeamTests         *repoguardsapv1.GithubOrganization
	githubTeamTest                                          *repoguardsapv1.GithubTeam
	greenhouseTeamTest                                      *greenhousesapv1alpha1.Team
	githubTeamTestWithExternalUsername                      *repoguardsapv1.GithubTeam
	greenhouseTeamTestWithExternalUsername                  *greenhousesapv1alpha1.Team
	githubAccountLink                                       *repoguardsapv1.GithubAccountLink
	githubOrganizationGreenhouseSandboxForRepositoryTests   *repoguardsapv1.GithubOrganization
	githubTeamRepository                                    *repoguardsapv1.GithubTeamRepository
	githubOrganizationGreenhouseSandboxForTTLTests          *repoguardsapv1.GithubOrganization

	empHTTP                                      *repoguardsapv1.GenericExternalMemberProvider
	empHTTPSecret                                *v1.Secret
	githubTeamTestWithExternalMemberProviderHTTP *repoguardsapv1.GithubTeam

	empStatic                                      *repoguardsapv1.StaticMemberProvider
	githubTeamTestWithExternalMemberProviderStatic *repoguardsapv1.GithubTeam
)

const (
	TEST_DEFAULT_PUBLIC_REPO_PULL  = "public-pull-team"
	TEST_DEFAULT_PUBLIC_REPO_PUSH  = "public-push-team"
	TEST_DEFAULT_PUBLIC_REPO_ADMIN = "public-admin-team"

	TEST_DEFAULT_PRIVATE_REPO_PULL  = "private-pull-team"
	TEST_DEFAULT_PRIVATE_REPO_PUSH  = "private-push-team"
	TEST_DEFAULT_PRIVATE_REPO_ADMIN = "private-admin-team"

	TEST_CUSTOM_TEAM = "custom-team-for-private-repo"
)

func loadTestEnv() map[string]string {
	env, err := godotenv.Read("test.env")
	if err != nil {
		env = make(map[string]string)
	}

	// Allow overriding from process env (both KEY and TEST_KEY)
	keys := []string{
		"GITHUB_TOKEN", "GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET", "GITHUB_PRIVATE_KEY",
		"ORGANIZATION", "NAMESPACE", "OPERATOR_NAMESPACE",
		"GITHUB_KUBERNETES_RESOURCE_NAME", "GITHUB_KUBERNETES_SECRET_RESOURCE_NAME",
		"GITHUB_WEB_URL", "GITHUB_V3_API_URL", "GITHUB_INTEGRATION_ID", "GITHUB_INSTALLATION_ID",

		"ORGANIZATION_KUBERNETES_RESOURCE_NAME",
		"ORGANIZATION_OWNER_TEAM", "ORGANIZATION_OWNER_TEAM_KUBERNETES_RESOURCE_NAME", "ORGANIZATION_OWNER_GREENHOUSE_ID",

		"TEAM_1", "TEAM_1_KUBERNETES_RESOURCE_NAME",
		"TEAM_2", "TEAM_2_KUBERNETES_RESOURCE_NAME",

		"USER_0_GREENHOUSE_ID", "USER_0_GITHUB_USERID", "USER_0_GITHUB_USERNAME",
		"USER_1", "USER_1_GREENHOUSE_ID", "USER_1_GITHUB_USERID", "USER_1_GITHUB_USERNAME",
		"USER_2",

		"LDAP_GROUP_PROVIDER_HOST", "LDAP_GROUP_PROVIDER_BASE_DN", "LDAP_GROUP_PROVIDER_BIND_DN", "LDAP_GROUP_PROVIDER_BIND_PW",
		"LDAP_GROUP_PROVIDER_KUBERNETES_RESOURCE_NAME", "LDAP_GROUP_PROVIDER_KUBERNETES_SECRET_RESOURCE_NAME",
		"LDAP_GROUP_PROVIDER_TEAM_NAME", "LDAP_GROUP_PROVIDER_TEAM_KUBERNETES_RESOURCE_NAME",
		"LDAP_GROUP_PROVIDER_GROUP_NAME", "LDAP_GROUP_PROVIDER_USER_INTERNAL_USERNAME", "LDAP_GROUP_PROVIDER_USER_GITHUB_USERID",
		"LDAP_GROUP_PROVIDER_USER_GITHUB_USERNAME",

		"EMP_HTTP_ENDPOINT", "EMP_HTTP_TEST_CONNECTION_URL",
		"EMP_HTTP_USERNAME", "EMP_HTTP_PASSWORD", "EMP_HTTP_GROUP_ID", "EMP_HTTP_USER_INTERNAL_USERNAME",
		"EMP_HTTP_USER_GITHUB_USERNAME",
		"EMP_HTTP_KUBERNETES_RESOURCE_NAME", "EMP_HTTP_KUBERNETES_SECRET_RESOURCE_NAME", "EMP_HTTP_TEAM_NAME", "EMP_HTTP_TEAM_KUBERNETES_RESOURCE_NAME",

		"EMP_STATIC_KUBERNETES_RESOURCE_NAME", "EMP_STATIC_TEAM_NAME", "EMP_STATIC_TEAM_KUBERNETES_RESOURCE_NAME",
		"EMP_STATIC_USER_INTERNAL_USERNAME", "EMP_STATIC_USER_GITHUB_USERNAME",

		// Verified-domain email filtering test
		"EMAIL_DOMAIN_TEST_SKIP",
		"EMAIL_DOMAIN_TEST_USER_INTERNAL_USERNAME",
		"EMAIL_DOMAIN_TEST_USER_GITHUB_USERID",
		"EMAIL_DOMAIN_TEST_TEAM_NAME",
	}

	for _, key := range keys {
		if val, ok := os.LookupEnv(key); ok {
			env[key] = val
		}
		if val, ok := os.LookupEnv("TEST_" + key); ok {
			env[key] = val
		}
	}

	for k, v := range env {
		// Some CI environments escape multi-line secrets; keep behavior but don't be too clever.
		if strings.Contains(v, "\\") {
			env[k] = strings.ReplaceAll(v, "\\", "")
		}
	}
	return env
}

func mustParseInt64(key string, def int64) int64 {
	raw := strings.TrimSpace(TEST_ENV[key])
	if raw == "" {
		return def
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		panic(fmt.Errorf("invalid %s=%q: %w", key, raw, err))
	}
	return n
}

func initSharedResources() {
	if TEST_ENV == nil {
		TEST_ENV = map[string]string{}
	}

	GITHUB_INTEGRATION_ID = mustParseInt64("GITHUB_INTEGRATION_ID", 0)
	GITHUB_INSTALLATION_ID = mustParseInt64("GITHUB_INSTALLATION_ID", 0)

	ns := strings.TrimSpace(TEST_ENV["NAMESPACE"])
	if ns == "" {
		ns = "default"
	}

	TestOperatorNamespace = strings.TrimSpace(TEST_ENV["OPERATOR_NAMESPACE"])
	if TestOperatorNamespace == "" {
		TestOperatorNamespace = ns
	}

	githubCom = &repoguardsapv1.Github{
		ObjectMeta: metav1.ObjectMeta{
			Name: TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
		},
		Spec: repoguardsapv1.GithubSpec{
			WebURL:          TEST_ENV["GITHUB_WEB_URL"],
			V3APIURL:        TEST_ENV["GITHUB_V3_API_URL"],
			IntegrationID:   GITHUB_INTEGRATION_ID,
			ClientUserAgent: "greenhouse-repo-guard",
			Secret:          TEST_ENV["GITHUB_KUBERNETES_SECRET_RESOURCE_NAME"],
		},
	}

	githubComSecret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["GITHUB_KUBERNETES_SECRET_RESOURCE_NAME"],
			Namespace: TestOperatorNamespace,
		},
		StringData: map[string]string{
			"clientID":     TEST_ENV["GITHUB_CLIENT_ID"],
			"clientSecret": TEST_ENV["GITHUB_CLIENT_SECRET"],
			"privateKey":   TEST_ENV["GITHUB_PRIVATE_KEY"],
		},
	}

	ldapGroupProvider = &repoguardsapv1.LDAPGroupProvider{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["LDAP_GROUP_PROVIDER_KUBERNETES_RESOURCE_NAME"],
			Namespace: ns,
		},
		Spec: repoguardsapv1.LDAPGroupProviderSpec{
			Host:   TEST_ENV["LDAP_GROUP_PROVIDER_HOST"],
			BaseDN: TEST_ENV["LDAP_GROUP_PROVIDER_BASE_DN"],
			Secret: TEST_ENV["LDAP_GROUP_PROVIDER_KUBERNETES_SECRET_RESOURCE_NAME"],
		},
	}

	ldapGroupProviderSecret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["LDAP_GROUP_PROVIDER_KUBERNETES_SECRET_RESOURCE_NAME"],
			Namespace: ns,
		},
		StringData: map[string]string{
			"bindDN": TEST_ENV["LDAP_GROUP_PROVIDER_BIND_DN"],
			"bindPW": TEST_ENV["LDAP_GROUP_PROVIDER_BIND_PW"],
		},
	}

	githubTeamTestWithExternalMemberProviderLDAP = &repoguardsapv1.GithubTeam{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["LDAP_GROUP_PROVIDER_TEAM_KUBERNETES_RESOURCE_NAME"],
			Namespace: ns,
			Labels: map[string]string{
				"repoguard.sap/addUser":                       "true",
				"repoguard.sap/removeUser":                    "true",
				GITHUB_TEAMS_LABEL_DISABLE_INTERNAL_USERNAMES: GITHUB_TEAMS_LABEL_DISABLE_INTERNAL_USERNAMES_VALUE,
			},
		},
		Spec: repoguardsapv1.GithubTeamSpec{
			Github:       TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization: TEST_ENV["ORGANIZATION"],
			Team:         TEST_ENV["LDAP_GROUP_PROVIDER_TEAM_NAME"],
			ExternalMemberProvider: &repoguardsapv1.ExternalMemberProviderConfig{
				LDAPGroupDepreceated: &repoguardsapv1.LDAPGroup{
					LDAPGroupProvider: TEST_ENV["LDAP_GROUP_PROVIDER_KUBERNETES_RESOURCE_NAME"],
					Group:             TEST_ENV["LDAP_GROUP_PROVIDER_GROUP_NAME"],
				},
			},
		},
	}

	githubAccountLinkForExternalMemberProviderLDAP = &repoguardsapv1.GithubAccountLink{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"], strings.ToLower(TEST_ENV["LDAP_GROUP_PROVIDER_USER_INTERNAL_USERNAME"])),
			Namespace: ns,
		},
		Spec: repoguardsapv1.GithubAccountLinkSpec{
			GreenhouseUserID: TEST_ENV["LDAP_GROUP_PROVIDER_USER_INTERNAL_USERNAME"],
			GithubUserID:     TEST_ENV["LDAP_GROUP_PROVIDER_USER_GITHUB_USERID"],
			Github:           TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
		},
	}

	empHTTP = &repoguardsapv1.GenericExternalMemberProvider{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["EMP_HTTP_KUBERNETES_RESOURCE_NAME"],
			Namespace: ns,
		},
		Spec: repoguardsapv1.GenericExternalMemberProviderSpec{
			Endpoint:          TEST_ENV["EMP_HTTP_ENDPOINT"],
			Secret:            TEST_ENV["EMP_HTTP_KUBERNETES_SECRET_RESOURCE_NAME"],
			ResultsField:      "results",
			IDField:           "id",
			Paginated:         true,
			TotalPagesField:   "total_pages",
			PageParam:         "page",
			TestConnectionURL: TEST_ENV["EMP_HTTP_TEST_CONNECTION_URL"],
		},
	}

	empHTTPSecret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["EMP_HTTP_KUBERNETES_SECRET_RESOURCE_NAME"],
			Namespace: ns,
		},
		StringData: map[string]string{
			"username": TEST_ENV["EMP_HTTP_USERNAME"],
			"password": TEST_ENV["EMP_HTTP_PASSWORD"],
		},
	}

	githubTeamTestWithExternalMemberProviderHTTP = &repoguardsapv1.GithubTeam{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["EMP_HTTP_TEAM_KUBERNETES_RESOURCE_NAME"],
			Namespace: ns,
			Labels: map[string]string{
				"repoguard.sap/addUser":    "true",
				"repoguard.sap/removeUser": "true",
			},
		},
		Spec: repoguardsapv1.GithubTeamSpec{
			Github:       TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization: TEST_ENV["ORGANIZATION"],
			Team:         TEST_ENV["EMP_HTTP_TEAM_NAME"],
			ExternalMemberProvider: &repoguardsapv1.ExternalMemberProviderConfig{
				GenericHTTP: &repoguardsapv1.GenericProvider{
					ExternalMemberProvider: TEST_ENV["EMP_HTTP_KUBERNETES_RESOURCE_NAME"],
					Group:                  TEST_ENV["EMP_HTTP_GROUP_ID"],
				},
			},
		},
	}

	empStatic = &repoguardsapv1.StaticMemberProvider{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["EMP_STATIC_KUBERNETES_RESOURCE_NAME"],
			Namespace: ns,
		},
		Spec: repoguardsapv1.StaticMemberProviderSpec{
			Groups: []repoguardsapv1.StaticGroup{
				{Group: "any", Members: []string{TEST_ENV["EMP_STATIC_USER_INTERNAL_USERNAME"]}},
			},
		},
	}

	githubTeamTestWithExternalMemberProviderStatic = &repoguardsapv1.GithubTeam{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["EMP_STATIC_TEAM_KUBERNETES_RESOURCE_NAME"],
			Namespace: ns,
			Labels: map[string]string{
				"repoguard.sap/addUser":    "true",
				"repoguard.sap/removeUser": "true",
			},
		},
		Spec: repoguardsapv1.GithubTeamSpec{
			Github:       TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization: TEST_ENV["ORGANIZATION"],
			Team:         TEST_ENV["EMP_STATIC_TEAM_NAME"],
			ExternalMemberProvider: &repoguardsapv1.ExternalMemberProviderConfig{
				Static: &repoguardsapv1.GenericProvider{
					ExternalMemberProvider: TEST_ENV["EMP_STATIC_KUBERNETES_RESOURCE_NAME"],
					Group:                  "any",
				},
			},
		},
	}

	githubOrganizationGreenhouseSandboxForOrganizationTests = &repoguardsapv1.GithubOrganization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["ORGANIZATION_KUBERNETES_RESOURCE_NAME"],
			Namespace: ns,
			Labels: map[string]string{
				"repoguard.sap/addOrganizationOwner":    "true",
				"repoguard.sap/removeOrganizationOwner": "true",
				"repoguard.sap/addRepositoryTeam":       "false",
				"repoguard.sap/removeRepositoryTeam":    "false",
				"repoguard.sap/addTeam":                 "true",
				"repoguard.sap/removeTeam":              "true",
			},
		},
		Spec: repoguardsapv1.GithubOrganizationSpec{
			Github:                 TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization:           TEST_ENV["ORGANIZATION"],
			OrganizationOwnerTeams: []string{TEST_ENV["ORGANIZATION_OWNER_TEAM"]},
			InstallationID:         GITHUB_INSTALLATION_ID,
		},
	}

	githubTeamOwnerTest = &repoguardsapv1.GithubTeam{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["ORGANIZATION_OWNER_TEAM_KUBERNETES_RESOURCE_NAME"],
			Namespace: ns,
			Labels: map[string]string{
				"repoguard.sap/addUser":                       "true",
				"repoguard.sap/removeUser":                    "true",
				GITHUB_TEAMS_LABEL_DISABLE_INTERNAL_USERNAMES: GITHUB_TEAMS_LABEL_DISABLE_INTERNAL_USERNAMES_VALUE,
			},
		},
		Spec: repoguardsapv1.GithubTeamSpec{
			Github:         TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization:   TEST_ENV["ORGANIZATION"],
			Team:           TEST_ENV["ORGANIZATION_OWNER_TEAM"],
			GreenhouseTeam: TEST_ENV["ORGANIZATION_OWNER_TEAM"],
		},
	}

	greenhouseTeamOwnerTest = &greenhousesapv1alpha1.Team{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["ORGANIZATION_OWNER_TEAM"],
			Namespace: ns,
		},
		Spec: greenhousesapv1alpha1.TeamSpec{},
		Status: greenhousesapv1alpha1.TeamStatus{
			Members: []greenhousesapv1alpha1.User{{
				ID:        TEST_ENV["ORGANIZATION_OWNER_GREENHOUSE_ID"],
				Email:     fmt.Sprintf("%s@example.com", strings.ToLower(TEST_ENV["ORGANIZATION_OWNER_GREENHOUSE_ID"])),
				FirstName: "Owner",
				LastName:  "User",
			}},
		},
	}

	githubOrganizationGreenhouseSandboxForTTLTests = &repoguardsapv1.GithubOrganization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["ORGANIZATION_KUBERNETES_RESOURCE_NAME"],
			Namespace: ns,
			Labels: map[string]string{
				"repoguard.sap/addOrganizationOwner":    "false",
				"repoguard.sap/removeOrganizationOwner": "false",
				"repoguard.sap/addRepositoryTeam":       "false",
				"repoguard.sap/removeRepositoryTeam":    "false",
				"repoguard.sap/addTeam":                 "false",
				"repoguard.sap/removeTeam":              "false",
			},
		},
		Spec: repoguardsapv1.GithubOrganizationSpec{
			Github:         TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization:   TEST_ENV["ORGANIZATION"],
			InstallationID: GITHUB_INSTALLATION_ID,
		},
		Status: repoguardsapv1.GithubOrganizationStatus{
			OrganizationStatus:          repoguardsapv1.GithubOrganizationStateFailed,
			OrganizationStatusError:     "some failure",
			OrganizationStatusTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Second)),
			Operations: repoguardsapv1.GithubOrganizationStatusOperations{
				RepositoryTeamOperations: []repoguardsapv1.GithubRepoTeamOperation{
					{Repo: "r1", State: repoguardsapv1.GithubRepoTeamOperationStateFailed, Timestamp: metav1.Now()},
					{Repo: "r1", State: repoguardsapv1.GithubRepoTeamOperationStateComplete, Timestamp: metav1.Now()},
				},
				GithubTeamOperations:        []repoguardsapv1.GithubTeamOperation{{Team: "t1", State: repoguardsapv1.GithubTeamOperationStateComplete, Timestamp: metav1.Now()}},
				OrganizationOwnerOperations: []repoguardsapv1.GithubUserOperation{{User: "u1", State: repoguardsapv1.GithubUserOperationStateComplete, Timestamp: metav1.Now()}},
			},
		},
	}

	githubOrganizationGreenhouseSandboxForTeamTests = &repoguardsapv1.GithubOrganization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["ORGANIZATION_KUBERNETES_RESOURCE_NAME"],
			Namespace: ns,
			Labels: map[string]string{
				"repoguard.sap/addOrganizationOwner":    "false",
				"repoguard.sap/removeOrganizationOwner": "false",
				"repoguard.sap/addRepositoryTeam":       "false",
				"repoguard.sap/removeRepositoryTeam":    "false",
				"repoguard.sap/addTeam":                 "true",
				"repoguard.sap/removeTeam":              "true",
			},
		},
		Spec: repoguardsapv1.GithubOrganizationSpec{
			Github:         TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization:   TEST_ENV["ORGANIZATION"],
			InstallationID: GITHUB_INSTALLATION_ID,
		},
	}

	githubTeamTest = &repoguardsapv1.GithubTeam{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["TEAM_1_KUBERNETES_RESOURCE_NAME"],
			Namespace: ns,
			Labels: map[string]string{
				"repoguard.sap/addUser":    "true",
				"repoguard.sap/removeUser": "true",
			},
		},
		Spec: repoguardsapv1.GithubTeamSpec{
			Github:         TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization:   TEST_ENV["ORGANIZATION"],
			Team:           TEST_ENV["TEAM_1"],
			GreenhouseTeam: TEST_ENV["TEAM_1"],
		},
	}

	greenhouseTeamTest = &greenhousesapv1alpha1.Team{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["TEAM_1"],
			Namespace: ns,
		},
		Status: greenhousesapv1alpha1.TeamStatus{
			Members: []greenhousesapv1alpha1.User{{
				ID:        TEST_ENV["USER_1"],
				Email:     fmt.Sprintf("%s@example.com", strings.ToLower(TEST_ENV["USER_1"])),
				FirstName: "User1",
				LastName:  "Test",
			}},
		},
	}

	githubTeamTestWithExternalUsername = &repoguardsapv1.GithubTeam{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["TEAM_2_KUBERNETES_RESOURCE_NAME"],
			Namespace: ns,
			Labels: map[string]string{
				"repoguard.sap/addUser":                       "true",
				"repoguard.sap/removeUser":                    "true",
				GITHUB_TEAMS_LABEL_DISABLE_INTERNAL_USERNAMES: GITHUB_TEAMS_LABEL_DISABLE_INTERNAL_USERNAMES_VALUE,
			},
		},
		Spec: repoguardsapv1.GithubTeamSpec{
			Github:         TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization:   TEST_ENV["ORGANIZATION"],
			Team:           TEST_ENV["TEAM_2"],
			GreenhouseTeam: TEST_ENV["TEAM_2"],
		},
	}

	greenhouseTeamTestWithExternalUsername = &greenhousesapv1alpha1.Team{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["TEAM_2"],
			Namespace: ns,
		},
		Status: greenhousesapv1alpha1.TeamStatus{
			Members: []greenhousesapv1alpha1.User{{
				ID:        TEST_ENV["USER_1_GREENHOUSE_ID"],
				Email:     fmt.Sprintf("%s@example.com", strings.ToLower(TEST_ENV["USER_1_GREENHOUSE_ID"])),
				FirstName: "User1",
				LastName:  "Test",
			}},
		},
	}

	githubAccountLinkOrgOwner = &repoguardsapv1.GithubAccountLink{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"], strings.ToLower(TEST_ENV["USER_0_GREENHOUSE_ID"])),
			Namespace: ns,
		},
		Spec: repoguardsapv1.GithubAccountLinkSpec{
			GreenhouseUserID: TEST_ENV["USER_0_GREENHOUSE_ID"],
			GithubUserID:     TEST_ENV["USER_0_GITHUB_USERID"],
			Github:           TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
		},
	}

	githubAccountLink = &repoguardsapv1.GithubAccountLink{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"], strings.ToLower(TEST_ENV["USER_1_GREENHOUSE_ID"])),
			Namespace: ns,
		},
		Spec: repoguardsapv1.GithubAccountLinkSpec{
			GreenhouseUserID: TEST_ENV["USER_1_GREENHOUSE_ID"],
			GithubUserID:     TEST_ENV["USER_1_GITHUB_USERID"],
			Github:           TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
		},
	}

	githubOrganizationGreenhouseSandboxForRepositoryTests = &repoguardsapv1.GithubOrganization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["ORGANIZATION_KUBERNETES_RESOURCE_NAME"],
			Namespace: ns,
			Labels: map[string]string{
				"repoguard.sap/addOrganizationOwner":    "false",
				"repoguard.sap/removeOrganizationOwner": "false",
				"repoguard.sap/addRepositoryTeam":       "true",
				"repoguard.sap/removeRepositoryTeam":    "true",
				"repoguard.sap/addTeam":                 "false",
				"repoguard.sap/removeTeam":              "false",
			},
		},
		Spec: repoguardsapv1.GithubOrganizationSpec{
			Github:       TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization: TEST_ENV["ORGANIZATION"],
			DefaultPublicRepositoryTeams: []repoguardsapv1.GithubTeamWithPermission{
				{Team: TEST_DEFAULT_PUBLIC_REPO_PULL, Permission: repoguardsapv1.GithubTeamPermissionPull},
				{Team: TEST_DEFAULT_PUBLIC_REPO_PUSH, Permission: repoguardsapv1.GithubTeamPermissionPush},
				{Team: TEST_DEFAULT_PUBLIC_REPO_ADMIN, Permission: repoguardsapv1.GithubTeamPermissionAdmin},
			},
			DefaultPrivateRepositoryTeams: []repoguardsapv1.GithubTeamWithPermission{
				{Team: TEST_DEFAULT_PRIVATE_REPO_PULL, Permission: repoguardsapv1.GithubTeamPermissionPull},
				{Team: TEST_DEFAULT_PRIVATE_REPO_PUSH, Permission: repoguardsapv1.GithubTeamPermissionPush},
				{Team: TEST_DEFAULT_PRIVATE_REPO_ADMIN, Permission: repoguardsapv1.GithubTeamPermissionAdmin},
			},
			InstallationID: GITHUB_INSTALLATION_ID,
		},
	}

	githubTeamRepository = &repoguardsapv1.GithubTeamRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "custom-team-for-private-repository",
			Namespace: ns,
		},
		Spec: repoguardsapv1.GithubTeamRepositorySpec{
			Github:       TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization: TEST_ENV["ORGANIZATION"],
			Team:         TEST_CUSTOM_TEAM,
			Repository:   []string{"private-repo"},
			Permission:   repoguardsapv1.GithubTeamPermissionPush,
		},
	}
}
