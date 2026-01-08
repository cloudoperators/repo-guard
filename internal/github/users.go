package github

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/google/go-github/v81/github"
	"github.com/palantir/go-githubapp/githubapp"
	githubv4 "github.com/shurcooL/githubv4"
)

type UsersProvider interface {
	GithubUsernameByID(id string) (string, bool, error)
	GithubIDByUsername(username string) (string, bool, error)
	// HasVerifiedEmailDomainForGithubUID checks whether the GitHub user with the given UID
	// has an email address visible to the given organization that matches the provided domain.
	// This uses the organization members endpoint which exposes members' verified emails to
	// organization owners, per GitHub changelog 2019-03-25.
	HasVerifiedEmailDomainForGithubUID(ctx context.Context, org string, uid string, domain string) (bool, error)
}

type DefaultUsersProvider struct {
	service github.UsersService
	orgs    github.OrganizationsService
	http    *github.Client // underlying go-github client to reuse its http.Client for GraphQL
}

func NewUsersProvider(cc githubapp.ClientCreator, installationID int64) (UsersProvider, error) {
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

	return &DefaultUsersProvider{service: *client.Users, orgs: *client.Organizations, http: client}, nil
}

// GithubUsernameByID returns the GitHub login for a given GitHub user ID.
// It returns (username, found, error).
func (u *DefaultUsersProvider) GithubUsernameByID(id string) (string, bool, error) {
	// parse the string ID to int64
	userID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return "", false, err
	}

	// fetch the user by ID
	user, resp, err := u.service.GetByID(context.Background(), userID)
	if err != nil {
		// if not found, return found=false
		if resp != nil && resp.StatusCode == 404 {
			return "", false, nil
		}
		return "", false, err
	}

	return user.GetLogin(), true, nil
}

// GithubIDByUsername returns the GitHub user ID string for a given login.
// It returns (id, found, error).
func (u *DefaultUsersProvider) GithubIDByUsername(username string) (string, bool, error) {
	// fetch the user by login name
	user, resp, err := u.service.Get(context.Background(), username)
	if err != nil {
		// if not found, return found=false
		if resp != nil && resp.StatusCode == 404 {
			return "", false, nil
		}
		return "", false, err
	}

	// convert numeric ID to string
	return strconv.FormatInt(user.GetID(), 10), true, nil
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
		return false, err
	}
	user, resp, err := u.service.GetByID(ctx, userID)
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
	vars := map[string]interface{}{
		"login": githubv4.String(login),
		"org":   githubv4.String(org),
	}
	if err := v4.Query(ctx, &q, vars); err != nil {
		return false, err
	}

	for _, e := range q.User.Emails {
		email := string(e)
		if email == "" {
			continue
		}
		at := strings.LastIndexByte(email, '@')
		if at <= 0 || at == len(email)-1 {
			continue
		}
		if strings.EqualFold(email[at+1:], domain) {
			return true, nil
		}
	}
	return false, nil
}
