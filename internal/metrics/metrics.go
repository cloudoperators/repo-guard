// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"strings"
	"time"

	v1 "github.com/cloudoperators/repo-guard/api/v1"
	"github.com/prometheus/client_golang/prometheus"
	ctrmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	ReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "repo_guard",
			Subsystem: "controller",
			Name:      "reconcile_total",
			Help:      "Total number of reconciliations by controller and result.",
		},
		[]string{"controller", "result"},
	)

	ReconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "repo_guard",
			Subsystem: "controller",
			Name:      "reconcile_duration_seconds",
			Help:      "Duration of reconciliations in seconds by controller.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"controller"},
	)

	ExternalAPIRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "repo_guard",
			Subsystem: "external",
			Name:      "api_requests_total",
			Help:      "Total external API requests by provider, operation and status.",
		},
		[]string{"provider", "operation", "status"},
	)

	ExternalAPIDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "repo_guard",
			Subsystem: "external",
			Name:      "api_request_duration_seconds",
			Help:      "Duration of external API requests in seconds by provider and operation.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"provider", "operation"},
	)

	// Organization status and operations gauges
	GithubOrganizationStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "repo_guard",
			Subsystem: "githuborganization",
			Name:      "status",
			Help:      "Current status of a GithubOrganization resource (one-hot gauge).",
		},
		[]string{"github", "organization", "status"},
	)

	GithubOrganizationOperations = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "repo_guard",
			Subsystem: "githuborganization",
			Name:      "operations",
			Help:      "Number of queued operations in the GithubOrganization status by scope, operation and state.",
		},
		[]string{"github", "organization", "scope", "operation", "state"},
	)

	// Team status and operations gauges
	GithubTeamStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "repo_guard",
			Subsystem: "githubteam",
			Name:      "status",
			Help:      "Current status of a GithubTeam resource (one-hot gauge).",
		},
		[]string{"organization", "team", "status"},
	)

	GithubTeamOperations = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "repo_guard",
			Subsystem: "githubteam",
			Name:      "operations",
			Help:      "Number of member operations in the GithubTeam status by operation and state.",
		},
		[]string{"organization", "team", "operation", "state"},
	)
)

func init() {
	ctrmetrics.Registry.MustRegister(
		ReconcileTotal,
		ReconcileDuration,
		ExternalAPIRequestsTotal,
		ExternalAPIDuration,
		GithubOrganizationStatus,
		GithubOrganizationOperations,
		GithubTeamStatus,
		GithubTeamOperations,
	)
}

// StartReconcileTimer starts a timer for a reconciliation and returns a function to observe
// the duration and increment the reconcile counter with the given result label when called.
func StartReconcileTimer(controller string) func(result string) {
	start := time.Now()
	return func(result string) {
		ReconcileTotal.WithLabelValues(controller, result).Inc()
		ReconcileDuration.WithLabelValues(controller).Observe(time.Since(start).Seconds())
	}
}

// ObserveExternalRequest records duration and increments total counters for an external API call.
func ObserveExternalRequest(provider, operation, status string, started time.Time) {
	ExternalAPIRequestsTotal.WithLabelValues(provider, operation, status).Inc()
	ExternalAPIDuration.WithLabelValues(provider, operation).Observe(time.Since(started).Seconds())
}

// SetGithubOrganizationMetrics sets gauges for the given GithubOrganization's current status
// and counts of pending operations. It zeroes all known status values to avoid stale metrics.
func SetGithubOrganizationMetrics(org *v1.GithubOrganization) {
	if org == nil {
		return
	}
	github := strings.TrimSpace(org.Spec.Github)
	organization := strings.TrimSpace(org.Spec.Organization)
	// one-hot status
	for _, st := range []string{
		string(v1.GithubOrganizationStatePendingOperations),
		string(v1.GithubOrganizationStateFailed),
		string(v1.GithubOrganizationStateComplete),
		string(v1.GithubOrganizationStateDryRun),
		string(v1.GithubOrganizationStateRateLimited),
	} {
		val := 0.0
		if st == string(org.Status.OrganizationStatus) {
			val = 1.0
		}
		GithubOrganizationStatus.WithLabelValues(github, organization, st).Set(val)
	}

	// zero all known buckets we use before recounting
	zeroOrgOps := func(scope, op, state string) {
		GithubOrganizationOperations.WithLabelValues(github, organization, scope, op, state).Set(0)
	}
	setOrgOps := func(scope, op, state string, v float64) {
		GithubOrganizationOperations.WithLabelValues(github, organization, scope, op, state).Set(v)
	}
	scopes := []string{"owners", "teams", "repos"}
	ops := []string{"add", "remove"}
	states := []string{"pending", "complete", "failed", "skipped", "notfound"}
	for _, sc := range scopes {
		for _, op := range ops {
			for _, st := range states {
				zeroOrgOps(sc, op, st)
			}
		}
	}
	// owners
	if org.Status.Operations.OrganizationOwnerOperations != nil {
		counts := map[string]map[string]float64{}
		for _, o := range org.Status.Operations.OrganizationOwnerOperations {
			op := string(o.Operation)
			st := string(o.State)
			if counts[op] == nil {
				counts[op] = map[string]float64{}
			}
			counts[op][st] += 1
		}
		for op, byState := range counts {
			for st, v := range byState {
				setOrgOps("owners", op, st, v)
			}
		}
	}
	// teams
	if org.Status.Operations.GithubTeamOperations != nil {
		counts := map[string]map[string]float64{}
		for _, o := range org.Status.Operations.GithubTeamOperations {
			op := string(o.Operation)
			st := string(o.State)
			if counts[op] == nil {
				counts[op] = map[string]float64{}
			}
			counts[op][st] += 1
		}
		for op, byState := range counts {
			for st, v := range byState {
				setOrgOps("teams", op, st, v)
			}
		}
	}
	// repos
	if org.Status.Operations.RepositoryTeamOperations != nil {
		counts := map[string]map[string]float64{}
		for _, o := range org.Status.Operations.RepositoryTeamOperations {
			op := string(o.Operation)
			st := string(o.State)
			if counts[op] == nil {
				counts[op] = map[string]float64{}
			}
			counts[op][st] += 1
		}
		for op, byState := range counts {
			for st, v := range byState {
				setOrgOps("repos", op, st, v)
			}
		}
	}
}

// SetGithubTeamMetrics sets gauges for the given GithubTeam's current status and operation counts.
func SetGithubTeamMetrics(team *v1.GithubTeam) {
	if team == nil {
		return
	}
	org := strings.TrimSpace(team.Spec.Organization)
	tname := strings.TrimSpace(team.Spec.Team)
	for _, st := range []string{
		string(v1.GithubTeamStatePendingOperations),
		string(v1.GithubTeamStateFailed),
		string(v1.GithubTeamStateComplete),
		string(v1.GithubTeamStateDryRun),
		string(v1.GithubTeamStateRateLimited),
	} {
		val := 0.0
		if st == string(team.Status.TeamStatus) {
			val = 1.0
		}
		GithubTeamStatus.WithLabelValues(org, tname, st).Set(val)
	}

	// zero ops buckets
	ops := []string{"add", "remove"}
	states := []string{"pending", "complete", "failed", "skipped", "notfound"}
	for _, op := range ops {
		for _, st := range states {
			GithubTeamOperations.WithLabelValues(org, tname, op, st).Set(0)
		}
	}
	if team.Status.Operations != nil {
		counts := map[string]map[string]float64{}
		for _, o := range team.Status.Operations {
			op := string(o.Operation)
			st := string(o.State)
			if counts[op] == nil {
				counts[op] = map[string]float64{}
			}
			counts[op][st] += 1
		}
		for op, byState := range counts {
			for st, v := range byState {
				GithubTeamOperations.WithLabelValues(org, tname, op, st).Set(v)
			}
		}
	}
}
