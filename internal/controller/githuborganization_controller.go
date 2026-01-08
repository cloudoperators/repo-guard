/*
Copyright 2023 cc.
*/

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	v1 "github.com/cloudoperators/repo-guard/api/v1"
	"github.com/cloudoperators/repo-guard/internal/github"
	ghmetrics "github.com/cloudoperators/repo-guard/internal/metrics"
	"github.com/palantir/go-githubapp/githubapp"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// GithubOrganizationReconciler reconciles a GithubOrganization object
type GithubOrganizationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=githubguard.sap,resources=githuborganizations,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=githubguard.sap,resources=githuborganizations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=githubguard.sap,resources=githuborganizations/finalizers,verbs=update
// +kubebuilder:rbac:groups=githubguard.sap,resources=githubaccountlinks,verbs=get;list;watch
// +kubebuilder:rbac:groups=githubguard.sap,resources=githubteamrepositories,verbs=get;list;watch

// +kubebuilder:rbac:groups=greenhouse.sap,resources=teams,verbs=get;list;watch
func (r *GithubOrganizationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	l := log.FromContext(ctx)
	done := ghmetrics.StartReconcileTimer("GithubOrganization")
	defer func() {
		result := "success"
		if err != nil {
			result = "error"
		} else if res.RequeueAfter > 0 {
			result = "requeue"
		}
		done(result)
	}()

	githubOrganization := &v1.GithubOrganization{}
	err = r.Get(ctx, req.NamespacedName, githubOrganization)
	if err != nil {
		if errors.IsNotFound(err) {
			// if not found -- skip
			l.Error(err, "resource not found in kubernetes: reconcile is skipped")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Update metrics to reflect current state at the beginning of reconcile
	ghmetrics.SetGithubOrganizationMetrics(githubOrganization)

	// If previously rate-limited, honor retry time from the stored error message
	if githubOrganization.Status.OrganizationStatus == v1.GithubOrganizationStateRateLimited && githubOrganization.Status.OrganizationStatusError != "" {
		if resetAt, ok := parseGitHubRateLimitReset(githubOrganization.Status.OrganizationStatusError); ok {
			now := time.Now().UTC()
			if resetAt.After(now) {
				// Still ratelimited: requeue after the remaining duration
				return reconcile.Result{RequeueAfter: resetAt.Sub(now)}, nil
			}
			// Past the reset time: clear error and recompute top-level state, then continue
			newStatus := githubOrganization.Status
			newStatus.OrganizationStatusError = ""
			tmp := &v1.GithubOrganization{Status: newStatus}
			switch {
			case tmp.PendingOperationsFound():
				newStatus.OrganizationStatus = v1.GithubOrganizationStatePendingOperations
			case tmp.FailedOperationsFound():
				newStatus.OrganizationStatus = v1.GithubOrganizationStateFailed
			default:
				newStatus.OrganizationStatus = v1.GithubOrganizationStateComplete
			}
			newStatus.OrganizationStatusTimestamp = metav1.Now()
			githubOrganization.Status = newStatus
			if err := r.Client.Status().Update(ctx, githubOrganization); err != nil {
				return reconcile.Result{}, err
			}
			// reflect new status in metrics before proceeding
			ghmetrics.SetGithubOrganizationMetrics(githubOrganization)
		}
	}

	// TTL-based maintenance to keep status small and healthy
	if githubOrganization.Labels != nil {
		// Clear failed operations and failed status after TTL
		if ttlStr, ok := githubOrganization.Labels[GITHUB_ORG_LABEL_FAILED_TTL]; ok && ttlStr != "" {
			if githubOrganization.Status.OrganizationStatus == v1.GithubOrganizationStateFailed && !githubOrganization.Status.OrganizationStatusTimestamp.IsZero() {
				if expired, err := ttlExpired(ttlStr, githubOrganization.Status.OrganizationStatusTimestamp.Time, time.Now()); err != nil {
					l.Info("invalid failedTTL duration label; skipping cleanup", "label", GITHUB_ORG_LABEL_FAILED_TTL, "value", ttlStr, "error", err.Error())
				} else if expired {
					l.Info("failed TTL expired: cleaning failed operations and status error")
					newStatus, changed := githubOrganization.CleanFailedOperations()
					if changed || githubOrganization.Status.OrganizationStatusError != "" {
						// reset error and recompute top-level state
						newStatus.OrganizationStatusError = ""
						if (&v1.GithubOrganization{Status: newStatus}).PendingOperationsFound() {
							newStatus.OrganizationStatus = v1.GithubOrganizationStatePendingOperations
						} else if (&v1.GithubOrganization{Status: newStatus}).FailedOperationsFound() {
							newStatus.OrganizationStatus = v1.GithubOrganizationStateFailed
						} else {
							newStatus.OrganizationStatus = v1.GithubOrganizationStateComplete
						}
						newStatus.OrganizationStatusTimestamp = metav1.Now()
						githubOrganization.Status = newStatus
						if err := r.Client.Status().Update(ctx, githubOrganization); err != nil {
							return reconcile.Result{}, err
						}
						// reflect new status/operations in metrics
						ghmetrics.SetGithubOrganizationMetrics(githubOrganization)
						return reconcile.Result{}, nil
					}
				}
			}
		}
		// Clear completed operations after TTL
		if ttlStr, ok := githubOrganization.Labels[GITHUB_ORG_LABEL_COMPLETED_TTL]; ok && ttlStr != "" {
			if !githubOrganization.Status.OrganizationStatusTimestamp.IsZero() {
				if expired, err := ttlExpired(ttlStr, githubOrganization.Status.OrganizationStatusTimestamp.Time, time.Now()); err != nil {
					l.Info("invalid completedTTL duration label; skipping cleanup", "label", GITHUB_ORG_LABEL_COMPLETED_TTL, "value", ttlStr, "error", err.Error())
				} else if expired {
					l.Info("completed TTL expired: cleaning completed operations")
					newStatus, changed := githubOrganization.CleanCompletedOperations()
					if changed {
						// keep current orgStatus if still pending/failed, otherwise mark complete
						if (&v1.GithubOrganization{Status: newStatus}).PendingOperationsFound() {
							newStatus.OrganizationStatus = v1.GithubOrganizationStatePendingOperations
						} else if (&v1.GithubOrganization{Status: newStatus}).FailedOperationsFound() {
							newStatus.OrganizationStatus = v1.GithubOrganizationStateFailed
						} else {
							newStatus.OrganizationStatus = v1.GithubOrganizationStateComplete
						}
						newStatus.OrganizationStatusTimestamp = metav1.Now()
						githubOrganization.Status = newStatus
						if err := r.Client.Status().Update(ctx, githubOrganization); err != nil {
							return reconcile.Result{}, err
						}
						ghmetrics.SetGithubOrganizationMetrics(githubOrganization)
						return reconcile.Result{}, nil
					}
				}
			}
		}
	}

	// check for github and org data
	githubName := githubOrganization.Spec.Github
	if githubName == "" {
		l.Info("github name is not provided for github organization")
		githubOrganization.Status.OrganizationStatus = v1.GithubOrganizationStateFailed
		githubOrganization.Status.OrganizationStatusError = "github name not provided"
		githubOrganization.Status.OrganizationStatusTimestamp = metav1.Now()
		err := r.Client.Status().Update(ctx, githubOrganization)
		if err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	githubOrganizationName := githubOrganization.Spec.Organization
	if githubOrganizationName == "" {
		l.Info("github organization name is not provided for github organization")
		githubOrganization.Status.OrganizationStatus = v1.GithubTeamStateFailed
		githubOrganization.Status.OrganizationStatusError = "organization name not provided"
		githubOrganization.Status.OrganizationStatusTimestamp = metav1.Now()
		err := r.Client.Status().Update(ctx, githubOrganization)
		if err != nil {
			return reconcile.Result{}, err
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
			githubOrganization.Status.OrganizationStatus = v1.GithubOrganizationStateFailed
			githubOrganization.Status.OrganizationStatusError = "github not found"
			githubOrganization.Status.OrganizationStatusTimestamp = metav1.Now()
			err := r.Client.Status().Update(ctx, githubOrganization)
			if err != nil {
				l.Error(err, "error during status update")
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		} else {
			l.Error(err, "error during getting the github for github organization", "github", githubName)
			githubOrganization.Status.OrganizationStatus = v1.GithubOrganizationStateFailed
			githubOrganization.Status.OrganizationStatusError = "error during getting the github: " + err.Error()
			githubOrganization.Status.OrganizationStatusTimestamp = metav1.Now()
			err := r.Client.Status().Update(ctx, githubOrganization)
			if err != nil {
				l.Error(err, "error during status update")
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		}
	}

	githubClient, ok := GithubClients[githubName]
	if !ok {
		l.Info("waiting for github to be initialized", "github", githubName)
		return reconcile.Result{RequeueAfter: time.Second}, nil
	}

	reposProvider, err := github.NewRepositoryProvider(githubClient, githubOrganizationName, githubOrganization.Spec.InstallationID)
	if err != nil {
		l.Error(err, "error during creating the repository provider")
		return reconcile.Result{}, err
	}

	organizationsProvider, err := github.NewOrganizationProvider(githubClient, githubOrganizationName, githubOrganization.Spec.InstallationID)
	if err != nil {
		l.Error(err, "error during creating the organizations provider")
		return reconcile.Result{}, err
	}

	teamsProvider, err := github.NewTeamsProvider(githubClient, githubOrganizationName, githubOrganization.Spec.InstallationID)
	if err != nil {
		l.Error(err, "error during creating the teams provider")
		return reconcile.Result{}, err
	}

	usersProvider, err := github.NewUsersProvider(githubClient, githubOrganization.Spec.InstallationID)
	if err != nil {
		l.Error(err, "error during creating the users provider")
		return reconcile.Result{}, err
	}

	// pending means there are still waiting operations on Github side, otherwise check for owners, teams and repos in each side
	if githubOrganization.Status.OrganizationStatus != v1.GithubOrganizationStatePendingOperations {

		l.Info("there are no pending operations, status check started", "current-status", githubOrganization.Status.OrganizationStatus)

		ownerList, err := organizationsProvider.OwnersExtended()
		if err != nil {
			l.Error(err, "error in getting organization owners from github")
			// Check for GitHub rate limit and requeue accordingly
			if t, ok := parseGitHubRateLimitReset(err.Error()); ok {
				now := time.Now().UTC()
				requeue := time.Duration(0)
				if t.After(now) {
					requeue = t.Sub(now)
				}
				githubOrganization.Status.OrganizationStatus = v1.GithubOrganizationStateRateLimited
				githubOrganization.Status.OrganizationStatusError = "error in getting organization owners: " + err.Error()
				githubOrganization.Status.OrganizationStatusTimestamp = metav1.Now()
				if uerr := r.Client.Status().Update(ctx, githubOrganization); uerr != nil {
					l.Error(uerr, "error during status update")
					return reconcile.Result{}, uerr
				}
				return reconcile.Result{RequeueAfter: requeue}, nil
			}
			githubOrganization.Status.OrganizationStatus = v1.GithubOrganizationStateFailed
			githubOrganization.Status.OrganizationStatusError = "error in getting organization owners: " + err.Error()
			githubOrganization.Status.OrganizationStatusTimestamp = metav1.Now()
			err := r.Client.Status().Update(ctx, githubOrganization)
			if err != nil {
				l.Error(err, "error during status update")
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		}

		teamsList, err := teamsProvider.List()
		if err != nil {
			l.Error(err, "error in getting teams from github")
			if t, ok := parseGitHubRateLimitReset(err.Error()); ok {
				now := time.Now().UTC()
				requeue := time.Duration(0)
				if t.After(now) {
					requeue = t.Sub(now)
				}
				githubOrganization.Status.OrganizationStatus = v1.GithubOrganizationStateRateLimited
				githubOrganization.Status.OrganizationStatusError = "error in getting teams: " + err.Error()
				githubOrganization.Status.OrganizationStatusTimestamp = metav1.Now()
				if uerr := r.Client.Status().Update(ctx, githubOrganization); uerr != nil {
					l.Error(uerr, "error during status update")
					return reconcile.Result{}, uerr
				}
				return reconcile.Result{RequeueAfter: requeue}, nil
			}
			githubOrganization.Status.OrganizationStatus = v1.GithubOrganizationStateFailed
			githubOrganization.Status.OrganizationStatusError = "error in getting teams: " + err.Error()
			githubOrganization.Status.OrganizationStatusTimestamp = metav1.Now()
			err := r.Client.Status().Update(ctx, githubOrganization)
			if err != nil {
				l.Error(err, "error during status update")
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		}

		publicRepos, privateRepos, err := reposProvider.ExtendedList()
		if err != nil {
			l.Error(err, "error in getting teams from github")
			if t, ok := parseGitHubRateLimitReset(err.Error()); ok {
				now := time.Now().UTC()
				requeue := time.Duration(0)
				if t.After(now) {
					requeue = t.Sub(now)
				}
				githubOrganization.Status.OrganizationStatus = v1.GithubOrganizationStateRateLimited
				githubOrganization.Status.OrganizationStatusError = "error in getting teams: " + err.Error()
				githubOrganization.Status.OrganizationStatusTimestamp = metav1.Now()
				if uerr := r.Client.Status().Update(ctx, githubOrganization); uerr != nil {
					l.Error(uerr, "error during status update")
					return reconcile.Result{}, uerr
				}
				return reconcile.Result{RequeueAfter: requeue}, nil
			}
			githubOrganization.Status.OrganizationStatus = v1.GithubOrganizationStateFailed
			githubOrganization.Status.OrganizationStatusError = "error in getting teams: " + err.Error()
			githubOrganization.Status.OrganizationStatusTimestamp = metav1.Now()
			err := r.Client.Status().Update(ctx, githubOrganization)
			if err != nil {
				l.Error(err, "error during status update")
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		}

		updateRequired := false
		// Compact repository status is enabled by default: avoid persisting full repo lists.
		// Ensure we don't keep growing the status by repo lists; clear them if present.
		if len(githubOrganization.Status.PublicRepositories) > 0 || len(githubOrganization.Status.PrivateRepositories) > 0 {
			githubOrganization.Status.PublicRepositories = nil
			githubOrganization.Status.PrivateRepositories = nil
			updateRequired = true
		}

		// Convert owner usernames to GithubMember with UID for proper GreenhouseID mapping
		ownerListExtended, err := extendGithubMembersWithGreenhouseIDs(ctx, ownerList, githubName, r.Client, usersProvider)
		if err != nil {
			l.Error(err, "error during extending github members with greenhouse ids")
			return reconcile.Result{}, err
		}
		if !elementsMatch(githubOrganization.Status.OrganizationOwners, ownerListExtended) {
			l.Info("organization owner list will be updated")
			githubOrganization.Status.OrganizationOwners = ownerListExtended
			updateRequired = true
		}

		if !elementsMatch(githubOrganization.Status.Teams, teamsList) {
			l.Info("teams list will be updated")
			githubOrganization.Status.Teams = teamsList
			updateRequired = true
		}

		if updateRequired {
			githubOrganizationForUpdate := &v1.GithubOrganization{}
			err := r.Get(ctx, req.NamespacedName, githubOrganizationForUpdate)
			if err != nil {
				l.Error(err, "error during getting the resource for update")
				return reconcile.Result{}, err
			}
			githubOrganizationForUpdate.Status = githubOrganization.Status

			err = r.Client.Status().Update(ctx, githubOrganizationForUpdate)
			if err != nil {
				l.Error(err, "error during status update")
				return reconcile.Result{}, err
			} else {
				return reconcile.Result{}, nil
			}
		}

		// find differences and add actions to status
		// PART 1: organization owner comparison
		syncOrgOwners := true
		if githubOrganization.Labels != nil {
			if githubOrganization.Labels[GITHUB_ORG_LABEL_ADD_ORG_OWNER] == "false" && githubOrganization.Labels[GITHUB_ORG_LABEL_REMOVE_ORG_OWNER] == "false" {
				syncOrgOwners = false
			}
		}
		if syncOrgOwners {
			ownersFromKubernetes, retryLater, err := r.ownersFromGithubTeams(ctx, githubOrganization)
			if retryLater {
				l.Info("owners from github teams: it requires retry later")
				return reconcile.Result{RequeueAfter: time.Second * 5}, nil
			}
			if err != nil {
				l.Error(err, "error in getting owners from github teams")
				githubOrganization.Status.OrganizationStatus = v1.GithubOrganizationStateFailed
				githubOrganization.Status.OrganizationStatusError = "error in getting owners: " + err.Error()
				githubOrganization.Status.OrganizationStatusTimestamp = metav1.Now()
				err := r.Client.Status().Update(ctx, githubOrganization)
				if err != nil {
					l.Error(err, "error during status update")
					return reconcile.Result{}, err
				}
				return reconcile.Result{}, nil
			}
			if len(ownersFromKubernetes) == 0 {
				l.Info("owners from github teams: it requires retry later - no owners found in kubernetes side")
				return reconcile.Result{RequeueAfter: time.Second * 5}, nil
			}

			statusChanged, newStatus := githubOrganization.OwnerChangeCalculator(ownersFromKubernetes)
			if statusChanged {
				l.Info("status update for organization due to owner change calculation", "update", newStatus)
				githubOrganizationForUpdate := &v1.GithubOrganization{}
				err := r.Get(ctx, req.NamespacedName, githubOrganizationForUpdate)
				if err != nil {
					l.Error(err, "error during getting the resource for update")
					return reconcile.Result{}, err
				}
				githubOrganizationForUpdate.Status = *newStatus

				err = r.Client.Status().Update(ctx, githubOrganizationForUpdate)
				if err != nil {
					l.Error(err, "error during status update")
					return reconcile.Result{}, err
				}
				return reconcile.Result{}, nil
			}
		}

		// Part 2: team comparison
		// go over the list of teams in Github, check if they exist in kubernetes. if not: add operations to status
		syncTeams := true
		if githubOrganization.Labels != nil {
			if githubOrganization.Labels[GITHUB_ORG_LABEL_ADD_TEAM] == "false" && githubOrganization.Labels[GITHUB_ORG_LABEL_REMOVE_TEAM] == "false" {
				syncTeams = false
			}
		}
		if syncTeams {
			teamsFromKubernetes, err := r.teamsFromGithubTeams(ctx, githubOrganization)
			if err != nil {
				l.Error(err, "error in getting github teams for the organization")
				githubOrganization.Status.OrganizationStatus = v1.GithubOrganizationStateFailed
				githubOrganization.Status.OrganizationStatusError = err.Error()
				githubOrganization.Status.OrganizationStatusTimestamp = metav1.Now()
				err := r.Client.Status().Update(ctx, githubOrganization)
				if err != nil {
					l.Error(err, "error during status update")
					return reconcile.Result{}, err
				}
				return reconcile.Result{}, nil
			}
			statusChanged, newStatus := githubOrganization.TeamChangeCalculator(teamsFromKubernetes)
			if statusChanged {
				l.Info("status update for organization due to team change calculation")
				githubOrganization.Status = *newStatus
				err := r.Client.Status().Update(ctx, githubOrganization)
				if err != nil {
					l.Error(err, "error during status update")
					return reconcile.Result{}, err
				}
				return reconcile.Result{}, nil
			}
		}

		// PART 3: repo comparison
		syncRepos := true
		if githubOrganization.Labels != nil {
			if githubOrganization.Labels[GITHUB_ORG_LABEL_ADD_REPOSITORY_TEAM] == "false" && githubOrganization.Labels[GITHUB_ORG_LABEL_REMOVE_REPOSITORY_TEAM] == "false" {
				syncRepos = false
			}
		}
		if syncRepos {
			githubTeamRepositoryListByOrganization, err := r.GithubTeamRepositoryListByOrganization(ctx, githubOrganization.Spec.Github, githubOrganization.Spec.Organization)
			if err != nil {
				l.Error(err, "error in getting repositories from github")
				return reconcile.Result{}, err
			}
			// Compute changes against freshly fetched repo lists without persisting them
			statusChanged := false
			var newStatus *v1.GithubOrganizationStatus
			temp := githubOrganization.DeepCopy()
			temp.Status.PrivateRepositories = privateRepos
			temp.Status.PublicRepositories = publicRepos
			statusChanged, newStatus = temp.RepoChangeCalculator(githubTeamRepositoryListByOrganization)

			if statusChanged {
				l.Info("status update for organization due to repository change calculation")
				// Populate compact out-of-policy repository list and clear bulky repo lists
				newStatus.OutOfPolicyRepositories = uniquePendingOrFailedRepoNames(newStatus.Operations.RepositoryTeamOperations)
				newStatus.PublicRepositories = nil
				newStatus.PrivateRepositories = nil
				githubOrganization.Status = *newStatus
				err := r.Client.Status().Update(ctx, githubOrganization)
				if err != nil {
					l.Error(err, "error during status update")
					return reconcile.Result{}, err
				}
				return reconcile.Result{}, nil
			}
		}

		// check for empty status in kubernetes resource (for the first run)
		//  no error until here, if there is already error in the status, remove it
		if githubOrganization.Status.OrganizationStatus == "" {
			l.Info("OrganizationStatus is empty, it could be the first round of the resource reconciliation")
			githubOrganization.Status.OrganizationStatus = v1.GithubOrganizationStateComplete
			githubOrganization.Status.OrganizationStatusError = ""
			githubOrganization.Status.OrganizationStatusTimestamp = metav1.Now()
			err := r.Client.Status().Update(ctx, githubOrganization)
			if err != nil {
				l.Error(err, "error during status update")
				return reconcile.Result{}, err
			}
		}

	}

	// dry run - do not take actions on github side
	if githubOrganization.Labels != nil {
		if githubOrganization.Labels[GITHUB_ORG_LABEL_DRY_RUN] == GITHUB_ORG_LABEL_DRY_RUN_ENABLED_VALUE {
			if githubOrganization.Status.OrganizationStatus != v1.GithubOrganizationStateDryRun {
				l.Info("switching to dry run mode")
				githubOrganization.Status.OrganizationStatus = v1.GithubOrganizationStateDryRun
				githubOrganization.Status.OrganizationStatusTimestamp = metav1.Now()
				err := r.Client.Status().Update(ctx, githubOrganization)
				if err != nil {
					l.Error(err, "error during status update")
					return reconcile.Result{}, err
				}
				l.Error(err, "dry run mode set, resource is sent to requeue")
				return reconcile.Result{Requeue: true}, nil
			}
		} else {
			// remove the dry run status if it is not enabled
			if githubOrganization.Status.OrganizationStatus == v1.GithubOrganizationStateDryRun {

				if githubOrganization.PendingOperationsFound() {
					githubOrganization.Status.OrganizationStatus = v1.GithubOrganizationStatePendingOperations
				} else if githubOrganization.FailedOperationsFound() {
					githubOrganization.Status.OrganizationStatus = v1.GithubOrganizationStateFailed
				} else {
					githubOrganization.Status.OrganizationStatus = v1.GithubOrganizationStateComplete
					// if there is an error, remove it from status
					githubOrganization.Status.OrganizationStatusError = ""
				}
				l.Info("switching from dry run mode", "newStatus", githubOrganization.Status.OrganizationStatus)

				githubOrganization.Status.OrganizationStatusTimestamp = metav1.Now()
				err := r.Client.Status().Update(ctx, githubOrganization)
				if err != nil {
					l.Error(err, "error during status update")
					return reconcile.Result{}, err
				}
				l.Error(err, "resource is sent to requeue")
				return reconcile.Result{Requeue: true}, nil
			}
		}
	}

	if githubOrganization.Status.OrganizationStatus == v1.GithubOrganizationStateDryRun {
		l.Info("status is dry run: cleaning check starts")
		// dry run is enabled, do not take actions on github side
		// check for clean labels
		if githubOrganization.Labels[GITHUB_ORG_LABEL_CLEAN_OPERATIONS] == GITHUB_ORG_LABEL_CLEAN_OPERATIONS_COMPLETE {

			statusChanged := false
			githubOrganization.Status, statusChanged = githubOrganization.CleanCompletedOperations()
			if statusChanged {
				l.Info("status will be updated: cleaning completed operations")
				githubOrganization.Status.OrganizationStatusTimestamp = metav1.Now()
				err := r.Client.Status().Update(ctx, githubOrganization)
				if err != nil {
					l.Error(err, "error during status update")
					return reconcile.Result{}, err
				}
				return reconcile.Result{}, nil
			}
			l.Info("clean operations label will be removed")
			delete(githubOrganization.Labels, GITHUB_ORG_LABEL_CLEAN_OPERATIONS)
			err = r.Update(ctx, githubOrganization)
			if err != nil {
				l.Error(err, "error during status update")
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil

		}
		if githubOrganization.Labels[GITHUB_ORG_LABEL_CLEAN_OPERATIONS] == GITHUB_ORG_LABEL_CLEAN_OPERATIONS_FAILED {

			statusChanged := false
			githubOrganization.Status, statusChanged = githubOrganization.CleanFailedOperations()
			if statusChanged {
				l.Info("status will be updated: cleaning failed operations")
				githubOrganization.Status.OrganizationStatusTimestamp = metav1.Now()
				err := r.Client.Status().Update(ctx, githubOrganization)
				if err != nil {
					l.Error(err, "error during status update")
					return reconcile.Result{}, err
				}
				return reconcile.Result{}, nil
			}
			l.Info("clean operations label will be removed")
			delete(githubOrganization.Labels, GITHUB_ORG_LABEL_CLEAN_OPERATIONS)
			err = r.Update(ctx, githubOrganization)
			if err != nil {
				l.Error(err, "error during status update")
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, nil
	}

	// if GithubOrganizationState is "pending" -- take actions on the Github side
	if githubOrganization.Status.OrganizationStatus == v1.GithubOrganizationStatePendingOperations {

		l.Info("there are pending operations in the status")

		newStatus := githubOrganization.Status.DeepCopy()
		statusChanged := false
		failed := false
		// track whether we successfully applied any organization owner change in this cycle
		ownerChangeApplied := false

		// OrganizationOwnerOperations
		for i, organizationOwnerOperation := range newStatus.Operations.OrganizationOwnerOperations {

			if organizationOwnerOperation.State == v1.GithubUserOperationStatePending {

				if organizationOwnerOperation.Operation == v1.GithubUserOperationTypeAdd {

					// check whether action is allowed
					if githubOrganization.Labels != nil && githubOrganization.Labels[GITHUB_ORG_LABEL_ADD_ORG_OWNER] != GITHUB_ORG_LABEL_ADD_REMOVE_ORG_OWNER_ENABLED_VALUE {
						l.Info("adding organization owners is not enabled: operation skipped")
						newStatus.Operations.OrganizationOwnerOperations[i].State = v1.GithubUserOperationStateSkipped
						newStatus.Operations.OrganizationOwnerOperations[i].Timestamp = metav1.Now()
						statusChanged = true
						failed = false

					} else {
						err := organizationsProvider.ChangeToOwner(organizationOwnerOperation.User)
						if err != nil {
							l.Error(err, "error during adding organization owner", "organizationOwner", organizationOwnerOperation.User)
							newStatus.Operations.OrganizationOwnerOperations[i].State = v1.GithubUserOperationStateFailed
							newStatus.Operations.OrganizationOwnerOperations[i].Error = err.Error()
							newStatus.Operations.OrganizationOwnerOperations[i].Timestamp = metav1.Now()
							statusChanged = true
							failed = true
						} else {
							l.Info("organization owner is added", "organizationOwner", organizationOwnerOperation.User)
							newStatus.Operations.OrganizationOwnerOperations[i].State = v1.GithubUserOperationStateComplete
							newStatus.Operations.OrganizationOwnerOperations[i].Timestamp = metav1.Now()
							statusChanged = true
							ownerChangeApplied = true
						}
					}
				}

				if organizationOwnerOperation.Operation == v1.GithubUserOperationTypeRemove {

					// check whether action is allowed
					if githubOrganization.Labels != nil && githubOrganization.Labels[GITHUB_ORG_LABEL_REMOVE_ORG_OWNER] != GITHUB_TEAMS_LABEL_ADD_REMOVE_USER_ENABLED_VALUE {
						l.Info("removing organization owners is not enabled: operation skipped")
						newStatus.Operations.OrganizationOwnerOperations[i].State = v1.GithubUserOperationStateSkipped
						newStatus.Operations.OrganizationOwnerOperations[i].Timestamp = metav1.Now()
						statusChanged = true
						failed = false
					} else {
						err := organizationsProvider.ChangeToMember(organizationOwnerOperation.User)
						if err != nil {
							// Special handling: cannot demote the last admin
							if strings.Contains(strings.ToLower(err.Error()), "last admin") ||
								strings.Contains(err.Error(), "You can't demote the last admin to a member.") {
								l.Info("removing organization owner skipped: last admin cannot be demoted", "organizationOwner", organizationOwnerOperation.User)
								newStatus.Operations.OrganizationOwnerOperations[i].State = v1.GithubUserOperationStateSkipped
								newStatus.Operations.OrganizationOwnerOperations[i].Error = err.Error()
								newStatus.Operations.OrganizationOwnerOperations[i].Timestamp = metav1.Now()
								statusChanged = true
							} else {
								l.Error(err, "error during removing organization owner", "organizationOwner", organizationOwnerOperation.User)
								newStatus.Operations.OrganizationOwnerOperations[i].State = v1.GithubUserOperationStateFailed
								newStatus.Operations.OrganizationOwnerOperations[i].Error = err.Error()
								newStatus.Operations.OrganizationOwnerOperations[i].Timestamp = metav1.Now()
								statusChanged = true
								failed = true
							}
						} else {
							l.Info("organization owner is removed", "organizationOwner", organizationOwnerOperation.User)
							newStatus.Operations.OrganizationOwnerOperations[i].State = v1.GithubUserOperationStateComplete
							newStatus.Operations.OrganizationOwnerOperations[i].Timestamp = metav1.Now()
							statusChanged = true
							ownerChangeApplied = true
						}

					}
				}
			}
		}

		// GithubTeamOperations
		for i, githubTeamOperation := range newStatus.Operations.GithubTeamOperations {

			if githubTeamOperation.State == v1.GithubTeamOperationStatePending {

				if githubTeamOperation.Operation == v1.GithubTeamOperationTypeAdd {

					// check whether action is allowed
					if githubOrganization.Labels != nil && githubOrganization.Labels[GITHUB_ORG_LABEL_ADD_TEAM] != GITHUB_ORG_LABEL_ADD_REMOVE_TEAM_ENABLED_VALUE {
						l.Info("adding teams is not enabled: operation skipped")
						newStatus.Operations.GithubTeamOperations[i].State = v1.GithubTeamOperationStateSkipped
						newStatus.Operations.GithubTeamOperations[i].Timestamp = metav1.Now()
						statusChanged = true
						failed = false

					} else {
						err := teamsProvider.AddTeam(githubTeamOperation.Team)
						if err != nil {
							l.Error(err, "error during adding team", "team", githubTeamOperation.Team)
							newStatus.Operations.GithubTeamOperations[i].State = v1.GithubTeamStateFailed
							newStatus.Operations.GithubTeamOperations[i].Error = err.Error()
							newStatus.Operations.GithubTeamOperations[i].Timestamp = metav1.Now()
							statusChanged = true
							failed = true
						} else {
							l.Info("team is added", "team", githubTeamOperation.Team)
							newStatus.Operations.GithubTeamOperations[i].State = v1.GithubTeamStateComplete
							newStatus.Operations.GithubTeamOperations[i].Timestamp = metav1.Now()
							statusChanged = true
						}
					}
				}

				if githubTeamOperation.Operation == v1.GithubTeamOperationTypeRemove {

					// check whether action is allowed
					if githubOrganization.Labels != nil && githubOrganization.Labels[GITHUB_ORG_LABEL_REMOVE_TEAM] != GITHUB_ORG_LABEL_ADD_REMOVE_TEAM_ENABLED_VALUE {
						l.Info("removing teams is not enabled: operation skipped")
						newStatus.Operations.GithubTeamOperations[i].State = v1.GithubTeamOperationStateSkipped
						newStatus.Operations.GithubTeamOperations[i].Timestamp = metav1.Now()
						statusChanged = true
						failed = false
					} else {
						err := teamsProvider.RemoveTeam(githubTeamOperation.Team)
						if err != nil {
							l.Error(err, "error during removing team", "team", githubTeamOperation.Team)
							newStatus.Operations.GithubTeamOperations[i].State = v1.GithubTeamOperationStateFailed
							newStatus.Operations.GithubTeamOperations[i].Error = err.Error()
							newStatus.Operations.GithubTeamOperations[i].Timestamp = metav1.Now()
							statusChanged = true
							failed = true
						} else {
							l.Info("team is removed", "team", githubTeamOperation.Team)
							newStatus.Operations.GithubTeamOperations[i].State = v1.GithubTeamOperationStateComplete
							newStatus.Operations.GithubTeamOperations[i].Timestamp = metav1.Now()
							statusChanged = true
						}

					}
				}

			}
		}

		// team repository operations
		for i, repositoryTeamOperation := range newStatus.Operations.RepositoryTeamOperations {

			if repositoryTeamOperation.State == v1.GithubRepoTeamOperationStatePending {

				if repositoryTeamOperation.Operation == v1.GithubRepoTeamOperationTypeAdd {

					// check whether action is allowed
					if githubOrganization.Labels != nil && githubOrganization.Labels[GITHUB_ORG_LABEL_ADD_REPOSITORY_TEAM] != GITHUB_ORG_LABEL_ADD_REMOVE_REPOSITORY_TEAM_ENABLED_VALUE {
						l.Info("adding repository&team is not enabled: operation skipped")
						newStatus.Operations.RepositoryTeamOperations[i].State = v1.GithubRepoTeamOperationStateSkipped
						newStatus.Operations.RepositoryTeamOperations[i].Timestamp = metav1.Now()
						statusChanged = true
						failed = false

					} else {
						err := reposProvider.RepositoryTeamAdd(repositoryTeamOperation.Repo, repositoryTeamOperation.Team, repositoryTeamOperation.Permission)
						if err != nil {
							l.Error(err, "error during adding repository&team", "repository", repositoryTeamOperation.Repo, "team", repositoryTeamOperation.Team, "permission", repositoryTeamOperation.Permission)
							newStatus.Operations.RepositoryTeamOperations[i].State = v1.GithubRepoTeamOperationStateFailed
							newStatus.Operations.RepositoryTeamOperations[i].Error = err.Error()
							newStatus.Operations.RepositoryTeamOperations[i].Timestamp = metav1.Now()
							statusChanged = true
							failed = true
						} else {
							l.Info("repository&team is added", "repository", repositoryTeamOperation.Repo, "team", repositoryTeamOperation.Team, "permission", repositoryTeamOperation.Permission)
							newStatus.Operations.RepositoryTeamOperations[i].State = v1.GithubRepoTeamOperationStateComplete
							newStatus.Operations.RepositoryTeamOperations[i].Timestamp = metav1.Now()
							statusChanged = true
						}
					}
				}

				if repositoryTeamOperation.Operation == v1.GithubRepoTeamOperationTypeRemove {

					// check whether action is allowed
					if githubOrganization.Labels != nil && githubOrganization.Labels[GITHUB_ORG_LABEL_REMOVE_REPOSITORY_TEAM] != GITHUB_ORG_LABEL_ADD_REMOVE_TEAM_ENABLED_VALUE {
						l.Info("removing repository&team is not enabled: operation skipped")
						newStatus.Operations.RepositoryTeamOperations[i].State = v1.GithubRepoTeamOperationStateSkipped
						newStatus.Operations.RepositoryTeamOperations[i].Timestamp = metav1.Now()
						statusChanged = true
						failed = false
					} else {
						err := reposProvider.RepositoryTeamRemove(repositoryTeamOperation.Repo, repositoryTeamOperation.Team)
						if err != nil {
							l.Error(err, "error during removing repository&team", "repository", repositoryTeamOperation.Repo, "team", repositoryTeamOperation.Team)
							newStatus.Operations.RepositoryTeamOperations[i].State = v1.GithubRepoTeamOperationStateFailed
							newStatus.Operations.RepositoryTeamOperations[i].Error = err.Error()
							newStatus.Operations.RepositoryTeamOperations[i].Timestamp = metav1.Now()
							statusChanged = true
							failed = true
						} else {
							l.Info("repository&team is removed", "repository", repositoryTeamOperation.Repo, "team", repositoryTeamOperation.Team)
							newStatus.Operations.RepositoryTeamOperations[i].State = v1.GithubRepoTeamOperationStateComplete
							newStatus.Operations.RepositoryTeamOperations[i].Timestamp = metav1.Now()
							statusChanged = true
						}

					}
				}

			}
		}

		// status changed check & reflect on Kubernetes
		if statusChanged {

			if failed {
				newStatus.OrganizationStatus = v1.GithubOrganizationStateFailed
			} else {
				newStatus.OrganizationStatus = v1.GithubOrganizationStateComplete
				newStatus.OrganizationStatusError = ""
			}
			newStatus.OrganizationStatusTimestamp = metav1.Now()
			l.Info("new status is calculated", "status", newStatus.OrganizationStatus)

			githubOrganizationForUpdate := &v1.GithubOrganization{}
			err := r.Get(ctx, req.NamespacedName, githubOrganizationForUpdate)
			if err != nil {
				l.Error(err, "error during getting the resource for update")
				return reconcile.Result{}, err
			}
			githubOrganizationForUpdate.Status = *newStatus

			err = r.Client.Status().Update(ctx, githubOrganizationForUpdate)
			if err != nil {
				l.Error(err, "error during status update")
				return reconcile.Result{}, err
			} else {
				// After applying any owner change, requeue after a short delay
				// to give GitHub time to reflect the new state before recalculation
				if ownerChangeApplied && !failed {
					return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
				}
				return reconcile.Result{}, nil
			}
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GithubOrganizationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.GithubOrganization{}).
		Watches(&v1.GithubTeam{}, handler.EnqueueRequestsFromMapFunc(r.githubTeamToGithubOrganizationAsOrganizationOwner)).
		Watches(&v1.GithubTeamRepository{}, handler.EnqueueRequestsFromMapFunc(r.githubTeamRepositoryToGithubOrganization)).
		Complete(r)
}

func (r *GithubOrganizationReconciler) teamsFromGithubTeams(ctx context.Context, githubOrganization *v1.GithubOrganization) ([]string, error) {

	teamList := make([]string, 0)
	githubTeamList := &v1.GithubTeamList{}
	l := log.FromContext(ctx)

	err := r.List(context.Background(), githubTeamList)
	if err != nil {
		l.Error(err, "failed to list GithubTeams")
		return nil, err
	}

	for _, team := range githubTeamList.Items {
		if team.Spec.Organization == githubOrganization.Spec.Organization && team.Spec.Github == githubOrganization.Spec.Github {
			teamList = append(teamList, team.Spec.Team)
		}
	}
	return teamList, nil

}

func (r *GithubOrganizationReconciler) ownersFromGithubTeams(ctx context.Context, githubOrganization *v1.GithubOrganization) ([]v1.Member, bool, error) {

	ownerMap := make(map[string]v1.Member, 0)

	for _, team := range githubOrganization.Spec.OrganizationOwnerTeams {

		githubTeam := &v1.GithubTeam{}
		teamFullName := fmt.Sprintf("%s--%s--%s", strings.ToLower(githubOrganization.Spec.Github), strings.ToLower(githubOrganization.Spec.Organization), strings.ToLower(team))
		err := r.Get(ctx, types.NamespacedName{Name: teamFullName, Namespace: githubOrganization.Namespace}, githubTeam)
		if err != nil {
			// If the GithubTeam resource is not found yet, consider it "not ready" and retry later
			if errors.IsNotFound(err) {
				return nil, true, nil
			}
			return nil, false, err
		}

		switch githubTeam.Status.TeamStatus {
		case v1.GithubTeamStatePendingOperations:
			// Team not ready yet
			return nil, true, nil
		case v1.GithubTeamStateFailed:
			// Team reconciliation failed; propagate error
			return nil, false, fmt.Errorf("team %s state is failed. cannot sync organization owners", team)
		case v1.GithubTeamStateComplete, v1.GithubTeamStateDryRun:
			// Ready states: use members
			for _, m := range githubTeam.Status.Members {
				ownerMap[m.GreenhouseID] = m
			}
		default:
			// Unknown/empty state: treat as not ready yet
			return nil, true, nil
		}
	}

	ownerList := make([]v1.Member, 0)
	for _, k := range ownerMap {
		ownerList = append(ownerList, k)
	}
	return ownerList, false, nil
}

func (r *GithubOrganizationReconciler) GithubTeamRepositoryListByOrganization(ctx context.Context, github, organization string) ([]v1.GithubTeamRepository, error) {
	l := log.FromContext(ctx)

	list := v1.GithubTeamRepositoryList{}
	err := r.List(ctx, &list)
	if err != nil {
		l.Error(err, "error during listing the GithubTeamRepository")
		return nil, err
	}

	githubTeamRepositoryListFiltered := make([]v1.GithubTeamRepository, 0)

	for _, githubTeamRepository := range list.Items {
		if githubTeamRepository.Spec.Github == github && githubTeamRepository.Spec.Organization == organization {
			githubTeamRepositoryListFiltered = append(githubTeamRepositoryListFiltered, githubTeamRepository)
		}
	}
	return githubTeamRepositoryListFiltered, nil

}

func (r *GithubOrganizationReconciler) githubTeamToGithubOrganizationAsOrganizationOwner(ctx context.Context, o client.Object) []reconcile.Request {

	l := log.FromContext(ctx).WithValues("GithubTeam", o.GetName())

	orgList := v1.GithubOrganizationList{}
	err := r.List(ctx, &orgList)

	if err != nil {
		l.Error(err, "failed to list GithubOrganizations")
		return nil
	}

	githubTeam, ok := o.(*v1.GithubTeam)
	if !ok {
		l.Error(nil, "failed to cast received object to GithubTeam")
		return nil
	}

	reconcileList := make([]reconcile.Request, 0)
	for _, org := range orgList.Items {
		for _, team := range org.Spec.OrganizationOwnerTeams {
			if githubTeam.Spec.Team == team {
				reconcileList = append(reconcileList, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: org.GetNamespace(), Name: org.GetName()}})
			}
		}

	}
	if len(reconcileList) > 0 {
		l.Info("GithubTeam triggers the following resources as organization owner teams", "resources", reconcileList)
	}
	return reconcileList
}

func (r *GithubOrganizationReconciler) githubTeamRepositoryToGithubOrganization(ctx context.Context, o client.Object) []reconcile.Request {

	l := log.FromContext(ctx)

	teamRepository, ok := o.(*v1.GithubTeamRepository)
	if !ok {
		l.Error(nil, "failed to cast received object to GithubTeamRepository")
		return nil
	}

	orgList := v1.GithubOrganizationList{}
	err := r.List(ctx, &orgList)
	if err != nil {
		l.Error(err, "failed to list GithubOrganizations")
		return nil
	}

	reconcileList := make([]reconcile.Request, 0)
	for _, org := range orgList.Items {
		if org.Spec.Github == teamRepository.Spec.Github && org.Spec.Organization == teamRepository.Spec.Organization {
			reconcileList = append(reconcileList, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: org.GetNamespace(), Name: org.GetName()}})
		}
	}
	if len(reconcileList) > 0 {
		l.Info("GithubTeamRepository triggers the following organization resources", "resources", reconcileList)
	}
	return reconcileList

}

const GITHUB_ORG_LABEL_ADD_ORG_OWNER = "githubguard.sap/addOrganizationOwner"
const GITHUB_ORG_LABEL_REMOVE_ORG_OWNER = "githubguard.sap/removeOrganizationOwner"
const GITHUB_ORG_LABEL_ADD_REMOVE_ORG_OWNER_ENABLED_VALUE = "true"

const GITHUB_ORG_LABEL_ADD_TEAM = "githubguard.sap/addTeam"
const GITHUB_ORG_LABEL_REMOVE_TEAM = "githubguard.sap/removeTeam"
const GITHUB_ORG_LABEL_ADD_REMOVE_TEAM_ENABLED_VALUE = "true"

const GITHUB_ORG_LABEL_DRY_RUN = "githubguard.sap/dryRun"
const GITHUB_ORG_LABEL_DRY_RUN_ENABLED_VALUE = "true"

const GITHUB_ORG_LABEL_ADD_REPOSITORY_TEAM = "githubguard.sap/addRepositoryTeam"
const GITHUB_ORG_LABEL_REMOVE_REPOSITORY_TEAM = "githubguard.sap/removeRepositoryTeam"
const GITHUB_ORG_LABEL_ADD_REMOVE_REPOSITORY_TEAM_ENABLED_VALUE = "true"

const GITHUB_ORG_LABEL_CLEAN_OPERATIONS = "githubguard.sap/cleanOperations"
const GITHUB_ORG_LABEL_CLEAN_OPERATIONS_COMPLETE = "complete"
const GITHUB_ORG_LABEL_CLEAN_OPERATIONS_FAILED = "failed"

// TTL labels for automatic cleanup
// When present on GithubOrganization, failedTTL clears failed operations and org failed status
// completedTTL clears completed operations to avoid status bloat
const GITHUB_ORG_LABEL_FAILED_TTL = "githubguard.sap/failedTTL"
const GITHUB_ORG_LABEL_COMPLETED_TTL = "githubguard.sap/completedTTL"

// ttlExpired parses a duration string (e.g., "24h", "30m") and checks if since+TTL is before now.
func ttlExpired(ttlStr string, since time.Time, now time.Time) (bool, error) {
	d, err := time.ParseDuration(ttlStr)
	if err != nil {
		return false, err
	}
	return now.After(since.Add(d)), nil
}

// uniquePendingOrFailedRepoNames returns unique repository names that have pending or failed operations.
func uniquePendingOrFailedRepoNames(ops []v1.GithubRepoTeamOperation) []string {
	m := map[string]struct{}{}
	for _, op := range ops {
		if op.State == v1.GithubRepoTeamOperationStatePending || op.State == v1.GithubRepoTeamOperationStateFailed {
			if op.Repo != "" {
				m[op.Repo] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(m))
	for r := range m {
		out = append(out, r)
	}
	return out
}
