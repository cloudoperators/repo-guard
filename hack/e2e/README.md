### Local End-to-End (k3d) Environment

This project includes a lightweight end-to-end environment using k3d and the Helm chart in `charts/repo-guard`.

Prerequisites:
- k3d, kubectl, helm
- Docker (or Podman) to build the controller image that will be imported into k3d
- jq and curl (required for the richer checks and GitHub API validations)
- openssl (used by `gen-values.sh` to generate a throwaway RSA key in mock mode)
- A populated `internal/controller/test.env` (only required for live mode — see below)

---

### Mock mode vs. live mode for controller tests

Controller tests (`make controller-test`) default to **mock mode** (`GITHUB_MOCK=true`).

In mock mode:
- An in-process `net/http/httptest` server handles all GitHub API requests.
- No real GitHub credentials are needed — the tests work on forks and in CI without
  secrets.
- The mock server is started in `BeforeSuite`; a throwaway RSA key is generated at
  runtime by `generateMockPrivateKey()` in `internal/controller/suite_mock_test.go`
  and used in place of the App private key. JWT signatures are sent to the mock and
  accepted without validation.

To extend mock responses (e.g. to add new users/teams/repos), edit `MockConfig` in
`suite_mock_test.go` and add endpoint handlers in `internal/github/mock_server.go`.

To run against real GitHub (live mode):

```sh
make controller-test-live          # or: make controller-test GITHUB_MOCK=false
```

Live mode requires a valid `internal/controller/test.env` with real credentials.

---

### Mock mode vs. live mode for e2e tests

E2E tests (`make e2e`) also default to **mock GitHub mode** (`USE_MOCK_GITHUB=true`).

In mock e2e mode:
- A standalone `hack/github-mock-server` binary is built, imported into k3d, and
  deployed as a Kubernetes `Deployment` + `Service` inside the cluster.
- The controller's Helm values are generated with `v3APIURL` pointing at the in-cluster
  mock service (`http://github-mock.<ns>.svc.cluster.local:8080/api/v3`) and a
  throwaway RSA key.  Real credentials are not required.
- All GitHub API checks in `e2e-test` are also served through the same mock (via a
  `kubectl port-forward` to localhost).
- This mode is used by default in CI (`ci.yaml`) so that PRs and pushes to `main` run
  the full e2e suite without GitHub secrets.

To run e2e against real GitHub (live mode):

```sh
make e2e-live                       # or: make e2e USE_MOCK_GITHUB=false
```

Live mode requires a valid `internal/controller/test.env` with real credentials. It is
only used by the nightly workflow (`nightly.yaml`).

To extend what the mock GitHub server knows about (org members, teams, repos), update
`deploy_incluster_mock_github()` in `hack/e2e/e2e.sh` to pass additional env vars to
the server container. The server reads `MOCK_GITHUB_MEMBERS`, `MOCK_GITHUB_OWNERS`,
`MOCK_GITHUB_TEAMS`, and `MOCK_GITHUB_REPOS` (comma-separated lists).

---

Quickstart (mock mode, no credentials needed):
- make e2e-up       # creates a k3d cluster, builds the repo image and imports it, ensures Prometheus CRDs
- make e2e-install  # deploys mock GitHub server, generates Helm values, installs Helm chart
- make e2e-test     # runs runtime checks + all scenarios (teams/providers/owners/repos); prints ✅/❌ summary at the end
- make e2e-down     # deletes the k3d cluster

Or all at once:

```sh
make e2e            # mock mode (default, no credentials needed)
make e2e-live       # live mode (requires credentials in test.env)
```

Other useful targets:
- make e2e-image    # build only the controller image used by e2e
- make e2e-install-dry-run  # render Helm manifests with your generated values (no apply)
  # Note: This will auto-start the in-repo dummy Generic HTTP server to ensure values render cleanly,
  # and shut it down right after the dry-run. When dummy mode is disabled, the generator emits an
  # empty provider list to avoid template errors.
- make e2e-install-crds     # install only repo-guard CRDs from the chart
- make e2e-github-cleanup   # remove e2e-created GitHub teams; supports optional repo cleanup (live mode only)

Cleanup flags (environment variables):
- E2E_DRY_RUN=true|false (default: true) — print planned deletions without performing them
- E2E_CLEANUP_REPOS=true|false (default: false) — also delete repositories
- E2E_REPO_PREFIX=<prefix> — when deleting repos, only remove those whose name starts with this prefix (required when E2E_CLEANUP_REPOS=true)

Requirements for cleanup:
- `GITHUB_TOKEN` in `internal/controller/test.env` with sufficient org admin permissions to delete teams and (if enabled) the `delete_repo` scope to delete repos

What the E2E does:
- Creates a k3d cluster and installs the Greenhouse Team CRD (`config/crd/external/greenhouse.sap_teams.yaml`)
- Installs Prometheus Operator CRDs (PodMonitor/PrometheusRule) for monitoring templates
- Builds the repo image locally and imports it into k3d; Helm is configured to use this image
- In mock mode: builds and deploys the in-cluster mock GitHub API server; configures Helm values to point at it
- Generates chart values from `internal/controller/test.env` (script: `hack/e2e/gen-values.sh`):
  - Handles multi-line `GITHUB_PRIVATE_KEY` (YAML block scalar)
  - Sanitizes LDAP and HTTP credentials (removes literal backslashes by default)
  - Emits `githubAccountLinks` for LDAP/HTTP/Static/Owner/User3 mappings
  - Sets per-team `disableInternalUsernames` (LDAP team only) with org-level default false
  - Adds `repo-guard.cloudoperators.dev/skippedTTL` label support via chart values
  - Creates Greenhouse-backed teams for `TEAM_1`, `TEAM_2`, and the owner team
- Pre-creates Greenhouse `Team` CRs for `TEAM_1`, `TEAM_2`, and the owner team during `e2e-install`
- Installs/Upgrades the Helm chart with `--wait` and ensures manager metrics are exposed
- `e2e-test` runs, in order:
  1) Readiness checks (controller running; org converge)
  2) Metrics probe (robust, retries; accepts controller_runtime metrics)
  3) Teams scenario (Greenhouse-driven):
     - Patches Greenhouse `.status.members` and waits for `GithubTeam.status.members` to converge (1 → 2 → 1)
     - Before each expectation, bumps a trigger label on the team to nudge reconciliation; will re-trigger again after every 10 unchanged observations
     - Toggles `repo-guard.cloudoperators.dev/addUser` / `repo-guard.cloudoperators.dev/removeUser` labels and verifies expected behavior
  4) Provider scenarios:
     - LDAP team with GithubAccountLink — verifies member on GitHub and K8s status
     - Generic HTTP provider team — verifies member on GitHub and K8s status
     - Static provider team — verifies member on GitHub and K8s status
  5) Owner scenario:
     - Owner team (Greenhouse-backed): add primary owner, verify GitHub org admins; then add a second member and verify both are admins
  6) Repository scenarios (run last):
     - Creates two repos (public/private) via GitHub API
     - Verifies default team permissions for public/private repos
     - Optionally verifies an explicit assignment for the private repo if configured
  7) Final summary line with emojis (✅ success or ❌ failure)

Key environment inputs (from `internal/controller/test.env`):
- GitHub App and PAT: `GITHUB_TOKEN`, `GITHUB_PRIVATE_KEY`, `GITHUB_*` (integration/installation/client IDs)
  - In mock mode these are replaced by dummy values automatically; `test.env` is only read for org/team/user names
- Organization and teams: `ORGANIZATION`, `TEAM_1`, `TEAM_2`, `ORGANIZATION_OWNER_TEAM`
- Owner mapping: `ORGANIZATION_OWNER_USER`, `ORGANIZATION_OWNER_GREENHOUSE_ID`, `ORGANIZATION_OWNER_GITHUB_USERID`
- Users for Greenhouse scenario: `USER_1`, `USER_2`, and optional `USER_3_*` used in owner/repo checks
- LDAP: host/baseDN/bindDN/bindPW and a test group; generator removes backslashes in DN/PW by default
- Generic HTTP provider: endpoint/username/password/test URL, team/group IDs; generator removes backslashes in password by default

Generic HTTP provider test mode (default: dummy server):
- By default, e2e uses the in-repo dummy Generic HTTP server started automatically by the harness and the generated Helm values point to this dummy server.
- To switch to the external Profiles API, explicitly opt in by setting either `E2E_USE_EXTERNAL_EMP=true` or `USE_DUMMY_EMP_HTTP=false` before running e2e commands.
- The dummy server uses credentials from `EMP_HTTP_DUMMY_USERNAME` / `EMP_HTTP_DUMMY_PASSWORD` and overrides endpoint/test URL in generated values.
- Static provider: user/group names for a simple membership check
- Repositories: `E2E_REPO_PREFIX` (default `e2e-`), `E2E_REPO_PUBLIC`/`E2E_REPO_PRIVATE` (optional), and an optional `CUSTOM_PRIVATE_REPO_TEAM` with `CUSTOM_PRIVATE_REPO_PERMISSION`

Runtime tunables:
- `E2E_TIMEOUT` (default 180s), `E2E_INTERVAL` (default 3s)
- `CONTAINER_TOOL` (docker|podman), `E2E_IMAGE_REPO`, `E2E_IMAGE_TAG` if you want a custom image name
- Optional: `E2E_SKIP_TEAMS=true` to skip the teams scenario only (other scenarios still run)
- `USE_MOCK_GITHUB=true|false` (default: true) — deploy in-cluster mock GitHub API instead of using real credentials

Notes:
- In mock e2e mode, `test.env` credentials are replaced by dummy values; only org/team/user names are used
- In live e2e mode, ensure your GitHub App/installation data in `test.env` matches the test organization you control
- The e2e harness validates both Kubernetes-side status and GitHub-side state via REST API where applicable

