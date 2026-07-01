# AI Agent Guide for Repo Guard

This document provides context and guidelines for AI agents working on the `repo-guard` project.

## Project Overview

`repo-guard` is a Kubernetes operator designed to automate GitHub organization management. It uses Custom Resource Definitions (CRDs) to manage GitHub teams, memberships, repository permissions, and organization ownership.

### Core Mission
- Automate GitHub governance via K8s GitOps.
- Sync GitHub memberships from external sources (LDAP, HTTP APIs, Static lists, etc.).
- Ensure consistency between the desired state in K8s and the actual state in GitHub.

## Tech Stack

- **Language:** Go (1.26+)
- **Framework:** Operator SDK / controller-runtime
- **Testing:** Ginkgo, Gomega, Testify (for assertions)
- **GitHub API:** `google/go-github` and `palantir/go-githubapp`
- **Infrastructure:** Kubernetes, Helm (for deployment)
- **Metrics:** Prometheus (via controller-runtime)

### Modern Go Idioms (Required)
- Use `slices` and `maps` standard library packages for collection manipulations (e.g., `slices.Contains`, `slices.SortFunc`).
- Use `omitzero` instead of `omitempty` in JSON tags for `time.Time`, `time.Duration`, structs, slices, and maps.
- Use `t.Context()` in tests when a context is needed.
- Use `errors.AsType[T](err)` for type-safe error checking.
- Use `new(val)` for pointer-to-value initialization.

## Domain Model (CRDs)

### Connection & Identity
- **`Github` (Cluster):** Configuration for a GitHub App installation.
- **`GithubAccountLink` (Cluster):** Maps internal user IDs to GitHub User IDs. Handles email verification.

### Management
- **`GithubOrganization` (Namespaced):** Managed GitHub organization. Defines global policies.
- **`GithubTeam` (Namespaced):** Managed GitHub team linked to a membership provider.
- **`GithubTeamRepository` (Namespaced):** Granular team permissions on specific repositories.

### Member Providers (Identity Sources)
- **`LDAPGroupProvider`** / **`ClusterLDAPGroupProvider`**
- **`GenericExternalMemberProvider`** / **`ClusterGenericExternalMemberProvider`**
- **`StaticMemberProvider`** / **`ClusterStaticMemberProvider`**

## Development Workflow

### Standard Commands
- `make manifests`: Update CRD manifests and RBAC (run after changing `_types.go`).
- `make verify-manifests`: Verify Helm CRDs are in sync with `config/crd/bases`.
- `make generate`: Update generated code (DeepCopy, etc.).
- `make fmt` / `make vet`: Format code and run static analysis.
- `make lint`: Run `golangci-lint`.
- `make build`: Compile the manager binary.

### E2E Commands
- `make e2e`: Full e2e flow (up + install + test).
- `make e2e-install`: Renders Helm values from `test.env` and installs the chart.
- `make e2e-github-cleanup`: Cleanup e2e-created teams using PAT from `test.env`.

### Commit Guidelines
- **DCO:** Ensure all commits have signed-off (`git commit -s`).
- **Style:** Use `type(scope): short description` style (e.g., `feat(controller): add dry-run support`).
- **Note:** Do NOT add `Co-authored-by` trailers unless explicitly requested.

## Reconciliation Design Patterns

### Status & Conditions
- **State Machine:** Controllers use an `orgStatus` / `teamStatus` field to track progress.
- **Operations:** Pending operations (Add/Remove) are tracked in the status before execution.
- **Optimization:** Use fields like `OutOfPolicyRepositories` in `GithubOrganizationStatus` to store only the diff, avoiding large status payloads in etcd.

### Rate Limiting & Backoff
- **GitHub API:** The controller handles rate limits by extracting the reset timestamp from error messages (see `internal/controller/utils.go`).
- **Requeue:** When rate-limited, the controller requeues the resource with a `RequeueAfter` duration based on the reset timestamp.

### Safety Rails
- **Dry Run:** Respect the `dryRun` label on resources. If set, log actions but don't call the GitHub API for mutations.
- **Action Labels:** Mutations are often guarded by specific labels (e.g., `repo-guard.cloudoperators.dev/addUser: "true"`).

## Local Development & Testing

### Environment Setup
1. **Envtest:** Run `make setup-envtest` to download required Kubernetes binaries.
2. **Secrets:** Create `internal/controller/test.env` (git-ignored) with required credentials.

### `test.env` Requirements
- `GITHUB_TOKEN`: PAT with org-admin scope.
- `GITHUB_INTEGRATION_ID`, `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `GITHUB_PRIVATE_KEY`: GitHub App credentials.
- `ORGANIZATION`: The target GitHub organization for testing.

### Test Execution
- **Controller Tests:** `make controller-test` (runs integration tests using `envtest`).
- **External Dependencies:** Controller tests start dummy external services (LDAP and HTTP) to simulate membership providers. See `internal/controller/suite_test.go`.
- **E2E Tests:** `make e2e` (requires k3d). Always run `make e2e-down e2e-github-cleanup` afterwards.

## Observability

- **Metrics:** Defined in `internal/metrics/metrics.go`.
- **Usage:** Call `ghmetrics.StartReconcileTimer` at the start of `Reconcile` and use `defer` to record the result.
- **Custom Gauges:** Use `SetGithubOrganizationMetrics` to update resource-specific metrics.

## Agent Checklist (Definition of Done)

- [ ] CRD changes? Ran `make manifests generate`.
- [ ] Code style? Followed Modern Go idioms and ran `make fmt`.
- [ ] Linting? `make lint` passes.
- [ ] Testing? Added/updated unit tests and verified with `make controller-test`.
- [ ] Commits? All commits are signed-off (`-s`).
- [ ] Docs? If you changed any of the items below, update the corresponding doc page **in the same PR** or the `docs-lint` CI job will fail:
  - `internal/metrics/metrics.go` (added/renamed metric) → update `docs/operations/metrics.md`
  - `charts/repo-guard/templates/prometheusrules.yaml` (added/renamed alert) → update `docs/operations/metrics.md`
  - `api/v1/githuborganization_types.go` `GithubOrganizationSpec` (added/renamed field) → update `docs/crds/github-organization.md`

## Key Directories

- `api/v1/`: CRD Type definitions.
- `internal/controller/`: Controller implementations.
- `internal/github/`: GitHub API client logic.
- `internal/external-provider/`: Member source implementations.
- `config/`: K8s manifests (CRDs, RBAC).
- `charts/repo-guard/`: Helm chart.
