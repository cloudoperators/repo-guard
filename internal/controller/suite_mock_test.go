// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http/httptest"
	"os"

	internalgithub "github.com/cloudoperators/repo-guard/internal/github"
	. "github.com/onsi/ginkgo/v2"
)

// mockGitHubServer is the running mock server for the suite; non-nil in mock mode.
var mockGitHubServer *httptest.Server

// isMockMode returns true when GITHUB_MOCK is "true" (the default for CI and
// local runs without real credentials).
func isMockMode() bool {
	v := os.Getenv("GITHUB_MOCK")
	return v == "" || v == "true"
}

// mockTestOrg is the GitHub organisation name used in mock mode.
// It must match TEST_ENV["ORGANIZATION"] so existing resource fixtures work.
const mockTestOrg = "greenhouse-sandbox"

// generateMockPrivateKey creates a throwaway RSA key at runtime for mock mode.
// go-githubapp needs a valid PKCS1 key to build JWT tokens; the mock server
// ignores the token, so the key never leaves the test process.
func generateMockPrivateKey() string {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic("suite_mock_test: failed to generate RSA key: " + err.Error())
	}
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	return string(pem.EncodeToMemory(block))
}

// startMockGitHubServer starts the mock GitHub HTTP server and overrides
// TEST_ENV values so that githubCom / githubComSecret fixtures point at the
// mock instead of the real GitHub API.
//
// Must be called after loadTestEnv() and before initSharedResources().
func startMockGitHubServer() {
	By("starting mock GitHub HTTP server")

	cfg := internalgithub.MockConfig{
		Org: mockTestOrg,
		Members: []internalgithub.MockUser{
			{Login: TEST_ENV["USER_0_GITHUB_USERNAME"], ID: mustParseInt64("USER_0_GITHUB_USERID", 1)},
			{Login: TEST_ENV["USER_1_GITHUB_USERNAME"], ID: mustParseInt64("USER_1_GITHUB_USERID", 2)},
			{Login: TEST_ENV["USER_2_GITHUB_USERNAME"], ID: mustParseInt64("USER_2_GITHUB_USERID", 3)},
			{Login: TEST_ENV["LDAP_GROUP_PROVIDER_USER_GITHUB_USERNAME"], ID: mustParseInt64("LDAP_GROUP_PROVIDER_USER_GITHUB_USERID", 4)},
		},
		Owners: []internalgithub.MockUser{
			{Login: TEST_ENV["USER_0_GITHUB_USERNAME"], ID: mustParseInt64("USER_0_GITHUB_USERID", 1)},
		},
		Teams: []internalgithub.MockTeam{
			{ID: 1, Name: TEST_ENV["TEAM_1"], Slug: TEST_ENV["TEAM_1"]},
			{ID: 2, Name: TEST_ENV["TEAM_2"], Slug: TEST_ENV["TEAM_2"]},
			{ID: 3, Name: TEST_ENV["ORGANIZATION_OWNER_TEAM"], Slug: TEST_ENV["ORGANIZATION_OWNER_TEAM"]},
			{ID: 4, Name: TEST_ENV["LDAP_GROUP_PROVIDER_TEAM_NAME"], Slug: TEST_ENV["LDAP_GROUP_PROVIDER_TEAM_NAME"]},
			{ID: 5, Name: TEST_ENV["EMP_HTTP_TEAM_NAME"], Slug: TEST_ENV["EMP_HTTP_TEAM_NAME"]},
			{ID: 6, Name: TEST_ENV["EMP_STATIC_TEAM_NAME"], Slug: TEST_ENV["EMP_STATIC_TEAM_NAME"]},
		},
		Repos: []internalgithub.MockRepo{
			{Name: "public-repo", Private: false},
			{Name: "private-repo", Private: true},
		},
	}

	mockGitHubServer, _ = internalgithub.NewMockGitHubServer(GinkgoT(), cfg)

	// Override V3 API URL so the GithubReconciler points go-githubapp at the mock.
	TEST_ENV["GITHUB_V3_API_URL"] = mockGitHubServer.URL + "/api/v3"
	// Generate a throwaway private key so go-githubapp can build valid JWTs.
	// The mock server accepts requests without validating the JWT signature.
	TEST_ENV["GITHUB_PRIVATE_KEY"] = generateMockPrivateKey()
	// Provide dummy values for fields required by the secret spec.
	if TEST_ENV["GITHUB_CLIENT_ID"] == "" {
		TEST_ENV["GITHUB_CLIENT_ID"] = "mock-client-id"
	}
	if TEST_ENV["GITHUB_CLIENT_SECRET"] == "" {
		TEST_ENV["GITHUB_CLIENT_SECRET"] = "mock-client-secret"
	}
	// Any non-zero integration ID is fine; JWTs are sent to the mock and ignored.
	if TEST_ENV["GITHUB_INTEGRATION_ID"] == "" {
		TEST_ENV["GITHUB_INTEGRATION_ID"] = "1"
	}
	// Token used by tests that construct a direct githubAPI client (e.g. repository tests).
	if TEST_ENV["GITHUB_TOKEN"] == "" {
		TEST_ENV["GITHUB_TOKEN"] = "mock-token"
	}
}
