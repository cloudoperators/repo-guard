// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
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

// mockTestPrivateKey is a throwaway 2048-bit RSA key used only in mock mode.
// go-githubapp needs a valid PKCS1 key to build JWT tokens; the mock server
// ignores the token, so the key never leaves the test process.
const mockTestPrivateKey = `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEApD48diBEjGxojS67iuPAztDh8L23r1x0karLw0m/eG6jy4bL
W8maUpiamcvTyaIWLagh24Pmm5id2GUBIr/EF0JQCZ6HL3pEX0BYq4gDV+X9HLOr
x4ya8d2EYwlREA8a/JA4e9UHXwIIiSCkqsmWQFlkKiMhvnyajL4z6CV13fOzBF5c
wHHJzh7xyFRqdu0l2IHH6tAEFypGTJk3DS0kwuat3NhrkX0SxdA0hLFgYtt9OXQT
9E8BtQFLW1ugane4hZh6/TNrtSN+q+/OLm81TzcBRsFJvlyWpOA+jo7nHkSf0lGV
qhDlI4hcVRYAMaGssqeO2ykx5aOSRmnKy+fHjQIDAQABAoIBAE3xTwYL6BvvsmoV
nGCcFrrO+/ogPlRU/ujF8e7aR6gicU67yDPl53t8+hk0VmxgpD/Eg1TGMqDyey3f
OPvBn5AeIxd9iM/qKRo+0hWM9XE4Lrb5OPL48esH4bSSDksdsAPdeUCi5t2afGx+
9kYqZkhhY5xvkarxPPK/rKhlZpsOMYrfEMVim+bpXPXybilAXzLvhKC7wTPtFB7e
K+N28a19Y2F+Pxs/54M4uP4vRaEbBURIoPM6DwjhiuVRki0r0bPPu9TGh+UJg0w8
ealTi2yGf27qcZ6hFH0y4vS9gW2ca67JWXRRVf8kB1s9418YoGt4BZa15GE6JiTr
QN0eA00CgYEA2m+gIrolWixMA84A2wYoPtV1ZY84+j9OALo2lkXQXiZLbU9/O4xQ
pC9DXcGWmAxYC1aR+nslTHOwq2fdJrAKnxlpVjWYaTRlEJvfVHEcMjgr2BqJQePF
MyX2uo10vILK/DTRnBD5sowG95O0sVf9u+zm4QaEburZnqQSudW1qasCgYEAwHzV
o6cJBRDtWD+4iDltFFr13fsVbgUlfQJ79STQKQd44SlEiea+GBW6rU1Whsf77Czm
QR29B1qlXglV/5Y93HknK6gmo53c13Urrj+QyiVK4zIML8h13J/d8xA4HlmcJnts
Gk/NI3V4Elu/NHxQhVRIAiuR4N/igrvai7YTS6cCgYA5prZ8E+ch2okhg/Bj3kcm
9k2qxVdDbQvYU01u8fQhtTe5HP82pzztaG/+QcbOUIu4Slvy4Seh+vLI+nu650GW
Zi2QDEsykRqPfKQ/9C597qdbvP02/7efXUi2Sflie565W/NqnmxYvG5mT3ykRdX5
EHiLMZ4obCGNpj4vAAGXSwKBgQCX5asdouG+SrZRjq9LaK3Ig2NEkjA+CvejZ8N0
F2HhDtF4NX2tqRXXocYXnlEquUP3AxOMzS/vTrvyskFYGTKl+kFL9TzQnvG4YPFg
Zy8WJkmrckIlrvY7bTjT57exU5uizoDnXpQOaFOhgR8pBvTv0iuk8scCgwqXijCT
UdJ2PwKBgAnxUDZQFymrUnRKomCsSGTV3PtJVV6mvI4YvaBDNHd6QvSkkiY9nd3q
GDylTe+GHV3lXLv+1mfAxV6IcbztzwfiMTzgiUTu6IxIORGx3HdncJoeHbe9OoqA
YpefVRgFqssFeu6Y1ko7R4v/mXVWjlrsBmBuyyMf1IMwVn75mTYJ
-----END RSA PRIVATE KEY-----`

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
	// Provide the throwaway private key so go-githubapp can build valid JWTs.
	// The mock server accepts requests without validating the JWT signature.
	TEST_ENV["GITHUB_PRIVATE_KEY"] = mockTestPrivateKey
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
