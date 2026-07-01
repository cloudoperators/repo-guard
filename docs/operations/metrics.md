# Metrics & Monitoring

Repo Guard exposes Prometheus metrics under the `repo_guard_*` namespace, a `PodMonitor` for Prometheus Operator, and bundled alerting rules.

## Exported Metrics

### Reconcile metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `repo_guard_controller_reconcile_total` | Counter | `controller`, `result` | Total reconciliations per controller and result (`success`, `error`, `requeue`). |
| `repo_guard_controller_reconcile_duration_seconds` | Histogram | `controller` | Reconcile durations. |

### External API metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `repo_guard_external_api_requests_total` | Counter | `provider`, `operation`, `status` | External provider API calls. `status` is an HTTP status code or `success`/`error`. |
| `repo_guard_external_api_request_duration_seconds` | Histogram | `provider`, `operation` | External provider API call durations. |

### GithubOrganization metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `repo_guard_githuborganization_status` | Gauge | `github`, `organization`, `status` | One-hot gauge for the organization's current reconcile status. |
| `repo_guard_githuborganization_operations` | Gauge | `github`, `organization`, `scope`, `operation`, `state` | Count of queued operations by scope, operation, and state. |
| `repo_guard_githuborganization_managed_teams_total` | Gauge | `github`, `organization` | Number of tracked teams for this organization. |
| `repo_guard_githuborganization_managed_repos_total` | Gauge | `github`, `organization`, `visibility` | Number of managed repositories partitioned by visibility. |
| `repo_guard_githuborganization_sync_failures_total` | Counter | `github`, `organization`, `scope` | Cumulative reconcile cycles that ended in a failed state, by scope. |
| `repo_guard_githuborganization_pending_operations_total` | Gauge | `github`, `organization` | Total pending (not yet executed) operations across all scopes. |

### GithubTeam metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `repo_guard_githubteam_status` | Gauge | `organization`, `team`, `status` | One-hot gauge for the team's current reconcile status. |
| `repo_guard_githubteam_operations` | Gauge | `organization`, `team`, `operation`, `state` | Count of member operations by operation and state. |
| `repo_guard_githubteam_managed_members_total` | Gauge | `organization`, `team` | Number of members currently managed in this team. |
| `repo_guard_githubteam_sync_failures_total` | Counter | `organization`, `team` | Cumulative reconcile cycles that ended in a failed state. |

### GitHub API metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `repo_guard_github_ratelimit_hits_total` | Counter | `controller`, `type` | GitHub API rate-limit events encountered. `type` is `api` or `invitation`. |
| `repo_guard_github_ratelimit_backoff_seconds` | Histogram | `controller` | Duration of rate-limit backoff windows. |
| `repo_guard_github_graphql_calls_total` | Counter | `github`, `organization`, `result` | GitHub GraphQL calls made by ExtendedListGraphQL. |
| `repo_guard_github_etag_cache_hits_total` | Counter | `github`, `organization`, `endpoint` | GitHub REST requests that returned HTTP 304 (ETag cache hit). |
| `repo_guard_github_etag_cache_misses_total` | Counter | `github`, `organization`, `endpoint` | GitHub REST requests that returned HTTP 200 with a cacheable ETag. |

## PromQL Examples

### Basic Reconcile Activity

```
sum by (controller) (rate(repo_guard_controller_reconcile_total[5m]))
```

### Error Rate per Controller

```
sum by (controller) (increase(repo_guard_controller_reconcile_total{result="error"}[10m]))
/
clamp_min(sum by (controller) (increase(repo_guard_controller_reconcile_total[10m])), 1)
```

### Reconcile Latency (p50 / p90 / p95)

```
histogram_quantile(0.5,  sum by (controller,le) (rate(repo_guard_controller_reconcile_duration_seconds_bucket[10m])))
histogram_quantile(0.9,  sum by (controller,le) (rate(repo_guard_controller_reconcile_duration_seconds_bucket[10m])))
histogram_quantile(0.95, sum by (controller,le) (rate(repo_guard_controller_reconcile_duration_seconds_bucket[10m])))
```

### External API Error Rate per Provider/Operation

```
sum by (provider,operation) (increase(repo_guard_external_api_requests_total{status=~"error|[45].."}[10m]))
/
clamp_min(sum by (provider,operation) (increase(repo_guard_external_api_requests_total[10m])), 1)
```

### External API Latency p95

```
histogram_quantile(0.95, sum by (provider,operation,le) (rate(repo_guard_external_api_request_duration_seconds_bucket[10m])))
```

### No Reconcile Activity (per controller)

```
sum by (controller) (increase(repo_guard_controller_reconcile_total[30m]))
```

## Alerting Rules

Bundled alerting rules are deployed via the Helm chart (`charts/repo-guard/templates/prometheusrules.yaml`). The kustomize equivalent lives in `config/prometheus/rules.yaml`. The shipped alerts are:

**Controller alerts**

- **`GithubGuardControllerHighErrorRate`** — error rate above 5% for a controller over 10 minutes.
- **`GithubGuardControllerVeryHighErrorRate`** — error rate above 15% for a controller over 10 minutes.
- **`GithubGuardControllerSlowReconcileP95`** — p95 reconcile duration exceeds 10 s over 15 minutes.
- **`GithubGuardControllerNoReconciles`** — no reconciliations observed in 30 minutes (potential controller liveness issue).

**External provider alerts**

- **`GithubGuardExternalAPIHighErrorRate`** — external provider API error rate above 10% over 10 minutes.
- **`GithubGuardExternalAPISlowP95`** — external provider p95 latency exceeds 5 s over 15 minutes.

**Domain alerts**

- **`GithubGuardOrgRateLimited`** — an organization has been in rate-limited state for more than 5 minutes.
- **`GithubGuardHighPendingOperations`** — an organization has more than 50 pending operations for over 30 minutes.
- **`GithubGuardOrgSyncFailureSpike`** — an organization has failed reconciliation more than 5 times in 30 minutes.
- **`GithubGuardTeamSyncFailureSpike`** — a team has failed reconciliation more than 5 times in 30 minutes.
- **`GithubGuardRateLimitFrequent`** — more than 10 GitHub rate-limit hits in 1 hour.

## PodMonitor

The Helm chart ships a `PodMonitor` that Prometheus Operator will pick up automatically when `monitoring.podMonitor.enabled: true` is set in the Helm values.

## Perses Dashboard

A Perses dashboard is deployed via the Helm chart as a `ConfigMap` when `perses.enabled: true`. Import it into your Perses instance to get pre-built panels for all the metrics above.
