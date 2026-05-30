// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"net/http"

	gogithub "github.com/google/go-github/v85/github"
	githubv4 "github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// FakeClientCreator implements githubapp.ClientCreator by returning go-github
// clients pointing at a local httptest server URL.  All requests are handled
// by the mock server instead of real GitHub endpoints.
type FakeClientCreator struct {
	serverURL string
}

// NewFakeClientCreator returns a FakeClientCreator whose clients will send all
// requests to serverURL (typically an httptest.Server.URL).
func NewFakeClientCreator(serverURL string) *FakeClientCreator {
	return &FakeClientCreator{serverURL: serverURL}
}

func (f *FakeClientCreator) newClient() (*gogithub.Client, error) {
	httpClient := &http.Client{}
	c, err := gogithub.NewClient(httpClient).WithEnterpriseURLs(
		f.serverURL+"/api/v3/",
		f.serverURL+"/", // server root avoids go-github doubling the path to …/api/v3/api/uploads
	)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (f *FakeClientCreator) newV4Client() *githubv4.Client {
	return githubv4.NewEnterpriseClient(f.serverURL+"/api/graphql", &http.Client{})
}

// NewAppClient returns a go-github client aimed at the mock server.
func (f *FakeClientCreator) NewAppClient() (*gogithub.Client, error) {
	return f.newClient()
}

// NewAppV4Client returns a v4 client aimed at the mock server.
func (f *FakeClientCreator) NewAppV4Client() (*githubv4.Client, error) {
	return f.newV4Client(), nil
}

// NewInstallationClient returns a go-github client aimed at the mock server.
// The installationID is ignored in mock mode.
func (f *FakeClientCreator) NewInstallationClient(_ int64) (*gogithub.Client, error) {
	return f.newClient()
}

// NewInstallationV4Client returns a v4 client aimed at the mock server.
func (f *FakeClientCreator) NewInstallationV4Client(_ int64) (*githubv4.Client, error) {
	return f.newV4Client(), nil
}

// NewTokenSourceClient returns a go-github client aimed at the mock server.
func (f *FakeClientCreator) NewTokenSourceClient(_ oauth2.TokenSource) (*gogithub.Client, error) {
	return f.newClient()
}

// NewTokenSourceV4Client returns a v4 client aimed at the mock server.
func (f *FakeClientCreator) NewTokenSourceV4Client(_ oauth2.TokenSource) (*githubv4.Client, error) {
	return f.newV4Client(), nil
}

// NewTokenClient returns a go-github client aimed at the mock server.
func (f *FakeClientCreator) NewTokenClient(_ string) (*gogithub.Client, error) {
	return f.newClient()
}

// NewTokenV4Client returns a v4 client aimed at the mock server.
func (f *FakeClientCreator) NewTokenV4Client(_ string) (*githubv4.Client, error) {
	return f.newV4Client(), nil
}
