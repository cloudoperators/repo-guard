# GithubOrganizationRuleset CRD

`GithubOrganizationRuleset` is a **namespace-scoped** resource that manages an organization-level repository ruleset in GitHub. It maps directly to [GitHub organization rulesets](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/about-rulesets) and ensures the desired state is continuously reconciled.

## Example

```yaml
apiVersion: repo-guard.cloudoperators.dev/v1
kind: GithubOrganizationRuleset
metadata:
  name: com--greenhouse-sandbox--require-pr-review
  namespace: default
spec:
  github: com
  organization: greenhouse-sandbox
  name: require-pr-review
  target: branch
  enforcement: active
  conditions:
    refName:
      include:
        - "~DEFAULT_BRANCH"
    repositoryName:
      include:
        - "~ALL"
      exclude:
        - "sandbox-*"
  rules:
    pullRequest:
      requiredApprovingReviewCount: 1
      dismissStaleReviewsOnPush: true
      requireCodeOwnerReview: false
      requireLastPushApproval: true
      requiredReviewThreadResolution: false
    deletion: true
    nonFastForward: true
```

## Spec Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `github` | string | Yes | Name of the `Github` (cluster-scoped) resource to use for API access. |
| `organization` | string | Yes | GitHub organization slug. |
| `name` | string | Yes | Human-readable name of the ruleset in GitHub. If a ruleset with this name already exists it will be **adopted** (no recreation). |
| `target` | string | No | Kind of resource the ruleset applies to. One of `branch`, `tag`, `push`, `repository`. Defaults to `branch`. |
| `enforcement` | string | Yes | One of `active`, `evaluate`, or `disabled`. |
| `bypassActors` | []BypassActor | No | Actors that may bypass this ruleset. |
| `conditions` | Conditions | No | Narrows which refs and repositories this ruleset applies to. |
| `rules` | Rules | No | Enforcement rules. |

### BypassActor

| Field | Type | Description |
|---|---|---|
| `actorId` | integer | GitHub numeric ID of the team or app. Omit (or set 0) for `DeployKey`. |
| `actorType` | string | One of `Integration`, `OrganizationAdmin`, `RepositoryRole`, `Team`, `DeployKey`. |
| `bypassMode` | string | One of `always`, `pull_request`. |

### Conditions

| Field | Type | Description |
|---|---|---|
| `refName` | RefCondition | Filters which branches or tags the ruleset targets. |
| `repositoryName` | RepositoryNameCondition | Filters which repositories in the organization the ruleset applies to. |

#### RefCondition

| Field | Type | Description |
|---|---|---|
| `include` | []string | Ref patterns to include (e.g. `~DEFAULT_BRANCH`, `refs/heads/main`). |
| `exclude` | []string | Ref patterns to exclude. |

#### RepositoryNameCondition

| Field | Type | Description |
|---|---|---|
| `include` | []string | Repository name patterns to include. Use `~ALL` for all repositories. |
| `exclude` | []string | Repository name patterns to exclude. |
| `protected` | bool | Prevent matching repositories from being renamed (does not restrict matching to already-protected repositories). |

### Rules

| Field | Type | Description |
|---|---|---|
| `creation` | bool | Prevent creating matching refs. |
| `update` | bool | Prevent updating matching refs. |
| `deletion` | bool | Prevent deleting matching refs. |
| `requiredLinearHistory` | bool | Prevent merge commits on matching refs. |
| `requiredSignatures` | bool | Require signed commits. |
| `nonFastForward` | bool | Prevent force pushes to matching refs. |
| `pullRequest` | PullRequestRuleParameters | Enable pull request rules. |

#### PullRequestRuleParameters

| Field | Type | Description |
|---|---|---|
| `dismissStaleReviewsOnPush` | bool | Dismiss approvals when new commits are pushed. |
| `requireCodeOwnerReview` | bool | Require a code owner to approve the PR. |
| `requireLastPushApproval` | bool | Require approval of the most recent push. |
| `requiredApprovingReviewCount` | integer | Number of required approvals (0–10). |
| `requiredReviewThreadResolution` | bool | Require all review threads to be resolved. |

## Status Fields

| Field | Type | Description |
|---|---|---|
| `rulesetId` | integer | GitHub numeric ID of the managed ruleset. Set once created or adopted. |
| `installationId` | integer | GitHub App installation ID, persisted so the deletion path works even if the `GithubOrganization` CR is removed first. |
| `observedGeneration` | integer | The `.metadata.generation` of the spec that was last successfully reconciled. |
| `state` | string | One of `Pending`, `Ready`, `Failed`. |
| `message` | string | Human-readable description of the last reconciliation result. |
| `lastSyncTime` | timestamp | Time of the most recent completed reconcile attempt (Ready or dryRun). Used by TTL scheduling. |
| `conditions` | []Condition | Standard Kubernetes conditions. The `Ready` condition is set on each reconcile. |

## Lifecycle

1. **Create**: On first reconcile the controller lists existing rulesets. If one with the declared `name` and `Organization` source exists it is **adopted** (its ID is stored in `status.rulesetId`) without recreation. Otherwise a new ruleset is created.
2. **Update**: On every subsequent reconcile the controller calls GitHub's update API to keep the ruleset in sync with the spec.
3. **Delete**: A finalizer (`repo-guard.cloudoperators.dev/ruleset`) prevents the CR from being garbage collected until the controller successfully removes the ruleset from GitHub (or the `removeRuleset` label is `"false"`, in which case the finalizer is removed without calling GitHub).

## Behaviour Labels

Labels on the CR control mutation and lifecycle behaviour. These are set by the Helm chart and can also be applied manually.

| Label | Values | Default | Description |
|---|---|---|---|
| `repo-guard.cloudoperators.dev/addRuleset` | `"true"` / `"false"` | `"false"` | When `"false"` the controller skips create and update operations and marks the CR Ready without touching GitHub. Set to `"true"` to enable active management. |
| `repo-guard.cloudoperators.dev/removeRuleset` | `"true"` / `"false"` | `"false"` | When `"false"` the controller removes the finalizer on CR deletion without deleting the ruleset from GitHub (orphan). Set to `"true"` to delete the GitHub ruleset when the CR is removed. |
| `repo-guard.cloudoperators.dev/dryRun` | `"true"` / `"false"` | `"false"` | When `"true"` all GitHub mutations (create, update, delete) are skipped. The CR reaches Ready state without any API calls. |
| `repo-guard.cloudoperators.dev/completedTTL` | duration string (e.g. `"72h"`) | unset | When set, the controller re-reconciles (drifts correct) the ruleset after this duration. Uses `status.lastSyncTime` as the clock so consecutive same-state reconciles each get a fresh full interval. |
| `repo-guard.cloudoperators.dev/failedTTL` | duration string (e.g. `"24h"`) | unset | When set and the CR is in `Failed` state, the controller retries on a fixed schedule instead of exponential backoff. |

> **Safe defaults**: `addRuleset` and `removeRuleset` both default to `"false"` in the Helm chart. You must explicitly opt in to `"true"` to allow the controller to create, update, or delete rulesets in GitHub.

## Prerequisites

The namespace must contain a `GithubOrganization` CR named `<github>--<organization>` (e.g. `com--greenhouse-sandbox`). The controller reads the `installationID` from that CR.

## Helm Chart Configuration

Rulesets are declared under each organization entry in `values.yaml` using the `rulesets` key. The `addRuleset` and `removeRuleset` defaults can be set at org level and overridden per ruleset.

```yaml
githubOrganizations:
  - github: wdf
    organization: sap-cloud-infrastructure
    installationID: 7731
    # Opt in to active management for all rulesets in this org.
    # Per-ruleset addRuleset/removeRuleset override these.
    addRuleset: "true"
    removeRuleset: "true"
    rulesets:
      - name: Protect default branch
        target: branch          # branch | tag | push | repository (default: branch)
        enforcement: active     # active | evaluate | disabled (default: active)
        # addRuleset: "true"    # inherits org-level "true" above
        # removeRuleset: "false" # override: orphan this specific ruleset on deletion
        # dryRun: "false"       # "true": log but skip all GitHub mutations
        # ttl:
        #   completed: "72h"    # re-reconcile after this duration when Ready
        #   failed: "24h"       # retry after this duration when Failed
        conditions:
          refName:
            include:
              - "~DEFAULT_BRANCH"
          repositoryName:
            include:
              - "~ALL"
        rules:
          deletion: true
          nonFastForward: true
          requiredSignatures: true
          pullRequest:
            dismissStaleReviewsOnPush: true
            requireCodeOwnerReview: false
            requireLastPushApproval: true
            requiredApprovingReviewCount: 1
            requiredReviewThreadResolution: false
```

The Helm chart generates a `GithubOrganizationRuleset` CR named `<github>--<organization>--<ruleset-name-slug>` (e.g. `wdf--sap-cloud-infrastructure--protect-default-branch`) for each entry.

`bypassActors` is also supported:

```yaml
rulesets:
  - name: Protect main
    enforcement: active
    addRuleset: "true"
    bypassActors:
      - actorType: OrganizationAdmin
        bypassMode: always
      - actorId: 12345      # GitHub numeric team ID
        actorType: Team
        bypassMode: pull_request
    conditions:
      refName:
        include:
          - "~DEFAULT_BRANCH"
    rules:
      deletion: true
```
