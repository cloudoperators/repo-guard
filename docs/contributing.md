# Contributing

## Code of Conduct

All members of the project community must abide by the [SAP Open Source Code of Conduct](https://github.com/SAP/.github/blob/main/CODE_OF_CONDUCT.md). Only by respecting each other can we develop a productive, collaborative community.

## Engaging in Our Project

We use GitHub to manage reviews of pull requests.

- If you are a new contributor, see [Steps to Contribute](#steps-to-contribute).
- Before implementing your change, create an issue describing the problem or enhancement. Note that you are willing to work on it.
- The team will review the issue and decide whether it should be implemented as a pull request. If approved, they will assign the issue to you.

## Steps to Contribute

Claim an issue first by commenting that you want to work on it — this prevents duplicated effort from multiple contributors.

If you have questions about an issue, comment on it and a maintainer will clarify.

## Contributing Code or Documentation

Contributions must be licensed under the [Apache 2.0 License](https://github.com/cloudoperators/repo-guard/blob/main/LICENSE).

Due to legal reasons, contributors must accept a Developer Certificate of Origin (DCO) when they create their first pull request. This is handled automatically during submission. SAP uses [the standard DCO text of the Linux Foundation](https://developercertificate.org/).

## Commit Guidelines

- Sign off all commits: `git commit -s`
- Use `type(scope): short description` style (e.g. `feat(controller): add dry-run support`)
- Do not add `Co-authored-by` trailers unless explicitly requested.

## Development Setup

### Prerequisites

- Go 1.26+
- `make`
- Access to a Kubernetes cluster (or `envtest` for unit tests)

### Running Tests

```bash
# Download required Kubernetes binaries for envtest
make setup-envtest

# Create test.env with credentials (see below)
touch internal/controller/test.env

# Run controller integration tests
make controller-test
```

**`internal/controller/test.env` fields:**

| Variable | Description |
|---|---|
| `GITHUB_TOKEN` | PAT with org-admin scope |
| `GITHUB_INTEGRATION_ID` | GitHub App ID |
| `GITHUB_CLIENT_ID` | GitHub App client ID |
| `GITHUB_CLIENT_SECRET` | GitHub App client secret |
| `GITHUB_PRIVATE_KEY` | GitHub App private key (PEM) |
| `ORGANIZATION` | Target GitHub organization for testing |

### Code Quality

```bash
make fmt        # Format code
make vet        # Static analysis
make lint       # golangci-lint
make build      # Compile manager binary
```

### CRD Changes

After modifying any `_types.go` file:

```bash
make manifests generate
make verify-manifests
```

### E2E Tests

```bash
make e2e            # Full flow: up + install + test (requires k3d)
make e2e-down       # Tear down k3d cluster
make e2e-github-cleanup  # Remove e2e-created teams via PAT
```

## Issues and Planning

We follow a shared issue lifecycle across all `cloudoperators` repositories. See the [Issue Lifecycle documentation](https://github.com/cloudoperators/common/blob/main/ISSUE_LIFECYCLE.md) for the full process.

Quick links:

- [Issues needing triage](https://github.com/issues?q=org%3Acloudoperators+label%3Aneeds-triage+is%3Aopen+sort%3Acreated-asc)
- [Backlog (ready to pick up)](https://github.com/cloudoperators/repo-guard/issues?q=is%3Aopen+is%3Aissue+label%3Abacklog)
- [Project board](https://github.com/orgs/cloudoperators/projects/9)
