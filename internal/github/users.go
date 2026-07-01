// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	gogithub "github.com/google/go-github/v88/github"
	"github.com/palantir/go-githubapp/githubapp"
	githubv4 "github.com/shurcooL/githubv4"

	ghmetrics "github.com/cloudoperators/repo-guard/internal/metrics"
)

type UsersProvider interface {
	GithubUsernameByID(id string) (string, bool, error)
	GithubIDByUsername(username string) (string, bool, error)
	// IsMemberOfOrg checks whether the GitHub user with the given UID is a member of the organization.
	IsMemberOfOrg(ctx context.Context, org string, uid string) (bool, error)
	// HasVerifiedEmailDomainForGithubUID checks whether the GitHub user with the given UID
	// has an email address visible to the given organization that matches the provided domain.
	// This uses the organization members endpoint which exposes members' verified emails to
	// organization owners, per GitHub changelog 2019-03-25.
	HasVerifiedEmailDomainForGithubUID(ctx context.Context, org string, uid string, domain string) (bool, error)
}

type DefaultUsersProvider struct {
	service    gogithub.UsersService // ETag-wrapped: used by GithubUsernameByID / GithubIDByUsername
	rawService gogithub.UsersService // plain client: used by IsMemberOfOrg / HasVerifiedEmailDomainForGithubUID
	orgs       gogithub.OrganizationsService
	http       *gogithub.Client // underlying go-github client to reuse its http.Client for GraphQL
	githubName string
	cache      *etagCache
}

func NewUsersProvider(cc githubapp.ClientCreator, githubName string, installationID int64) (UsersProvider, error) {
	client, err := cc.NewInstallationClient(installationID)
	if err != nil {
		return nil, err
	}
	if client.Users == nil {
		return nil, errors.New("empty users service")
	}
	if client.Organizations == nil {
		return nil, errors.New("empty organizations service")
	}

	// Users lookups are not org-specific, but we still key the cache by GitHub instance
	// so that separate GitHub instances (github.com vs GHE) never share ETags or user data.
	cache := getOrCreateOrgCache(githubName, "__users__")

	// Clone the client with an ETag transport for conditional GET caching.
	baseHTTP := client.Client()
	baseTransport := baseHTTP.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	etagHTTP := &http.Client{
		Transport:     &etagTransport{wrapped: baseTransport, cache: cache, keyFn: urlCacheKey},
		CheckRedirect: baseHTTP.CheckRedirect,
		Jar:           baseHTTP.Jar,
		Timeout:       baseHTTP.Timeout,
	}
	etagClient, err := client.Clone(gogithub.WithHTTPClient(etagHTTP))
	if err != nil {
		return nil, fmt.Errorf("clone github client with etag transport: %w", err)
	}

	return &DefaultUsersProvider{
		service:    *etagClient.Users,
		rawService: *client.Users,
		orgs:       *client.Organizations,
		http:       client,
		githubName: githubName,
		cache:      cache,
	}, nil
}

// GithubUsernameByID returns the GitHub login for a given GitHub user ID.
// It returns (username, found, error).
func (u *DefaultUsersProvider) GithubUsernameByID(id string) (string, bool, error) {
	// parse the string ID to int64
	userID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return "", false, fmt.Errorf("invalid GitHub user ID: %q (expected numeric ID): %w", id, err)
	}

	userKey := fmt.Sprintf("/user/%s", id)

	// fetch the user by ID
	user, resp, err := u.service.GetByID(context.Background(), userID)
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return "", false, nil
		}
		if resp != nil && resp.StatusCode == http.StatusNotModified {
			ghmetrics.EtagCacheHitsTotal.WithLabelValues(u.githubName, "__users__", "user-by-id").Inc()
			if cached, ok := u.cache.getValue(userKey); ok {
				if v, ok := cached.(string); ok {
					return v, v != "", nil
				}
			}
			u.cache.invalidate(userKey)
			return "", false, fmt.Errorf("etag cache inconsistency for %s: 304 received but no valid cached value", userKey)
		}
		return "", false, err
	}

	login := user.GetLogin()
	if etag, ok := u.cache.getEtag(userKey); ok && etag != "" {
		ghmetrics.EtagCacheMissesTotal.WithLabelValues(u.githubName, "__users__", "user-by-id").Inc()
		u.cache.set(userKey, etag, login)
	}

	return login, true, nil
}

// GithubIDByUsername returns the GitHub user ID string for a given login.
// It returns (id, found, error).
func (u *DefaultUsersProvider) GithubIDByUsername(username string) (string, bool, error) {
	loginKey := fmt.Sprintf("/users/%s", username)

	// fetch the user by login name
	user, resp, err := u.service.Get(context.Background(), username)
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return "", false, nil
		}
		if resp != nil && resp.StatusCode == http.StatusNotModified {
			ghmetrics.EtagCacheHitsTotal.WithLabelValues(u.githubName, "__users__", "user-by-login").Inc()
			if cached, ok := u.cache.getValue(loginKey); ok {
				if v, ok := cached.(string); ok {
					return v, v != "", nil
				}
			}
			u.cache.invalidate(loginKey)
			return "", false, fmt.Errorf("etag cache inconsistency for %s: 304 received but no valid cached value", loginKey)
		}
		return "", false, err
	}

	// convert numeric ID to string
	idStr := strconv.FormatInt(user.GetID(), 10)
	if etag, ok := u.cache.getEtag(loginKey); ok && etag != "" {
		ghmetrics.EtagCacheMissesTotal.WithLabelValues(u.githubName, "__users__", "user-by-login").Inc()
		u.cache.set(loginKey, etag, idStr)
	}

	return idStr, true, nil
}

func (u *DefaultUsersProvider) IsMemberOfOrg(ctx context.Context, org string, uid string) (bool, error) {
	userID, err := strconv.ParseInt(uid, 10, 64)
	if err != nil {
		return false, fmt.Errorf("invalid GitHub user ID: %q (expected numeric ID): %w", uid, err)
	}
	user, resp, err := u.rawService.GetByID(ctx, userID)
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return false, nil
		}
		return false, err
	}
	login := user.GetLogin()
	if login == "" {
		return false, nil
	}

	isMember, resp, err := u.orgs.IsMember(ctx, org, login)
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return false, nil
		}
		return false, err
	}
	return isMember, nil
}

// HasVerifiedEmailDomainForGithubUID implements UsersProvider.HasVerifiedEmailDomainForGithubUID.
// It uses the GitHub GraphQL API to query User.organizationVerifiedDomainEmails and
// checks whether any email has the requested domain. This requires appropriate
// permissions and works with an installation-scoped client when the app is
// installed in an organization where the user is a member.
func (u *DefaultUsersProvider) HasVerifiedEmailDomainForGithubUID(ctx context.Context, org string, uid string, domain string) (bool, error) {
	if domain == "" {
		return false, nil
	}

	// Resolve login by numeric ID (GraphQL query requires login)
	userID, err := strconv.ParseInt(uid, 10, 64)
	if err != nil {
		return false, fmt.Errorf("invalid GitHub user ID: %q (expected numeric ID): %w", uid, err)
	}
	user, resp, err := u.rawService.GetByID(ctx, userID)
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return false, nil
		}
		return false, err
	}
	login := user.GetLogin()
	if login == "" {
		return false, nil
	}

	if u.http == nil {
		return false, errors.New("missing underlying http client for GraphQL")
	}

	v4 := githubv4.NewClient(u.http.Client())

	// GraphQL query: organizationVerifiedDomainEmails requires the organization login argument
	var q struct {
		User struct {
			Emails []githubv4.String `graphql:"organizationVerifiedDomainEmails(login: $org)"`
		} `graphql:"user(login: $login)"`
	}
	vars := map[string]any{
		"login": githubv4.String(login),
		"org":   githubv4.String(org),
	}
	if err := v4.Query(ctx, &q, vars); err != nil {
		return false, err
	}

	return slices.ContainsFunc(q.User.Emails, func(e githubv4.String) bool {
		email := string(e)
		at := strings.LastIndexByte(email, '@')
		return at > 0 && at < len(email)-1 && strings.EqualFold(email[at+1:], domain)
	}), nil
}
