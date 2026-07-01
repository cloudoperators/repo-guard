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

Bundled alerting rules are in `config/prometheus/rules.yaml`. They cover:

- **HighReconcileErrorRate** — error rate above threshold for a controller.
- **VeryHighReconcileErrorRate** — elevated error rate.
- **SlowReconciles** — p95 reconcile duration exceeds SLO.
- **NoReconcileActivity** — no reconciliations observed in a window (potential controller liveness issue).
- **ExternalAPIHighErrorRate** — external provider API error rate above threshold.
- **ExternalAPISlowLatency** — external provider p95 latency exceeds SLO.

## PodMonitor

The Helm chart ships a `PodMonitor` that Prometheus Operator will pick up automatically when `monitoring.podMonitor.enabled: true` is set in the Helm values.

## Perses Dashboard

A Perses dashboard is deployed via the Helm chart as a `ConfigMap` when `perses.enabled: true`. Import it into your Perses instance to get pre-built panels for all the metrics above.
