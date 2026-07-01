# Metrics & Monitoring

Repo Guard exposes Prometheus metrics under the `repo_guard_*` namespace, a `ServiceMonitor` for Prometheus Operator, and bundled alerting rules.

## Exported Metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `repo_guard_controller_reconcile_total` | Counter | `controller`, `result` | Total reconciliations by controller (e.g. `GithubTeam`) and result (`success`, `error`, `requeue`). |
| `repo_guard_controller_reconcile_duration_seconds` | Histogram | `controller`, `le` | Reconcile durations. Exposed as `_bucket`, `_sum`, `_count`. |
| `repo_guard_external_api_requests_total` | Counter | `provider`, `operation`, `status` | External API calls. `status` is an HTTP status code or `success`/`error`. |
| `repo_guard_external_api_request_duration_seconds` | Histogram | `provider`, `operation`, `le` | External API call durations. |

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

## ServiceMonitor

The Helm chart ships a `ServiceMonitor` that Prometheus Operator will pick up automatically when `serviceMonitor.enabled: true` is set in the Helm values.

## Perses Dashboard

A Perses dashboard is deployed via the Helm chart as a `ConfigMap` when `persesDashboard.enabled: true`. Import it into your Perses instance to get pre-built panels for all the metrics above.
