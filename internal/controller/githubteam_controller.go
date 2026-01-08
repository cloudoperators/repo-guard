/*
Copyright 2023 cc.
*/

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/palantir/go-githubapp/githubapp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "github.com/cloudoperators/repo-guard/api/v1"

	"github.com/cloudoperators/repo-guard/internal/github"
	ghmetrics "github.com/cloudoperators/repo-guard/internal/metrics"

	greenhousesapv1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
)

// GithubTeamReconciler reconciles a GithubTeam object
type GithubTeamReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=githubguard.sap,resources=githubteams,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=githubguard.sap,resources=githubteams/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=githubguard.sap,resources=githubteams/finalizers,verbs=update
// +kubebuilder:rbac:groups=githubguard.sap,resources=githubaccountlinks,verbs=get;list;watch
// +kubebuilder:rbac:groups=greenhouse.sap,resources=teams,verbs=get;list;watch
// +kubebuilder:rbac:groups=githubguard.sap,resources=externalmemberproviders,verbs=get;list;watch
func (r *GithubTeamReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	l := log.FromContext(ctx)
	done := ghmetrics.StartReconcileTimer("GithubTeam")
	var githubTeam *v1.GithubTeam
	defer func() {
		// reflect final metrics for team status/operations
		// note: githubTeam may be nil if Get failed
		// but in that case we simply skip setting metrics
		if githubTeam != nil {
			ghmetrics.SetGithubTeamMetrics(githubTeam)
		}
		result := "success"
		if err != nil {
			result = "error"
		} else if res.RequeueAfter > 0 {
			result = "requeue"
		}
		done(result)
	}()

	githubTeam = &v1.GithubTeam{}
	err = r.Get(ctx, req.NamespacedName, githubTeam)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Error(err, "resource not found in kubernetes: reconcile is skipped")
			// if not found -- skip
			return reconcile.Result{}, nil
		}
		l.Error(err, "error during getting the resource")
		return reconcile.Result{}, err
	}

	// update metrics to reflect current state at the beginning of reconcile
	ghmetrics.SetGithubTeamMetrics(githubTeam)

	// If previously rate-limited, honor retry time from the stored error message
	if githubTeam.Status.TeamStatus == v1.GithubTeamStateRateLimited && githubTeam.Status.TeamStatusError != "" {
		if resetAt, ok := parseGitHubRateLimitReset(githubTeam.Status.TeamStatusError); ok {
			now := time.Now().UTC()
			if resetAt.After(now) {
				return reconcile.Result{RequeueAfter: resetAt.Sub(now)}, nil
			}
			// Past reset: clear error and recompute state
			newStatus := githubTeam.Status
			newStatus.TeamStatusError = ""
			tmp := &v1.GithubTeam{Status: newStatus}
			switch {
			case tmp.PendingOperationsFound():
				newStatus.TeamStatus = v1.GithubTeamStatePendingOperations
			case tmp.FailedOperationsFound():
				newStatus.TeamStatus = v1.GithubTeamStateFailed
			default:
				newStatus.TeamStatus = v1.GithubTeamStateComplete
			}
			newStatus.TeamStatusTimestamp = metav1.Now()
			githubTeam.Status = newStatus
			if err := r.Client.Status().Update(ctx, githubTeam); err != nil {
				return reconcile.Result{}, err
			}
			// reflect new status in metrics before proceeding
			ghmetrics.SetGithubTeamMetrics(githubTeam)
		}
	}

	// TTL-based maintenance for GithubTeam status/operations
	if githubTeam.Labels != nil {
		// Clear failed operations and failed status after TTL
		if ttlStr, ok := githubTeam.Labels[GITHUB_TEAM_LABEL_FAILED_TTL]; ok && ttlStr != "" {
			if githubTeam.Status.TeamStatus == v1.GithubTeamStateFailed && !githubTeam.Status.TeamStatusTimestamp.IsZero() {
				if expired, _ := ttlExpired(ttlStr, githubTeam.Status.TeamStatusTimestamp.Time, time.Now()); expired {
					l.Info("failed TTL expired: cleaning failed operations and status error")
					// filter out failed operations
					newOps := make([]v1.GithubUserOperation, 0, len(githubTeam.Status.Operations))
					for _, op := range githubTeam.Status.Operations {
						if op.State != v1.GithubUserOperationStateFailed {
							newOps = append(newOps, op)
						}
					}
					changed := len(newOps) != len(githubTeam.Status.Operations) || githubTeam.Status.TeamStatusError != ""
					if changed {
						newStatus := githubTeam.Status
						newStatus.Operations = newOps
						newStatus.TeamStatusError = ""
						// recompute top-level team status
						temp := &v1.GithubTeam{Status: newStatus}
						if temp.PendingOperationsFound() {
							newStatus.TeamStatus = v1.GithubTeamStatePendingOperations
						} else if temp.FailedOperationsFound() {
							newStatus.TeamStatus = v1.GithubTeamStateFailed
						} else {
							newStatus.TeamStatus = v1.GithubTeamStateComplete
						}
						newStatus.TeamStatusTimestamp = metav1.Now()
						githubTeam.Status = newStatus
						if err := r.Client.Status().Update(ctx, githubTeam); err != nil {
							l.Error(err, "error during status update")
							return reconcile.Result{}, err
						}
						return reconcile.Result{}, nil
					}
				}
			}
		}
		// Clear completed operations after TTL
		if ttlStr, ok := githubTeam.Labels[GITHUB_TEAM_LABEL_COMPLETED_TTL]; ok && ttlStr != "" {
			if !githubTeam.Status.TeamStatusTimestamp.IsZero() {
				if expired, _ := ttlExpired(ttlStr, githubTeam.Status.TeamStatusTimestamp.Time, time.Now()); expired {
					l.Info("completed TTL expired: cleaning completed operations")
					newOps := make([]v1.GithubUserOperation, 0, len(githubTeam.Status.Operations))
					for _, op := range githubTeam.Status.Operations {
						if op.State != v1.GithubUserOperationStateComplete {
							newOps = append(newOps, op)
						}
					}
					if len(newOps) != len(githubTeam.Status.Operations) {
						newStatus := githubTeam.Status
						newStatus.Operations = newOps
						// recompute top-level team status
						temp := &v1.GithubTeam{Status: newStatus}
						if temp.PendingOperationsFound() {
							newStatus.TeamStatus = v1.GithubTeamStatePendingOperations
						} else if temp.FailedOperationsFound() {
							newStatus.TeamStatus = v1.GithubTeamStateFailed
						} else {
							newStatus.TeamStatus = v1.GithubTeamStateComplete
						}
						newStatus.TeamStatusTimestamp = metav1.Now()
						githubTeam.Status = newStatus
						if err := r.Client.Status().Update(ctx, githubTeam); err != nil {
							l.Error(err, "error during status update")
							return reconcile.Result{}, err
						}
						return reconcile.Result{}, nil
					}
				}
			}
		}
		// Clear notfound operations after TTL
		if ttlStr, ok := githubTeam.Labels[GITHUB_TEAM_LABEL_NOTFOUND_TTL]; ok && ttlStr != "" {
			if !githubTeam.Status.TeamStatusTimestamp.IsZero() {
				if expired, _ := ttlExpired(ttlStr, githubTeam.Status.TeamStatusTimestamp.Time, time.Now()); expired {
					l.Info("notfound TTL expired: cleaning notfound operations")
					newOps := make([]v1.GithubUserOperation, 0, len(githubTeam.Status.Operations))
					for _, op := range githubTeam.Status.Operations {
						if op.State != v1.GithubUserOperationStateNotFound {
							newOps = append(newOps, op)
						}
					}
					if len(newOps) != len(githubTeam.Status.Operations) {
						newStatus := githubTeam.Status
						newStatus.Operations = newOps
						// recompute top-level team status
						temp := &v1.GithubTeam{Status: newStatus}
						if temp.PendingOperationsFound() {
							newStatus.TeamStatus = v1.GithubTeamStatePendingOperations
						} else if temp.FailedOperationsFound() {
							newStatus.TeamStatus = v1.GithubTeamStateFailed
						} else {
							newStatus.TeamStatus = v1.GithubTeamStateComplete
						}
						newStatus.TeamStatusTimestamp = metav1.Now()
						githubTeam.Status = newStatus
						if err := r.Client.Status().Update(ctx, githubTeam); err != nil {
							l.Error(err, "error during status update")
							return reconcile.Result{}, err
						}
						return reconcile.Result{}, nil
					}
				}
			}
		}
		// Clear skipped operations after TTL
		if ttlStr, ok := githubTeam.Labels[GITHUB_TEAM_LABEL_SKIPPED_TTL]; ok && ttlStr != "" {
			if !githubTeam.Status.TeamStatusTimestamp.IsZero() {
				if expired, _ := ttlExpired(ttlStr, githubTeam.Status.TeamStatusTimestamp.Time, time.Now()); expired {
					l.Info("skipped TTL expired: cleaning skipped operations")
					newOps := make([]v1.GithubUserOperation, 0, len(githubTeam.Status.Operations))
					for _, op := range githubTeam.Status.Operations {
						if op.State != v1.GithubUserOperationStateSkipped {
							newOps = append(newOps, op)
						}
					}
					if len(newOps) != len(githubTeam.Status.Operations) {
						newStatus := githubTeam.Status
						newStatus.Operations = newOps
						// recompute top-level team status
						temp := &v1.GithubTeam{Status: newStatus}
						if temp.PendingOperationsFound() {
							newStatus.TeamStatus = v1.GithubTeamStatePendingOperations
						} else if temp.FailedOperationsFound() {
							newStatus.TeamStatus = v1.GithubTeamStateFailed
						} else {
							newStatus.TeamStatus = v1.GithubTeamStateComplete
						}
						newStatus.TeamStatusTimestamp = metav1.Now()
						githubTeam.Status = newStatus
						if err := r.Client.Status().Update(ctx, githubTeam); err != nil {
							l.Error(err, "error during status update")
							return reconcile.Result{}, err
						}
						return reconcile.Result{}, nil
					}
				}
			}
		}
	}

	// check for github, org and team data
	// helper to compute the minimum TTL from TTL labels to schedule a requeue for maintenance
	computeMinTTL := func() time.Duration {
		if githubTeam.Labels == nil {
			return 0
		}
		min := time.Duration(0)
		parse := func(key string) {
			if s, ok := githubTeam.Labels[key]; ok && s != "" {
				if d, err := time.ParseDuration(s); err == nil {
					if min == 0 || d < min {
						min = d
					}
				}
			}
		}
		parse(GITHUB_TEAM_LABEL_FAILED_TTL)
		parse(GITHUB_TEAM_LABEL_COMPLETED_TTL)
		parse(GITHUB_TEAM_LABEL_NOTFOUND_TTL)
		parse(GITHUB_TEAM_LABEL_SKIPPED_TTL)
		return min
	}

	githubName := githubTeam.Spec.Github
	if githubName == "" {
		l.Info("github name is not provided for github team")
		githubTeam.Status.TeamStatus = v1.GithubTeamStateFailed
		githubTeam.Status.TeamStatusError = "github name not provided"
		githubTeam.Status.TeamStatusTimestamp = metav1.Now()
		err := r.Client.Status().Update(ctx, githubTeam)
		if err != nil {
			l.Error(err, "error during status update")
			return reconcile.Result{}, err
		}
		if d := computeMinTTL(); d > 0 {
			return reconcile.Result{RequeueAfter: d}, nil
		}
		return reconcile.Result{}, nil
	}
	githubOrgName := githubTeam.Spec.Organization
	if githubOrgName == "" {
		l.Info("github organization is not provided for github team")
		githubTeam.Status.TeamStatus = v1.GithubTeamStateFailed
		githubTeam.Status.TeamStatusError = "organization name not provided"
		githubTeam.Status.TeamStatusTimestamp = metav1.Now()
		err := r.Client.Status().Update(ctx, githubTeam)
		if err != nil {
			l.Error(err, "error during status update")
			return reconcile.Result{}, err
		}
		if d := computeMinTTL(); d > 0 {
			return reconcile.Result{RequeueAfter: d}, nil
		}
		return reconcile.Result{}, nil
	}
	githubTeamName := githubTeam.Spec.Team
	if githubTeamName == "" {
		l.Info("github team name is not provided for github team")
		githubTeam.Status.TeamStatus = v1.GithubTeamStateFailed
		githubTeam.Status.TeamStatusError = "team name not provided"
		githubTeam.Status.TeamStatusTimestamp = metav1.Now()
		err := r.Client.Status().Update(ctx, githubTeam)
		if err != nil {
			l.Error(err, "error during status update")
			return reconcile.Result{}, err
		}
		if d := computeMinTTL(); d > 0 {
			return reconcile.Result{RequeueAfter: d}, nil
		}
		return reconcile.Result{}, nil
	}

	// check for github instance
	githubInstance := &v1.Github{}
	var githubClient githubapp.ClientCreator
	err = r.Get(ctx, types.NamespacedName{Name: githubName, Namespace: req.Namespace}, githubInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("github is not found in kubernetes", "github", githubName)
			githubTeam.Status.TeamStatus = v1.GithubTeamStateFailed
			githubTeam.Status.TeamStatusError = "github not found"
			githubTeam.Status.TeamStatusTimestamp = metav1.Now()
			err := r.Client.Status().Update(ctx, githubTeam)
			if err != nil {
				l.Error(err, "error during status update")
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		} else {
			l.Error(err, "error during getting the github for github team", "github", githubName)
			githubTeam.Status.TeamStatus = v1.GithubTeamStateFailed
			githubTeam.Status.TeamStatusError = "error during getting the github: " + err.Error()
			githubTeam.Status.TeamStatusTimestamp = metav1.Now()
			err := r.Client.Status().Update(ctx, githubTeam)
			if err != nil {
				l.Error(err, "error during status update")
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		}
	}
	githubClient = GithubClients[githubName]
	if githubClient == nil {
		l.Info("waiting for github to be initialized", "github", githubName)
		return reconcile.Result{Requeue: true}, nil
	}

	// check for github organization
	githubOrganization := &v1.GithubOrganization{}
	//FIXME -- naming convention for organizations: GITHUB_NAME--ORG_NAME
	githubOrganizationName := fmt.Sprintf("%s--%s", strings.ToLower(githubName), strings.ToLower(githubOrgName))
	err = r.Get(ctx, types.NamespacedName{Name: githubOrganizationName, Namespace: req.Namespace}, githubOrganization)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("github organization is not found in kubernetes", "GithubOrganization", githubOrganizationName)
			githubTeam.Status.TeamStatus = v1.GithubTeamStateFailed
			githubTeam.Status.TeamStatusError = fmt.Sprintf("organization not found: %v", err)
			githubTeam.Status.TeamStatusTimestamp = metav1.Now()
			err := r.Client.Status().Update(ctx, githubTeam)
			if err != nil {
				l.Error(err, "error during status update")
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		} else {
			l.Error(err, "error during getting the github organization for github team", "GithubOrganization", githubOrganizationName)
			githubTeam.Status.TeamStatus = v1.GithubTeamStateFailed
			githubTeam.Status.TeamStatusError = "error during getting the github organization: " + err.Error()
			githubTeam.Status.TeamStatusTimestamp = metav1.Now()
			err := r.Client.Status().Update(ctx, githubTeam)
			if err != nil {
				l.Error(err, "error during status update")
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		}
	}

	// check for spec fields
	if githubTeam.Spec.GreenhouseTeam != "" && githubTeam.Spec.ExternalMemberProvider != nil {
		l.Info("both greenhouseTeam and externalMemberProvider is set", "githubTeam", githubTeam.Name)
		githubTeam.Status.TeamStatus = v1.GithubTeamStateFailed
		githubTeam.Status.TeamStatusError = "both greenhouseTeam and externalMemberProvider is set"
		githubTeam.Status.TeamStatusTimestamp = metav1.Now()
		err := r.Client.Status().Update(ctx, githubTeam)
		if err != nil {
			l.Error(err, "error during status update")
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}
	if githubTeam.Spec.GreenhouseTeam == "" && githubTeam.Spec.ExternalMemberProvider == nil {
		if githubTeam.Labels == nil {
			githubTeam.Labels = make(map[string]string)
		}
		githubTeam.Labels[GITHUB_TEAMS_LABEL_ORPHANED] = "true"
		err := r.Update(ctx, githubTeam)
		if err != nil {
			l.Error(err, "error during label update")
			return reconcile.Result{}, err
		}
	}
	if githubTeam.Spec.ExternalMemberProvider != nil {
		providersSet := 0
		// Treat LDAP and LDAPGroup as the same provider (backwards compatibility)
		if githubTeam.Spec.ExternalMemberProvider.LDAP != nil || githubTeam.Spec.ExternalMemberProvider.LDAPGroupDepreceated != nil {
			providersSet++
		}
		if githubTeam.Spec.ExternalMemberProvider.GenericHTTP != nil {
			providersSet++
		}
		if githubTeam.Spec.ExternalMemberProvider.Static != nil {
			providersSet++
		}
		if providersSet > 1 {
			l.Info("multiple external member providers are set; only one is allowed", "githubTeam", githubTeam.Name)
			githubTeam.Status.TeamStatus = v1.GithubTeamStateFailed
			githubTeam.Status.TeamStatusError = "multiple external member providers are set; only one is allowed"
			githubTeam.Status.TeamStatusTimestamp = metav1.Now()
			err := r.Client.Status().Update(ctx, githubTeam)
			if err != nil {
				l.Error(err, "error during status update")
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		}
	}

	teamsProvider, err := github.NewTeamsProvider(githubClient, githubOrgName, githubOrganization.Spec.InstallationID)
	if err != nil {
		l.Error(err, "error during creating the teams provider")
		return reconcile.Result{}, err
	}

	usersProvider, err := github.NewUsersProvider(githubClient, githubOrganization.Spec.InstallationID)
	if err != nil {
		l.Error(err, "error during creating the users provider")
		return reconcile.Result{}, err
	}

	// pending means there are still waiting operations on Github side, otherwise check for teams and members in each side
	if githubTeam.Status.TeamStatus != v1.GithubTeamStatePendingOperations {

		l.Info("there are no pending operations, status check started")

		// Check if there is a team in Github, If there is no team in Github -- create it first
		organizationTeams, err := teamsProvider.List()
		if err != nil {
			l.Error(err, "error during listing organization teams")
			if t, ok := parseGitHubRateLimitReset(err.Error()); ok {
				now := time.Now().UTC()
				requeue := time.Duration(0)
				if t.After(now) {
					requeue = t.Sub(now)
				}
				githubTeam.Status.TeamStatus = v1.GithubTeamStateRateLimited
				githubTeam.Status.TeamStatusError = "error during listing organization teams: " + err.Error()
				githubTeam.Status.TeamStatusTimestamp = metav1.Now()
				if uerr := r.Client.Status().Update(ctx, githubTeam); uerr != nil {
					l.Error(uerr, "error during status update")
					return reconcile.Result{}, uerr
				}
				return reconcile.Result{RequeueAfter: requeue}, nil
			}
			return reconcile.Result{}, err
		}

		organizationTeamFound := false
		for _, ot := range organizationTeams {
			if ot == githubTeamName {
				organizationTeamFound = true
			}
		}
		if !organizationTeamFound {
			l.Info("team is not found in Github side, it will be created")
			err := teamsProvider.AddTeam(githubTeamName)
			if err != nil {
				l.Error(err, "error during adding team to Github")
				if t, ok := parseGitHubRateLimitReset(err.Error()); ok {
					now := time.Now().UTC()
					requeue := time.Duration(0)
					if t.After(now) {
						requeue = t.Sub(now)
					}
					githubTeam.Status.TeamStatus = v1.GithubTeamStateRateLimited
					githubTeam.Status.TeamStatusError = "error during adding team to Github: " + err.Error()
					githubTeam.Status.TeamStatusTimestamp = metav1.Now()
					if uerr := r.Client.Status().Update(ctx, githubTeam); uerr != nil {
						l.Error(uerr, "error during status update")
						return reconcile.Result{}, uerr
					}
					return reconcile.Result{RequeueAfter: requeue}, nil
				}
				return reconcile.Result{}, err
			}
			l.Info("team is added to Github, resource will be reconciled")
			return reconcile.Result{Requeue: true}, nil
		}

		// If there is a team -- check for its members in Github
		membersExtended, err := teamsProvider.MembersExtended(githubTeamName)
		if err != nil {
			l.Error(err, "error during getting the members of the team in Github")
			if t, ok := parseGitHubRateLimitReset(err.Error()); ok {
				now := time.Now().UTC()
				requeue := time.Duration(0)
				if t.After(now) {
					requeue = t.Sub(now)
				}
				githubTeam.Status.TeamStatus = v1.GithubTeamStateRateLimited
				githubTeam.Status.TeamStatusError = "error during getting the members of the team in Github: " + err.Error()
				githubTeam.Status.TeamStatusTimestamp = metav1.Now()
				if uerr := r.Client.Status().Update(ctx, githubTeam); uerr != nil {
					l.Error(uerr, "error during status update")
					return reconcile.Result{}, uerr
				}
				return reconcile.Result{RequeueAfter: requeue}, nil
			}
			return reconcile.Result{}, err
		}
		membersExtendedWithGithubUsernames, err := extendGithubMembersWithGreenhouseIDs(ctx, membersExtended, githubName, r.Client, usersProvider)
		if err != nil {
			l.Error(err, "error during extending the members of the team in Github")
			return reconcile.Result{}, err
		}

		if !elementsMatch(githubTeam.Status.Members, membersExtendedWithGithubUsernames) {

			githubTeamForUpdate := &v1.GithubTeam{}
			err := r.Get(ctx, req.NamespacedName, githubTeamForUpdate)
			if err != nil {
				l.Error(err, "error during getting the resource for update")
				return reconcile.Result{}, err
			}
			l.Info("status.members will be updated", "current", githubTeamForUpdate.Status.Members, "update", membersExtendedWithGithubUsernames)
			githubTeamForUpdate.Status.Members = membersExtendedWithGithubUsernames

			err = r.Client.Status().Update(ctx, githubTeamForUpdate)
			if err != nil {
				l.Error(err, "error during status update")
				return reconcile.Result{}, err
			}
			// Do not return here; continue reconciliation to calculate desired state

		}

		greenHouseTeamMemberList := make([]string, 0)

		if githubTeam.Spec.GreenhouseTeam != "" {
			// get the members from Greenhouse
			greenHouseTeam := greenhousesapv1alpha1.Team{}
			err = r.Get(ctx, types.NamespacedName{Name: githubTeam.Spec.GreenhouseTeam, Namespace: req.Namespace}, &greenHouseTeam)
			if err != nil {
				if errors.IsNotFound(err) {
					l.Info("Team is not found in Kubernetes. GithubTeam will be labeled as orphaned", "Team", githubTeam.Spec.GreenhouseTeam)
					// Orpaned GithubTeam
					if githubTeam.Labels == nil {
						githubTeam.Labels = make(map[string]string)
					}
					githubTeam.Labels[GITHUB_TEAMS_LABEL_ORPHANED] = "true"
					err := r.Update(ctx, githubTeam)
					if err != nil {
						l.Error(err, "error during label update")
						return reconcile.Result{}, err
					}
					return reconcile.Result{}, nil
				} else {
					l.Error(err, "error during getting the Team for GithubTeam")
					return reconcile.Result{}, err
				}
			}

			for _, gh := range greenHouseTeam.Status.Members {
				greenHouseTeamMemberList = append(greenHouseTeamMemberList, gh.ID)
			}
		} else if githubTeam.Spec.ExternalMemberProvider != nil {

			if githubTeam.Spec.ExternalMemberProvider.LDAP != nil || githubTeam.Spec.ExternalMemberProvider.LDAPGroupDepreceated != nil {
				// check LDAP Group Provider resource and its status
				ldap := &v1.LDAPGroupProvider{}
				ldapName := ""
				if githubTeam.Spec.ExternalMemberProvider.LDAP != nil {
					ldapName = githubTeam.Spec.ExternalMemberProvider.LDAP.ExternalMemberProvider
				} else {
					ldapName = githubTeam.Spec.ExternalMemberProvider.LDAPGroupDepreceated.LDAPGroupProvider
				}
				err = r.Get(ctx, types.NamespacedName{Name: ldapName, Namespace: req.Namespace}, ldap)
				if err != nil {
					if errors.IsNotFound(err) {
						l.Info("LDAP Group Provider is not found in kubernetes", "LDAPGroupProvider", ldapName)
						githubTeam.Status.TeamStatus = v1.GithubTeamStateFailed
						githubTeam.Status.TeamStatusError = fmt.Sprintf("LDAPGroupProvider is not found: %v", err)
						githubTeam.Status.TeamStatusTimestamp = metav1.Now()
						err := r.Client.Status().Update(ctx, githubTeam)
						if err != nil {
							l.Error(err, "error during status update")
							return reconcile.Result{}, err
						}
						return reconcile.Result{}, nil
					} else {
						l.Error(err, "error during getting the LDAPGroupProvider for github team", "LDAPGroupProvider", ldapName)
						githubTeam.Status.TeamStatus = v1.GithubTeamStateFailed
						githubTeam.Status.TeamStatusError = "error during getting the LDAPGroupProvider: " + err.Error()
						githubTeam.Status.TeamStatusTimestamp = metav1.Now()
						err := r.Client.Status().Update(ctx, githubTeam)
						if err != nil {
							l.Error(err, "error during status update")
							return reconcile.Result{}, err
						}
						return reconcile.Result{}, nil
					}
				}
				ldapProvider, ok := LDAPGroupProviders[ldapName]
				if !ok {
					l.Info("waiting for LDAPGroupProvider to be initialized", "LDAPGroupProviders", ldapName)
					return reconcile.Result{RequeueAfter: time.Second}, nil
				}

				group := ""
				if githubTeam.Spec.ExternalMemberProvider.LDAP != nil {
					group = githubTeam.Spec.ExternalMemberProvider.LDAP.Group
				} else {
					group = githubTeam.Spec.ExternalMemberProvider.LDAPGroupDepreceated.Group
				}
				userIDs, err := ldapProvider.Users(ctx, group)
				if err != nil {
					l.Error(err, "error during getting users for group", "group", group)
					githubTeam.Status.TeamStatus = v1.GithubTeamStateFailed
					githubTeam.Status.TeamStatusError = "error during getting users from ldap: " + err.Error()
					githubTeam.Status.TeamStatusTimestamp = metav1.Now()
					err := r.Client.Status().Update(ctx, githubTeam)
					if err != nil {
						l.Error(err, "error during status update")
						return reconcile.Result{}, err
					}
					return reconcile.Result{}, nil
				}
				greenHouseTeamMemberList = userIDs
			}

			if githubTeam.Spec.ExternalMemberProvider.GenericHTTP != nil {
				// check if provider is registered in the runtime registry
				empName := githubTeam.Spec.ExternalMemberProvider.GenericHTTP.ExternalMemberProvider

				testEmp := v1.GenericExternalMemberProvider{}
				err := r.Get(ctx, types.NamespacedName{Name: githubTeam.Spec.ExternalMemberProvider.GenericHTTP.ExternalMemberProvider, Namespace: req.Namespace}, &testEmp)
				if err != nil {
					l.Error(err, "error during getting external member provider")
					githubTeam.Status.TeamStatus = v1.GithubTeamStateFailed
					githubTeam.Status.TeamStatusError = "error during getting external member provider: " + err.Error()
					githubTeam.Status.TeamStatusTimestamp = metav1.Now()
					err := r.Client.Status().Update(ctx, githubTeam)
					if err != nil {
						l.Error(err, "error during status update")
						return reconcile.Result{}, err
					}
					return reconcile.Result{}, nil
				}

				provider, ok := GenericHTTPProviders[empName]
				if !ok {
					l.Info("waiting for external member provider to be initialized", "ExternalMemberProvider", empName)
					return reconcile.Result{RequeueAfter: time.Second}, nil
				}

				group := githubTeam.Spec.ExternalMemberProvider.GenericHTTP.Group
				userIDs, err := provider.Users(ctx, group)
				if err != nil {
					l.Error(err, "error during getting users for group from external member provider", "group", group)
					githubTeam.Status.TeamStatus = v1.GithubTeamStateFailed
					githubTeam.Status.TeamStatusError = "error during getting users from external member provider: " + err.Error()
					githubTeam.Status.TeamStatusTimestamp = metav1.Now()
					err := r.Client.Status().Update(ctx, githubTeam)
					if err != nil {
						l.Error(err, "error during status update")
						return reconcile.Result{}, err
					}
					return reconcile.Result{}, nil
				}
				greenHouseTeamMemberList = userIDs
			}

			// Static external member provider
			if githubTeam.Spec.ExternalMemberProvider.Static != nil {
				empName := githubTeam.Spec.ExternalMemberProvider.Static.ExternalMemberProvider
				provider, ok := StaticProviders[empName]
				if !ok {
					l.Info("waiting for static external member provider to be initialized", "StaticMemberProvider", empName)
					return reconcile.Result{RequeueAfter: time.Second}, nil
				}
				group := githubTeam.Spec.ExternalMemberProvider.Static.Group
				userIDs, err := provider.Users(ctx, group)
				if err != nil {
					l.Error(err, "error during getting users for group from static member provider", "group", group)
					githubTeam.Status.TeamStatus = v1.GithubTeamStateFailed
					githubTeam.Status.TeamStatusError = "error during getting users from static member provider: " + err.Error()
					githubTeam.Status.TeamStatusTimestamp = metav1.Now()
					if err := r.Client.Status().Update(ctx, githubTeam); err != nil {
						l.Error(err, "error during status update")
						return reconcile.Result{}, err
					}
					return reconcile.Result{}, nil
				}
				greenHouseTeamMemberList = userIDs
			}
		}

		// read optional verified-domain requirement labels
		var requiredDomain string
		if githubTeam.Labels != nil {
			if v := githubTeam.Labels[GITHUB_TEAMS_LABEL_REQUIRE_VERIFIED_DOMAIN_EMAIL]; v != "" {
				requiredDomain = v
			}
		}

		greenHouseTeamMemberListExtended, err := extendGreenhouseMembersWithGithubUsernames(ctx, greenHouseTeamMemberList, githubName, r.Client, usersProvider, requiredDomain, githubTeam.Spec.Organization)
		if err != nil {
			l.Error(err, "error during extending the members of the team in greenhouse team membership")
			return reconcile.Result{}, err
		}

		// "do not use internal usernames externally": If the flag is set,
		// then the internal usernames will not be used in the external operations.
		// This means if GreenhouseID == GithubUsername, we remove that member from the list.
		disableInternalUsernames := false
		if githubTeam.Labels != nil && githubTeam.Labels[GITHUB_TEAMS_LABEL_DISABLE_INTERNAL_USERNAMES] == GITHUB_TEAMS_LABEL_DISABLE_INTERNAL_USERNAMES_VALUE {
			disableInternalUsernames = true
		}

		if disableInternalUsernames {
			filteredMembers := make([]v1.Member, 0, len(greenHouseTeamMemberListExtended))
			for _, m := range greenHouseTeamMemberListExtended {
				if m.GreenhouseID != m.GithubUsername {
					filteredMembers = append(filteredMembers, m)
				} else {
					l.Info("Member is filtered since disableInternalUsernames flag is set", "member", m)
				}
			}
			greenHouseTeamMemberListExtended = filteredMembers
		}

		statusChanged, newStatus := githubTeam.ChangeCalculator(greenHouseTeamMemberListExtended)

		if statusChanged {

			githubTeamForUpdate := &v1.GithubTeam{}
			err := r.Get(ctx, req.NamespacedName, githubTeamForUpdate)
			if err != nil {
				l.Error(err, "error during getting the resource for update")
				return reconcile.Result{}, err
			}
			githubTeamForUpdate.Status = *newStatus
			// Reflect desired members immediately in the status to make
			// membership intentions visible even before GitHub operations
			// are executed. This helps tests and users observe the target
			// state while Operations track the work to be done.
			githubTeamForUpdate.Status.Members = greenHouseTeamMemberListExtended

			err = r.Client.Status().Update(ctx, githubTeamForUpdate)
			if err != nil {
				l.Error(err, "error during status update")
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil

		} else {
			// check for empty status in kubernetes resource
			if githubTeam.Status.TeamStatus == "" {
				l.Info("TeamStatus is empty, it could be the first round of the resource reconciliation")
				githubTeam.Status.TeamStatus = v1.GithubTeamStateComplete
				githubTeam.Status.TeamStatusTimestamp = metav1.Now()
				err := r.Client.Status().Update(ctx, githubTeam)
				if err != nil {
					l.Error(err, "error during status update")
					return reconcile.Result{}, err
				}
			}
		}

	}

	// dry run - do not take actions on github side
	if githubTeam.Labels != nil {
		if githubTeam.Labels[GITHUB_TEAMS_LABEL_DRY_RUN] == GITHUB_TEAMS_LABEL_DRY_RUN_ENABLED_VALUE {
			if githubTeam.Status.TeamStatus != v1.GithubTeamStateDryRun {
				l.Info("switching to dry run mode")
				githubTeam.Status.TeamStatus = v1.GithubTeamStateDryRun
				githubTeam.Status.TeamStatusTimestamp = metav1.Now()

				err := r.Client.Status().Update(ctx, githubTeam)
				if err != nil {
					l.Error(err, "error during status update")
					return reconcile.Result{}, err
				}
				l.Error(err, "dry run mode set, resource is sent to requeue")
				return reconcile.Result{Requeue: true}, nil
			}
		} else {
			// remove the dry run status if it is not enabled
			if githubTeam.Status.TeamStatus == v1.GithubTeamStateDryRun {

				if githubTeam.PendingOperationsFound() {
					githubTeam.Status.TeamStatus = v1.GithubTeamStatePendingOperations
				} else if githubTeam.FailedOperationsFound() {
					githubTeam.Status.TeamStatus = v1.GithubTeamStateFailed
				} else {
					githubTeam.Status.TeamStatus = v1.GithubTeamStateComplete
				}
				l.Info("switching from dry run mode", "newStatus", githubTeam.Status.TeamStatus)
				githubTeam.Status.TeamStatusTimestamp = metav1.Now()
				err := r.Client.Status().Update(ctx, githubTeam)
				if err != nil {
					l.Error(err, "error during status update")
					return reconcile.Result{}, err
				}
				l.Error(err, "resource is sent to requeue")
				return reconcile.Result{Requeue: true}, nil
			}

		}
	}

	if githubTeam.Status.TeamStatus == v1.GithubTeamStateDryRun {
		l.Info("status is dry run: reconcile is skipped")
		return reconcile.Result{}, nil
	}

	// if GithubTeamState is "pending" -- take actions on the Github side
	if githubTeam.Status.TeamStatus == v1.GithubTeamStatePendingOperations {

		l.Info("there are pending operations in the status")

		newStatus := githubTeam.Status.DeepCopy()
		statusChanged := false
		failed := false
		for i, userOperation := range newStatus.Operations {

			if userOperation.State == v1.GithubUserOperationStatePending {

				if userOperation.Operation == v1.GithubUserOperationTypeAdd {

					// check whether action is allowed
					if githubTeam.Labels != nil && githubTeam.Labels[GITHUB_TEAMS_LABEL_ADD_USER] != "" && githubTeam.Labels[GITHUB_TEAMS_LABEL_ADD_USER] != GITHUB_TEAMS_LABEL_ADD_REMOVE_USER_ENABLED_VALUE {
						l.Info("adding users is not enabled for the team: operation skipped")
						newStatus.Operations[i].State = v1.GithubUserOperationStateSkipped
						newStatus.Operations[i].Timestamp = metav1.Now()
						statusChanged = true
						failed = false
					} else {
						userFound, err := teamsProvider.AddUser(githubTeamName, userOperation.User)
						if !userFound {
							l.Info("user not found on GitHub: marking operation as notfound", "user", userOperation.User)
							newStatus.Operations[i].State = v1.GithubUserOperationStateNotFound
							newStatus.Operations[i].Error = "user not found on GitHub"
							newStatus.Operations[i].Timestamp = metav1.Now()
							statusChanged = true
							// Don't set 'failed' to true because this is a terminal state
						} else if err != nil {
							l.Error(err, "error during adding user to the team", "user", userOperation.User, "team", githubTeamName)
							newStatus.Operations[i].State = v1.GithubUserOperationStateFailed
							newStatus.Operations[i].Error = err.Error()
							newStatus.Operations[i].Timestamp = metav1.Now()
							statusChanged = true
							failed = true
						} else {
							l.Info("user is added to the team", "user", userOperation.User, "team", githubTeamName)
							newStatus.Operations[i].State = v1.GithubUserOperationStateComplete
							newStatus.Operations[i].Timestamp = metav1.Now()
							statusChanged = true
						}
					}
				}

				if userOperation.Operation == v1.GithubUserOperationTypeRemove {

					// check whether action is allowed
					if githubTeam.Labels != nil && githubTeam.Labels[GITHUB_TEAMS_LABEL_REMOVE_USER] != "" && githubTeam.Labels[GITHUB_TEAMS_LABEL_REMOVE_USER] != GITHUB_TEAMS_LABEL_ADD_REMOVE_USER_ENABLED_VALUE {
						l.Info("removing users is not enabled for the team: operation skipped")
						newStatus.Operations[i].State = v1.GithubUserOperationStateSkipped
						newStatus.Operations[i].Timestamp = metav1.Now()
						statusChanged = true
						failed = false
					} else {
						err := teamsProvider.RemoveUser(githubTeamName, userOperation.User)
						if err != nil {
							l.Error(err, "error during removing user from the team", "user", userOperation.User, "team", githubTeamName)
							newStatus.Operations[i].State = v1.GithubUserOperationStateFailed
							newStatus.Operations[i].Error = err.Error()
							newStatus.Operations[i].Timestamp = metav1.Now()
							statusChanged = true
							failed = true
						} else {
							l.Info("user is removed from the team", "user", userOperation.User, "team", githubTeamName)
							newStatus.Operations[i].State = v1.GithubUserOperationStateComplete
							newStatus.Operations[i].Timestamp = metav1.Now()
							statusChanged = true
						}

					}
				}
			}
		}
		if statusChanged {

			if failed {
				newStatus.TeamStatus = v1.GithubTeamStateFailed
			} else {
				newStatus.TeamStatus = v1.GithubTeamStateComplete
			}
			newStatus.TeamStatusTimestamp = metav1.Now()

			l.Info("new status is calculated", "status", newStatus.TeamStatus)

			githubTeamForUpdate := &v1.GithubTeam{}
			err := r.Get(ctx, req.NamespacedName, githubTeamForUpdate)
			if err != nil {
				l.Error(err, "error during getting the resource for update")
				return reconcile.Result{}, err
			}
			githubTeamForUpdate.Status = *newStatus

			err = r.Client.Status().Update(ctx, githubTeamForUpdate)
			if err != nil {
				l.Error(err, "error during status update")
				return reconcile.Result{}, err
			} else {
				return reconcile.Result{}, nil
			}
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GithubTeamReconciler) SetupWithManager(mgr ctrl.Manager) error {

	selector := labels.NewSelector()
	orphaned, _ := labels.NewRequirement(GITHUB_TEAMS_LABEL_ORPHANED, selection.NotIn, []string{"true"})
	selector = selector.Add(*orphaned)

	metav1LabelSelector, err := metav1.ParseToLabelSelector(selector.String())
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.GithubTeam{}, builder.WithPredicates(LabelSelectorPredicate(*metav1LabelSelector))).
		Watches(&greenhousesapv1alpha1.Team{}, handler.EnqueueRequestsFromMapFunc(r.greenhouseTeamToGithubTeam)).
		Watches(&v1.GithubAccountLink{}, handler.EnqueueRequestsFromMapFunc(r.githubAccountLinkToGithubTeam)).
		Complete(r)
}

func (r *GithubTeamReconciler) githubAccountLinkToGithubTeam(ctx context.Context, o client.Object) []reconcile.Request {
	l := log.FromContext(ctx).WithValues("GithubAccountLink", o.GetName())

	link, ok := o.(*v1.GithubAccountLink)
	if !ok {
		return nil
	}

	teamList := v1.GithubTeamList{}
	err := r.List(ctx, &teamList)
	if err != nil {
		l.Error(err, "failed to list GithubTeams")
		return nil
	}

	reconcileList := make([]reconcile.Request, 0)
	for _, team := range teamList.Items {
		// If the team has a verified domain requirement, we should re-evaluate it
		// as this account link might change the verification status for one of its members.
		if team.Labels != nil && team.Labels[GITHUB_TEAMS_LABEL_REQUIRE_VERIFIED_DOMAIN_EMAIL] != "" {
			if team.Spec.Github == link.Spec.Github {
				reconcileList = append(reconcileList, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: team.GetNamespace(), Name: team.GetName()}})
			}
		}
	}
	if len(reconcileList) > 0 {
		l.Info("Github Account Link triggers the following resources", "resources", reconcileList)
	}
	return reconcileList
}

func (r *GithubTeamReconciler) greenhouseTeamToGithubTeam(ctx context.Context, o client.Object) []reconcile.Request {

	teamList := v1.GithubTeamList{}
	err := r.List(ctx, &teamList)

	l := log.FromContext(ctx).WithValues("Team", o.GetName())

	if err != nil {
		l.Error(err, "failed to list GithubTeams")
		return nil
	}

	reconcileList := make([]reconcile.Request, 0)
	for _, team := range teamList.Items {
		if o.GetName() == team.Spec.GreenhouseTeam {
			reconcileList = append(reconcileList, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: team.GetNamespace(), Name: team.GetName()}})
		}
	}
	l.Info("Greenhouse Team triggers the following resources", "resources", reconcileList)
	return reconcileList

}

func extendGreenhouseMembersWithGithubUsernames(ctx context.Context, members []string, githubInstance string, k8sClient client.Client, usersProvider github.UsersProvider, requiredDomain string, teamOrg string) ([]v1.Member, error) {
	l := log.FromContext(ctx)

	// 1) List all account links
	var linkList v1.GithubAccountLinkList
	if err := k8sClient.List(ctx, &linkList); err != nil {
		l.Error(err, "listing GithubAccountLink")
		return nil, err
	}

	// 2) Build maps for quick lookup
	idMap := make(map[string]string, len(linkList.Items))
	//    and reverse map[GitHubUserID]GreenhouseID for the case when the provided
	//    member identifier is actually a GitHub login (common in some teams).
	revMap := make(map[string]string, len(linkList.Items))
	//    and maps to the corresponding GithubAccountLink objects
	byGHID := make(map[string]v1.GithubAccountLink, len(linkList.Items))
	byGHUser := make(map[string]v1.GithubAccountLink, len(linkList.Items))
	for _, link := range linkList.Items {
		if link.Spec.Github != githubInstance {
			continue
		}
		idMap[link.Spec.GreenhouseUserID] = link.Spec.GithubUserID
		revMap[link.Spec.GithubUserID] = link.Spec.GreenhouseUserID
		byGHID[link.Spec.GithubUserID] = link
		byGHUser[link.Spec.GreenhouseUserID] = link
	}

	// 3) For each greenhouse member, try to resolve their GitHub username
	var out []v1.Member
	for _, greenhouseInput := range members {
		ghID := greenhouseInput
		githubUsername := greenhouseInput // default fallback

		if gitID, ok := idMap[greenhouseInput]; ok {
			// Case A: input is a GreenhouseID → resolve GitHub login by mapped GitHub user ID
			fetched, found, err := usersProvider.GithubUsernameByID(gitID)
			if err != nil {
				l.Error(err, "fetching GitHub username by ID", "githubUserID", gitID)
				return nil, err
			} else if found {
				githubUsername = fetched
			}
		} else {
			// Case B: input might actually be a GitHub login; try to resolve numeric ID
			if gitID, found, err := usersProvider.GithubIDByUsername(greenhouseInput); err != nil {
				l.Error(err, "resolving GitHub ID by username", "login", greenhouseInput)
				return nil, err
			} else if found {
				// If we can map that GitHub ID back to a GreenhouseID via AccountLink, prefer it
				if mappedGreenhouseID, ok := revMap[gitID]; ok && mappedGreenhouseID != "" {
					ghID = mappedGreenhouseID
				}
				// Also ensure we have the canonical GitHub username for status consistency
				if fetched, ok2, err2 := usersProvider.GithubUsernameByID(gitID); err2 != nil {
					l.Error(err2, "fetching GitHub username by ID", "githubUserID", gitID)
					return nil, err2
				} else if ok2 {
					githubUsername = fetched
				}
			}
		}

		// Optional filtering by verified-domain requirement
		include := true
		if requiredDomain != "" {
			// resolve link by greenhouseID or by GitHub ID
			var link v1.GithubAccountLink
			if v, ok := byGHUser[ghID]; ok {
				link = v
			} else if v, ok := byGHID[idMap[ghID]]; ok {
				link = v
			}
			include = false
			if requiredDomain != "" {
				// New flow: look at results annotation for this team org
				if link.Annotations != nil {
					if raw, ok := link.Annotations[v1.GITHUB_ACCOUNT_LINK_EMAIL_CHECK_RESULTS]; ok && raw != "" {
						var results map[string]struct {
							Domain    string `json:"domain"`
							Verified  bool   `json:"verified"`
							Timestamp string `json:"timestamp"`
						}
						if err := json.Unmarshal([]byte(raw), &results); err == nil {
							if r, ok2 := results[teamOrg]; ok2 && r.Domain == requiredDomain && r.Verified {
								include = true
							}
						}
					}
				}
			}
		}

		if include {
			out = append(out, v1.Member{
				GreenhouseID:   ghID,
				GithubUsername: githubUsername,
			})
		} else {
			l.Info("Member filtered due to domain email verification requirement", "member", ghID, "org", teamOrg, "domain", requiredDomain)
		}
	}

	return out, nil
}

func extendGithubMembersWithGreenhouseIDs(ctx context.Context, members []github.GithubMember, githubInstance string, k8sClient client.Client, usersProv github.UsersProvider) ([]v1.Member, error) {
	l := log.FromContext(ctx)

	// 1) List all GithubAccountLink CRs
	var linkList v1.GithubAccountLinkList
	if err := k8sClient.List(ctx, &linkList); err != nil {
		l.Error(err, "listing GithubAccountLink")
		return nil, err
	}

	// 2) Build reverse map: GitHubUserID → GreenhouseUserID
	revMap := make(map[string]string, len(linkList.Items))
	for _, link := range linkList.Items {
		if link.Spec.Github == githubInstance {
			revMap[link.Spec.GithubUserID] = link.Spec.GreenhouseUserID
		}
	}

	// 3) For each GitHub login, resolve to a GreenhouseID
	var out []v1.Member
	for _, githubMember := range members {
		// default fallback: use the login itself
		greenhouseID := githubMember.Login

		if mapped, ok := revMap[strconv.FormatInt(githubMember.UID, 10)]; ok {
			greenhouseID = mapped
		}

		out = append(out, v1.Member{
			GreenhouseID:   greenhouseID,
			GithubUsername: githubMember.Login,
		})
	}

	return out, nil
}

func LabelSelectorPredicate(s metav1.LabelSelector) predicate.Predicate {
	selector, err := metav1.LabelSelectorAsSelector(&s)
	if err != nil {
		return predicate.Funcs{}
	}
	return predicate.NewPredicateFuncs(func(o client.Object) bool {
		return selector.Matches(labels.Set(o.GetLabels()))
	})
}

const GITHUB_TEAMS_LABEL_ORPHANED = "githubguard.sap/orphaned"

const GITHUB_TEAMS_LABEL_DRY_RUN = "githubguard.sap/dryRun"
const GITHUB_TEAMS_LABEL_DRY_RUN_ENABLED_VALUE = "true"

const GITHUB_TEAMS_LABEL_ADD_USER = "githubguard.sap/addUser"
const GITHUB_TEAMS_LABEL_REMOVE_USER = "githubguard.sap/removeUser"
const GITHUB_TEAMS_LABEL_ADD_REMOVE_USER_ENABLED_VALUE = "true"
const GITHUB_TEAMS_LABEL_DISABLE_INTERNAL_USERNAMES = "githubguard.sap/disableInternalUsernames"
const GITHUB_TEAMS_LABEL_DISABLE_INTERNAL_USERNAMES_VALUE = "true"

// domain-valued label on GithubTeam. When set, the controller will consider only
// GithubAccountLinks that report verified=true for this team's organization and this domain
// in their results annotation.
const GITHUB_TEAMS_LABEL_REQUIRE_VERIFIED_DOMAIN_EMAIL = "githubguard.sap/require-verified-domain-email"

// TTL labels for automatic cleanup on GithubTeam
// When present on GithubTeam, failedTTL clears failed user operations and team failed status
// completedTTL clears completed user operations to avoid status bloat
// notfoundTTL clears notfound user operations to allow retry after some time
// skippedTTL clears skipped user operations to allow retry/cleanup of skipped state after some time
const GITHUB_TEAM_LABEL_FAILED_TTL = "githubguard.sap/failedTTL"
const GITHUB_TEAM_LABEL_COMPLETED_TTL = "githubguard.sap/completedTTL"
const GITHUB_TEAM_LABEL_NOTFOUND_TTL = "githubguard.sap/notfoundTTL"
const GITHUB_TEAM_LABEL_SKIPPED_TTL = "githubguard.sap/skippedTTL"
