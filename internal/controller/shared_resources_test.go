package controller

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	greenhousesapv1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	githubguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	"github.com/joho/godotenv"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	TEST_ENV map[string]string
)

var (

	// github
	githubCom       = &githubguardsapv1.Github{}
	githubComSecret = &v1.Secret{}

	// organization owner tests
	githubTeamOwnerTest                                     = &githubguardsapv1.GithubTeam{}
	greenhouseTeamOwnerTest                                 = &greenhousesapv1alpha1.Team{}
	githubOrganizationGreenhouseSandboxForOrganizationTests = &githubguardsapv1.GithubOrganization{}
	githubAccountLinkOrgOwner                               = &githubguardsapv1.GithubAccountLink{}

	// team membership tests
	githubOrganizationGreenhouseSandboxForTeamTests = &githubguardsapv1.GithubOrganization{}
	githubTeamTest                                  = &githubguardsapv1.GithubTeam{}
	greenhouseTeamTest                              = &greenhousesapv1alpha1.Team{}

	// external username tests
	githubTeamTestWithExternalUsername     = &githubguardsapv1.GithubTeam{}
	greenhouseTeamTestWithExternalUsername = &greenhousesapv1alpha1.Team{}
	githubAccountLink                      = &githubguardsapv1.GithubAccountLink{}

	// repository tests
	githubOrganizationGreenhouseSandboxForRepositoryTests = &githubguardsapv1.GithubOrganization{}
	githubTeamRepository                                  = &githubguardsapv1.GithubTeamRepository{}

	// LDAP test resources
	ldapGroupProvider                              = &githubguardsapv1.LDAPGroupProvider{}
	ldapGroupProviderSecret                        = &v1.Secret{}
	githubTeamTestWithExternalMemberProviderLDAP   = &githubguardsapv1.GithubTeam{}
	githubAccountLinkForExternalMemberProviderLDAP = &githubguardsapv1.GithubAccountLink{}

	// External Member Provider (Generic HTTP) test resources
	empHTTP                                      = &githubguardsapv1.GenericExternalMemberProvider{}
	empHTTPSecret                                = &v1.Secret{}
	githubTeamTestWithExternalMemberProviderHTTP = &githubguardsapv1.GithubTeam{}

	// External Member Provider (Static) test resources
	empStatic                                      = &githubguardsapv1.StaticMemberProvider{}
	githubTeamTestWithExternalMemberProviderStatic = &githubguardsapv1.GithubTeam{}

	// ttl tests
	githubOrganizationGreenhouseSandboxForTTLTests = &githubguardsapv1.GithubOrganization{}
)

func init() {
	var err error
	TEST_ENV, err = godotenv.Read("test.env")
	if err != nil {
		TEST_ENV = make(map[string]string)
	}

	// Always check for environment variable overrides, including TEST_ prefixed ones from secrets
	for _, key := range []string{
		"GITHUB_TOKEN", "GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET", "GITHUB_PRIVATE_KEY",
		"ORGANIZATION", "NAMESPACE", "GITHUB_KUBERNETES_RESOURCE_NAME", "GITHUB_KUBERNETES_SECRET_RESOURCE_NAME",
		"GITHUB_WEB_URL", "GITHUB_V3_API_URL", "GITHUB_INTEGRATION_ID",
		"LDAP_GROUP_PROVIDER_HOST", "LDAP_GROUP_PROVIDER_BASE_DN", "LDAP_GROUP_PROVIDER_BIND_DN", "LDAP_GROUP_PROVIDER_BIND_PW",
		"LDAP_GROUP_PROVIDER_KUBERNETES_RESOURCE_NAME", "LDAP_GROUP_PROVIDER_KUBERNETES_SECRET_RESOURCE_NAME",
		"LDAP_GROUP_PROVIDER_TEAM_NAME", "LDAP_GROUP_PROVIDER_TEAM_KUBERNETES_RESOURCE_NAME",
		"LDAP_GROUP_PROVIDER_GROUP_NAME", "LDAP_GROUP_PROVIDER_USER_INTERNAL_USERNAME", "LDAP_GROUP_PROVIDER_USER_GITHUB_USERID",
		"EMP_HTTP_USERNAME", "EMP_HTTP_PASSWORD", "EMP_HTTP_GROUP_ID", "EMP_HTTP_USER_INTERNAL_USERNAME",
		"EMP_HTTP_KUBERNETES_RESOURCE_NAME", "EMP_HTTP_KUBERNETES_SECRET_RESOURCE_NAME", "EMP_HTTP_TEAM_NAME", "EMP_HTTP_TEAM_KUBERNETES_RESOURCE_NAME",
		"EMP_STATIC_KUBERNETES_RESOURCE_NAME", "EMP_STATIC_TEAM_NAME", "EMP_STATIC_TEAM_KUBERNETES_RESOURCE_NAME", "EMP_STATIC_USER_INTERNAL_USERNAME",
	} {
		if val, ok := os.LookupEnv(key); ok {
			TEST_ENV[key] = val
		}
		// Also support TEST_ prefix from GitHub Secrets
		if val, ok := os.LookupEnv("TEST_" + key); ok {
			TEST_ENV[key] = val
		}
	}

	for k, v := range TEST_ENV {
		if strings.ContainsAny(v, "\\") {
			TEST_ENV[k] = strings.ReplaceAll(v, "\\", "")
		}
	}

	// For mandatory fields that don't have good defaults, we might still need to check them later,
	// but let's avoid panicking here if they are missing but not used by the current test suite.
	integrationIDStr := TEST_ENV["GITHUB_INTEGRATION_ID"]
	if integrationIDStr == "" {
		integrationIDStr = "0"
	}
	GITHUB_INTEGRATION_ID, err := strconv.ParseInt(integrationIDStr, 10, 64)
	if err != nil {
		panic(err)
	}

	githubCom = &githubguardsapv1.Github{

		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Namespace: TEST_ENV["NAMESPACE"],
		},
		Spec: githubguardsapv1.GithubSpec{
			WebURL:          TEST_ENV["GITHUB_WEB_URL"],
			V3APIURL:        TEST_ENV["GITHUB_V3_API_URL"],
			IntegrationID:   GITHUB_INTEGRATION_ID,
			ClientUserAgent: "greenhouse-github-guard",
			Secret:          TEST_ENV["GITHUB_KUBERNETES_SECRET_RESOURCE_NAME"],
		},
	}

	githubComSecret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["GITHUB_KUBERNETES_SECRET_RESOURCE_NAME"],
			Namespace: TEST_ENV["NAMESPACE"],
		},
		StringData: map[string]string{
			"clientID":     TEST_ENV["GITHUB_CLIENT_ID"],
			"clientSecret": TEST_ENV["GITHUB_CLIENT_SECRET"],
			"privateKey":   TEST_ENV["GITHUB_PRIVATE_KEY"],
		},
	}

	ldapGroupProvider = &githubguardsapv1.LDAPGroupProvider{

		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["LDAP_GROUP_PROVIDER_KUBERNETES_RESOURCE_NAME"],
			Namespace: TEST_ENV["NAMESPACE"],
		},
		Spec: githubguardsapv1.LDAPGroupProviderSpec{
			Host:   TEST_ENV["LDAP_GROUP_PROVIDER_HOST"],
			BaseDN: TEST_ENV["LDAP_GROUP_PROVIDER_BASE_DN"],
			Secret: TEST_ENV["LDAP_GROUP_PROVIDER_KUBERNETES_SECRET_RESOURCE_NAME"],
		},
	}

	ldapGroupProviderSecret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["LDAP_GROUP_PROVIDER_KUBERNETES_SECRET_RESOURCE_NAME"],
			Namespace: TEST_ENV["NAMESPACE"],
		},
		StringData: map[string]string{
			"bindDN": TEST_ENV["LDAP_GROUP_PROVIDER_BIND_DN"],
			"bindPW": TEST_ENV["LDAP_GROUP_PROVIDER_BIND_PW"],
		},
	}

	githubTeamTestWithExternalMemberProviderLDAP = &githubguardsapv1.GithubTeam{

		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["LDAP_GROUP_PROVIDER_TEAM_KUBERNETES_RESOURCE_NAME"],
			Namespace: TEST_ENV["NAMESPACE"],
			Labels: map[string]string{
				"githubguard.sap/addUser":                     "true",
				"githubguard.sap/removeUser":                  "true",
				GITHUB_TEAMS_LABEL_DISABLE_INTERNAL_USERNAMES: GITHUB_TEAMS_LABEL_DISABLE_INTERNAL_USERNAMES_VALUE,
			},
		},
		Spec: githubguardsapv1.GithubTeamSpec{
			Github:       TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization: TEST_ENV["ORGANIZATION"],
			Team:         TEST_ENV["LDAP_GROUP_PROVIDER_TEAM_NAME"],
			ExternalMemberProvider: &githubguardsapv1.ExternalMemberProviderConfig{
				LDAPGroupDepreceated: &githubguardsapv1.LDAPGroup{
					LDAPGroupProvider: TEST_ENV["LDAP_GROUP_PROVIDER_KUBERNETES_RESOURCE_NAME"],
					Group:             TEST_ENV["LDAP_GROUP_PROVIDER_GROUP_NAME"],
				},
			},
		},
	}

	githubAccountLinkForExternalMemberProviderLDAP = &githubguardsapv1.GithubAccountLink{

		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"], strings.ToLower(TEST_ENV["LDAP_GROUP_PROVIDER_USER_INTERNAL_USERNAME"])),
			Namespace: TEST_ENV["NAMESPACE"],
		},
		Spec: githubguardsapv1.GithubAccountLinkSpec{
			GreenhouseUserID: TEST_ENV["LDAP_GROUP_PROVIDER_USER_INTERNAL_USERNAME"],
			GithubUserID:     TEST_ENV["LDAP_GROUP_PROVIDER_USER_GITHUB_USERID"],
			Github:           TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
		},
	}

	// External Member Provider (Generic HTTP)
	empHTTP = &githubguardsapv1.GenericExternalMemberProvider{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["EMP_HTTP_KUBERNETES_RESOURCE_NAME"],
			Namespace: TEST_ENV["NAMESPACE"],
		},
		Spec: githubguardsapv1.GenericExternalMemberProviderSpec{
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
			Namespace: TEST_ENV["NAMESPACE"],
		},
		StringData: map[string]string{
			"username": TEST_ENV["EMP_HTTP_USERNAME"],
			"password": TEST_ENV["EMP_HTTP_PASSWORD"],
		},
	}

	githubTeamTestWithExternalMemberProviderHTTP = &githubguardsapv1.GithubTeam{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["EMP_HTTP_TEAM_KUBERNETES_RESOURCE_NAME"],
			Namespace: TEST_ENV["NAMESPACE"],
			Labels: map[string]string{
				"githubguard.sap/addUser":    "true",
				"githubguard.sap/removeUser": "true",
			},
		},
		Spec: githubguardsapv1.GithubTeamSpec{
			Github:       TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization: TEST_ENV["ORGANIZATION"],
			Team:         TEST_ENV["EMP_HTTP_TEAM_NAME"],
			ExternalMemberProvider: &githubguardsapv1.ExternalMemberProviderConfig{
				GenericHTTP: &githubguardsapv1.GenericProvider{
					ExternalMemberProvider: TEST_ENV["EMP_HTTP_KUBERNETES_RESOURCE_NAME"],
					Group:                  TEST_ENV["EMP_HTTP_GROUP_ID"],
				},
			},
		},
	}

	// External Member Provider (Static)
	empStatic = &githubguardsapv1.StaticMemberProvider{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["EMP_STATIC_KUBERNETES_RESOURCE_NAME"],
			Namespace: TEST_ENV["NAMESPACE"],
		},
		Spec: githubguardsapv1.StaticMemberProviderSpec{
			Groups: []githubguardsapv1.StaticGroup{
				{Group: "any", Members: []string{TEST_ENV["EMP_STATIC_USER_INTERNAL_USERNAME"]}},
			},
		},
	}

	githubTeamTestWithExternalMemberProviderStatic = &githubguardsapv1.GithubTeam{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["EMP_STATIC_TEAM_KUBERNETES_RESOURCE_NAME"],
			Namespace: TEST_ENV["NAMESPACE"],
			Labels: map[string]string{
				"githubguard.sap/addUser":    "true",
				"githubguard.sap/removeUser": "true",
			},
		},
		Spec: githubguardsapv1.GithubTeamSpec{
			Github:       TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization: TEST_ENV["ORGANIZATION"],
			Team:         TEST_ENV["EMP_STATIC_TEAM_NAME"],
			ExternalMemberProvider: &githubguardsapv1.ExternalMemberProviderConfig{
				Static: &githubguardsapv1.GenericProvider{
					ExternalMemberProvider: TEST_ENV["EMP_STATIC_KUBERNETES_RESOURCE_NAME"],
					Group:                  "any",
				},
			},
		},
	}

	GITHUB_INSTALLATION_ID, err := strconv.ParseInt(TEST_ENV["GITHUB_INSTALLATION_ID"], 10, 64)
	if err != nil {
		panic(err)
	}

	githubOrganizationGreenhouseSandboxForOrganizationTests = &githubguardsapv1.GithubOrganization{

		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["ORGANIZATION_KUBERNETES_RESOURCE_NAME"],
			Namespace: TEST_ENV["NAMESPACE"],
			Labels: map[string]string{
				"githubguard.sap/addOrganizationOwner":    "true",
				"githubguard.sap/removeOrganizationOwner": "true",
				"githubguard.sap/addRepositoryTeam":       "false",
				"githubguard.sap/removeRepositoryTeam":    "false",
				"githubguard.sap/addTeam":                 "true",
				"githubguard.sap/removeTeam":              "true",
			},
		},
		Spec: githubguardsapv1.GithubOrganizationSpec{
			Github:                 TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization:           TEST_ENV["ORGANIZATION"],
			OrganizationOwnerTeams: []string{TEST_ENV["ORGANIZATION_OWNER_TEAM"]},
			//			DefaultPublicRepositoryTeams  []GithubTeamWithPermission `json:"defaultPublicRepositoryTeams,omitempty"`
			//			DefaultPrivateRepositoryTeams []GithubTeamWithPermission `json:"defaultPrivateRepositoryTeams,omitempty"`

			InstallationID: GITHUB_INSTALLATION_ID,
		},
	}

	githubTeamOwnerTest = &githubguardsapv1.GithubTeam{

		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["ORGANIZATION_OWNER_TEAM_KUBERNETES_RESOURCE_NAME"],
			Namespace: TEST_ENV["NAMESPACE"],
			Labels: map[string]string{
				"githubguard.sap/addUser":                     "true",
				"githubguard.sap/removeUser":                  "true",
				GITHUB_TEAMS_LABEL_DISABLE_INTERNAL_USERNAMES: GITHUB_TEAMS_LABEL_DISABLE_INTERNAL_USERNAMES_VALUE,
			},
		},
		Spec: githubguardsapv1.GithubTeamSpec{
			Github:         TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization:   TEST_ENV["ORGANIZATION"],
			Team:           TEST_ENV["ORGANIZATION_OWNER_TEAM"],
			GreenhouseTeam: TEST_ENV["ORGANIZATION_OWNER_TEAM"],
		},
	}

	greenhouseTeamOwnerTest = &greenhousesapv1alpha1.Team{

		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["ORGANIZATION_OWNER_TEAM"],
			Namespace: TEST_ENV["NAMESPACE"],
		},
		Spec: greenhousesapv1alpha1.TeamSpec{},
		Status: greenhousesapv1alpha1.TeamStatus{
			Members: []greenhousesapv1alpha1.User{
				{
					ID:        TEST_ENV["ORGANIZATION_OWNER_GREENHOUSE_ID"],
					Email:     fmt.Sprintf("%s@example.com", strings.ToLower(TEST_ENV["ORGANIZATION_OWNER_GREENHOUSE_ID"])),
					FirstName: "Owner",
					LastName:  "User",
				},
			},
		},
	}

	githubOrganizationGreenhouseSandboxForTTLTests = &githubguardsapv1.GithubOrganization{

		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["ORGANIZATION_KUBERNETES_RESOURCE_NAME"],
			Namespace: TEST_ENV["NAMESPACE"],
			Labels: map[string]string{
				"githubguard.sap/addOrganizationOwner":    "false",
				"githubguard.sap/removeOrganizationOwner": "false",
				"githubguard.sap/addRepositoryTeam":       "false",
				"githubguard.sap/removeRepositoryTeam":    "false",
				"githubguard.sap/addTeam":                 "false",
				"githubguard.sap/removeTeam":              "false",
			},
		},
		Spec: githubguardsapv1.GithubOrganizationSpec{

			Github:         TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization:   TEST_ENV["ORGANIZATION"],
			InstallationID: GITHUB_INSTALLATION_ID,
		},
		Status: githubguardsapv1.GithubOrganizationStatus{
			OrganizationStatus:          githubguardsapv1.GithubOrganizationStateFailed,
			OrganizationStatusError:     "some failure",
			OrganizationStatusTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Second)),
			Operations: githubguardsapv1.GithubOrganizationStatusOperations{
				RepositoryTeamOperations: []githubguardsapv1.GithubRepoTeamOperation{{Repo: "r1", State: githubguardsapv1.GithubRepoTeamOperationStateFailed, Timestamp: metav1.Now()},
					{Repo: "r1", State: githubguardsapv1.GithubRepoTeamOperationStateComplete, Timestamp: metav1.Now()}},
				GithubTeamOperations:        []githubguardsapv1.GithubTeamOperation{{Team: "t1", State: githubguardsapv1.GithubTeamOperationStateComplete, Timestamp: metav1.Now()}},
				OrganizationOwnerOperations: []githubguardsapv1.GithubUserOperation{{User: "u1", State: githubguardsapv1.GithubUserOperationStateComplete, Timestamp: metav1.Now()}},
			},
		},
	}

	githubOrganizationGreenhouseSandboxForTeamTests = &githubguardsapv1.GithubOrganization{

		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["ORGANIZATION_KUBERNETES_RESOURCE_NAME"],
			Namespace: TEST_ENV["NAMESPACE"],
			Labels: map[string]string{
				"githubguard.sap/addOrganizationOwner":    "false",
				"githubguard.sap/removeOrganizationOwner": "false",
				"githubguard.sap/addRepositoryTeam":       "false",
				"githubguard.sap/removeRepositoryTeam":    "false",
				"githubguard.sap/addTeam":                 "true",
				"githubguard.sap/removeTeam":              "true",
			},
		},
		Spec: githubguardsapv1.GithubOrganizationSpec{

			Github:       TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization: TEST_ENV["ORGANIZATION"],
			//			OrganizationOwnerTeams        []string                   `json:"organizationOwnerTeams,omitempty"`
			//			DefaultPublicRepositoryTeams  []GithubTeamWithPermission `json:"defaultPublicRepositoryTeams,omitempty"`
			//			DefaultPrivateRepositoryTeams []GithubTeamWithPermission `json:"defaultPrivateRepositoryTeams,omitempty"`

			InstallationID: GITHUB_INSTALLATION_ID,
		},
	}

	githubTeamTest = &githubguardsapv1.GithubTeam{

		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["TEAM_1_KUBERNETES_RESOURCE_NAME"],
			Namespace: TEST_ENV["NAMESPACE"],
			Labels: map[string]string{
				"githubguard.sap/addUser":    "true",
				"githubguard.sap/removeUser": "true",
			},
		},
		Spec: githubguardsapv1.GithubTeamSpec{

			Github:         TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization:   TEST_ENV["ORGANIZATION"],
			Team:           TEST_ENV["TEAM_1"],
			GreenhouseTeam: TEST_ENV["TEAM_1"],
		},
	}

	greenhouseTeamTest = &greenhousesapv1alpha1.Team{

		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["TEAM_1"],
			Namespace: TEST_ENV["NAMESPACE"],
		},
		Status: greenhousesapv1alpha1.TeamStatus{
			Members: []greenhousesapv1alpha1.User{
				{
					ID:        TEST_ENV["USER_1"],
					Email:     fmt.Sprintf("%s@example.com", strings.ToLower(TEST_ENV["USER_1"])),
					FirstName: "User1",
					LastName:  "Test",
				},
			},
		},
	}

	githubTeamTestWithExternalUsername = &githubguardsapv1.GithubTeam{

		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["TEAM_2_KUBERNETES_RESOURCE_NAME"],
			Namespace: TEST_ENV["NAMESPACE"],
			Labels: map[string]string{
				"githubguard.sap/addUser":                     "true",
				"githubguard.sap/removeUser":                  "true",
				GITHUB_TEAMS_LABEL_DISABLE_INTERNAL_USERNAMES: GITHUB_TEAMS_LABEL_DISABLE_INTERNAL_USERNAMES_VALUE,
			},
		},
		Spec: githubguardsapv1.GithubTeamSpec{

			Github:         TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization:   TEST_ENV["ORGANIZATION"],
			Team:           TEST_ENV["TEAM_2"],
			GreenhouseTeam: TEST_ENV["TEAM_2"],
		},
	}

	greenhouseTeamTestWithExternalUsername = &greenhousesapv1alpha1.Team{

		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["TEAM_2"],
			Namespace: TEST_ENV["NAMESPACE"],
		},
		Status: greenhousesapv1alpha1.TeamStatus{
			Members: []greenhousesapv1alpha1.User{
				{
					ID:        TEST_ENV["USER_1_GREENHOUSE_ID"],
					Email:     fmt.Sprintf("%s@example.com", strings.ToLower(TEST_ENV["USER_1_GREENHOUSE_ID"])),
					FirstName: "User1",
					LastName:  "Test",
				},
			},
		},
	}

	githubAccountLinkOrgOwner = &githubguardsapv1.GithubAccountLink{

		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"], strings.ToLower(TEST_ENV["USER_0_GREENHOUSE_ID"])),
			Namespace: TEST_ENV["NAMESPACE"],
		},
		Spec: githubguardsapv1.GithubAccountLinkSpec{
			GreenhouseUserID: TEST_ENV["USER_0_GREENHOUSE_ID"],
			GithubUserID:     TEST_ENV["USER_0_GITHUB_USERID"],
			Github:           TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
		},
	}
	githubAccountLink = &githubguardsapv1.GithubAccountLink{

		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"], strings.ToLower(TEST_ENV["USER_1_GREENHOUSE_ID"])),
			Namespace: TEST_ENV["NAMESPACE"],
		},
		Spec: githubguardsapv1.GithubAccountLinkSpec{
			GreenhouseUserID: TEST_ENV["USER_1_GREENHOUSE_ID"],
			GithubUserID:     TEST_ENV["USER_1_GITHUB_USERID"],
			Github:           TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
		},
	}

	githubOrganizationGreenhouseSandboxForRepositoryTests = &githubguardsapv1.GithubOrganization{

		ObjectMeta: metav1.ObjectMeta{
			Name:      TEST_ENV["ORGANIZATION_KUBERNETES_RESOURCE_NAME"],
			Namespace: TEST_ENV["NAMESPACE"],
			Labels: map[string]string{
				"githubguard.sap/addOrganizationOwner":    "false",
				"githubguard.sap/removeOrganizationOwner": "false",
				"githubguard.sap/addRepositoryTeam":       "true",
				"githubguard.sap/removeRepositoryTeam":    "true",
				"githubguard.sap/addTeam":                 "false",
				"githubguard.sap/removeTeam":              "false",
			},
		},
		Spec: githubguardsapv1.GithubOrganizationSpec{

			Github:       TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization: TEST_ENV["ORGANIZATION"],
			DefaultPublicRepositoryTeams: []githubguardsapv1.GithubTeamWithPermission{
				{
					Team:       TEST_DEFAULT_PUBLIC_REPO_PULL,
					Permission: githubguardsapv1.GithubTeamPermissionPull,
				},
				{
					Team:       TEST_DEFAULT_PUBLIC_REPO_PUSH,
					Permission: githubguardsapv1.GithubTeamPermissionPush,
				},
				{
					Team:       TEST_DEFAULT_PUBLIC_REPO_ADMIN,
					Permission: githubguardsapv1.GithubTeamPermissionAdmin,
				},
			},
			DefaultPrivateRepositoryTeams: []githubguardsapv1.GithubTeamWithPermission{
				{
					Team:       TEST_DEFAULT_PRIVATE_REPO_PULL,
					Permission: githubguardsapv1.GithubTeamPermissionPull,
				},
				{
					Team:       TEST_DEFAULT_PRIVATE_REPO_PUSH,
					Permission: githubguardsapv1.GithubTeamPermissionPush,
				},
				{
					Team:       TEST_DEFAULT_PRIVATE_REPO_ADMIN,
					Permission: githubguardsapv1.GithubTeamPermissionAdmin,
				},
			},

			InstallationID: 43715277,
		},
	}

	githubTeamRepository = &githubguardsapv1.GithubTeamRepository{

		ObjectMeta: metav1.ObjectMeta{
			Name:      "custom-team-for-private-repository",
			Namespace: TEST_ENV["NAMESPACE"],
		},
		Spec: githubguardsapv1.GithubTeamRepositorySpec{
			Github:       TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
			Organization: TEST_ENV["ORGANIZATION"],
			Team:         TEST_CUSTOM_TEAM,
			Repository:   []string{TEST_REPOSITORY_PRIVATE},
			Permission:   githubguardsapv1.GithubTeamPermissionPush,
		}}
}
