// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "github.com/cloudoperators/repo-guard/api/v1"
	"github.com/cloudoperators/repo-guard/internal/github"
	ghmetrics "github.com/cloudoperators/repo-guard/internal/metrics"
	gogithub "github.com/google/go-github/v90/github"
)

const rulesetFinalizer = "repo-guard.cloudoperators.dev/ruleset"

// GithubOrganizationRulesetReconciler reconciles GithubOrganizationRuleset objects.
type GithubOrganizationRulesetReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	MaxConcurrentReconciles int
}

// +kubebuilder:rbac:groups=repo-guard.cloudoperators.dev,resources=githuborganizationrulesets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=repo-guard.cloudoperators.dev,resources=githuborganizationrulesets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=repo-guard.cloudoperators.dev,resources=githuborganizationrulesets/finalizers,verbs=update

func (r *GithubOrganizationRulesetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	l := log.FromContext(ctx)
	done := ghmetrics.StartReconcileTimer("GithubOrganizationRuleset")
	defer func() {
		result := "success"
		if err != nil {
			result = "error"
		} else if res.RequeueAfter > 0 {
			result = "requeue"
		}
		done(result)
	}()

	rs := &v1.GithubOrganizationRuleset{}
	if err = r.Get(ctx, req.NamespacedName, rs); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	githubName := strings.TrimSpace(rs.Spec.Github)
	orgName := strings.TrimSpace(rs.Spec.Organization)

	// Handle deletion: remove the GitHub ruleset before removing the finalizer.
	if !rs.DeletionTimestamp.IsZero() {
		if containsString(rs.Finalizers, rulesetFinalizer) {
			// Respect dry-run during deletion too — skip the GitHub DELETE.
			if rs.Labels[GITHUB_ORG_LABEL_DRY_RUN] == GITHUB_ORG_LABEL_DRY_RUN_ENABLED_VALUE {
				l.Info("dry-run mode: skipping GitHub ruleset deletion", "name", rs.Spec.Name)
			} else if rs.Status.RulesetID != nil {
				cc := GithubClients[githubName]
				if cc == nil {
					l.Info("waiting for github to be initialised before deleting ruleset", "github", githubName)
					return reconcile.Result{RequeueAfter: time.Second}, nil
				}
				// Use the persisted InstallationID so deletion succeeds even when the
				// GithubOrganization CR has already been removed (e.g. namespace teardown).
				var installationID int64
				if rs.Status.InstallationID != nil {
					installationID = *rs.Status.InstallationID
				} else {
					// Fall back to looking up the GithubOrganization CR on first-time deletion.
					orgCRName := fmt.Sprintf("%s--%s", strings.ToLower(githubName), strings.ToLower(orgName))
					githubOrg := &v1.GithubOrganization{}
					if getErr := r.Get(ctx, types.NamespacedName{Name: orgCRName, Namespace: req.Namespace}, githubOrg); getErr != nil {
						l.Error(getErr, "cannot look up GithubOrganization for ruleset deletion; retaining finalizer")
						return reconcile.Result{}, getErr
					}
					installationID = githubOrg.Spec.InstallationID
				}
				rulesetProvider, provErr := github.NewRulesetProvider(cc, installationID, orgName)
				if provErr != nil {
					l.Error(provErr, "cannot create ruleset provider for deletion; retaining finalizer")
					return reconcile.Result{}, provErr
				}
				if delErr := rulesetProvider.Delete(ctx, *rs.Status.RulesetID); delErr != nil {
					if resetAt, ok := parseGitHubRateLimitReset(delErr.Error()); ok {
						return reconcile.Result{RequeueAfter: time.Until(resetAt) + time.Second}, nil
					}
					// 404 means the ruleset was already deleted — treat as success.
					if ghErr, ok := stderrors.AsType[*gogithub.ErrorResponse](delErr); !ok || ghErr.Response == nil || ghErr.Response.StatusCode != 404 {
						l.Error(delErr, "failed to delete ruleset from GitHub; retaining finalizer", "rulesetID", *rs.Status.RulesetID)
						return reconcile.Result{}, delErr
					}
					l.Info("ruleset already absent on GitHub; proceeding with finalizer removal", "rulesetID", *rs.Status.RulesetID)
				} else {
					l.Info("deleted ruleset from GitHub", "rulesetID", *rs.Status.RulesetID, "name", rs.Spec.Name)
				}
			}
			// Remote ruleset is gone (or never existed); remove the finalizer.
			if removeErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				latest := &v1.GithubOrganizationRuleset{}
				if getErr := r.Get(ctx, req.NamespacedName, latest); getErr != nil {
					return getErr
				}
				latest.Finalizers = removeString(latest.Finalizers, rulesetFinalizer)
				return r.Update(ctx, latest)
			}); removeErr != nil {
				return reconcile.Result{}, removeErr
			}
			// Clean up Prometheus label sets so deleted rulesets are not reported as stale series.
			ghmetrics.DeleteGithubOrganizationRulesetMetrics(
				strings.TrimSpace(rs.Spec.Github),
				strings.TrimSpace(rs.Spec.Organization),
				strings.TrimSpace(rs.Spec.Name),
			)
		}
		return reconcile.Result{}, nil
	}

	// Ensure finalizer is present on the live object.
	if !containsString(rs.Finalizers, rulesetFinalizer) {
		if addErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			latest := &v1.GithubOrganizationRuleset{}
			if getErr := r.Get(ctx, req.NamespacedName, latest); getErr != nil {
				return getErr
			}
			latest.Finalizers = append(latest.Finalizers, rulesetFinalizer)
			return r.Update(ctx, latest)
		}); addErr != nil {
			return reconcile.Result{}, addErr
		}
	}

	// Resolve Github CR to get the installation ID.
	githubInstance := &v1.Github{}
	if err = r.Get(ctx, types.NamespacedName{Name: githubName}, githubInstance); err != nil {
		if apierrors.IsNotFound(err) {
			return r.fail(ctx, rs, "github CR not found: "+githubName)
		}
		return reconcile.Result{}, err
	}

	cc := GithubClients[githubName]
	if cc == nil {
		l.Info("waiting for github to be initialised", "github", githubName)
		return reconcile.Result{RequeueAfter: time.Second}, nil
	}

	// Resolve the GithubOrganization to get the installation ID.
	orgCRName := fmt.Sprintf("%s--%s", strings.ToLower(githubName), strings.ToLower(orgName))
	githubOrg := &v1.GithubOrganization{}
	if err = r.Get(ctx, types.NamespacedName{Name: orgCRName, Namespace: req.Namespace}, githubOrg); err != nil {
		if apierrors.IsNotFound(err) {
			return r.fail(ctx, rs, "GithubOrganization not found: "+orgCRName)
		}
		return reconcile.Result{}, err
	}

	rulesetProvider, err := github.NewRulesetProvider(cc, githubOrg.Spec.InstallationID, orgName)
	if err != nil {
		return r.fail(ctx, rs, "create ruleset provider: "+err.Error())
	}

	// Persist the installation ID so the deletion path works even when the
	// GithubOrganization CR is gone (e.g. namespace teardown ordering).
	if rs.Status.InstallationID == nil || *rs.Status.InstallationID != githubOrg.Spec.InstallationID {
		iid := githubOrg.Spec.InstallationID
		if persistErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			latest := &v1.GithubOrganizationRuleset{}
			if getErr := r.Get(ctx, req.NamespacedName, latest); getErr != nil {
				return getErr
			}
			latest.Status.InstallationID = &iid
			return r.Client.Status().Update(ctx, latest)
		}); persistErr != nil {
			return reconcile.Result{}, persistErr
		}
	}

	// Respect the dry-run label — log what would happen but skip GitHub mutations.
	if rs.Labels[GITHUB_ORG_LABEL_DRY_RUN] == GITHUB_ORG_LABEL_DRY_RUN_ENABLED_VALUE {
		l.Info("dry-run mode: skipping GitHub ruleset mutations", "name", rs.Spec.Name)
		return r.succeed(ctx, rs)
	}

	// Adopt an existing ruleset by name if we don't have an ID yet.
	if rs.Status.RulesetID == nil {
		existing, listErr := rulesetProvider.ListAll(ctx)
		if listErr != nil {
			if resetAt, ok := parseGitHubRateLimitReset(listErr.Error()); ok {
				return reconcile.Result{RequeueAfter: time.Until(resetAt) + time.Second}, nil
			}
			return r.fail(ctx, rs, "list rulesets: "+listErr.Error())
		}
		for _, e := range existing {
			// Only adopt org-owned rulesets — inherited enterprise rulesets
			// share names but cannot be updated via the org endpoint.
			if e.GetName() != rs.Spec.Name {
				continue
			}
			if e.SourceType == nil || *e.SourceType != gogithub.RulesetSourceTypeOrganization {
				continue
			}
			// GitHub org names are case-insensitive; use EqualFold to avoid
			// skipping adoption when the API returns a differently-cased name.
			if !strings.EqualFold(e.GetSource(), orgName) {
				continue
			}
			id := e.GetID()
			if updateErr := r.setRulesetID(ctx, rs, id); updateErr != nil {
				return reconcile.Result{}, updateErr
			}
			l.Info("adopted existing ruleset", "rulesetID", id, "name", rs.Spec.Name)
			break
		}
	}

	desired := github.SpecToGitHub(rs.Spec)

	if rs.Status.RulesetID == nil {
		// Create a new ruleset.
		created, createErr := rulesetProvider.Create(ctx, desired)
		if createErr != nil {
			if resetAt, ok := parseGitHubRateLimitReset(createErr.Error()); ok {
				return reconcile.Result{RequeueAfter: time.Until(resetAt) + time.Second}, nil
			}
			return r.fail(ctx, rs, "create ruleset: "+createErr.Error())
		}
		id := created.GetID()
		if updateErr := r.setRulesetID(ctx, rs, id); updateErr != nil {
			return reconcile.Result{}, updateErr
		}
		l.Info("created ruleset", "rulesetID", id, "name", rs.Spec.Name)
	} else {
		// Update the existing ruleset.
		_, updateErr := rulesetProvider.Update(ctx, *rs.Status.RulesetID, desired)
		if updateErr != nil {
			if resetAt, ok := parseGitHubRateLimitReset(updateErr.Error()); ok {
				return reconcile.Result{RequeueAfter: time.Until(resetAt) + time.Second}, nil
			}
			// If GitHub returns 404 the ruleset was deleted externally. Clear the
			// stored ID so the next reconcile re-adopts or re-creates it.
			if ghErr, ok := stderrors.AsType[*gogithub.ErrorResponse](updateErr); ok && ghErr.Response != nil && ghErr.Response.StatusCode == 404 {
				if clearErr := r.setRulesetID(ctx, rs, 0); clearErr == nil {
					rs.Status.RulesetID = nil
				}
				l.Info("ruleset no longer exists on GitHub; clearing ID to trigger re-creation", "name", rs.Spec.Name)
				return reconcile.Result{RequeueAfter: time.Second}, nil
			}
			return r.fail(ctx, rs, "update ruleset: "+updateErr.Error())
		}
		l.Info("reconciled ruleset", "rulesetID", *rs.Status.RulesetID, "name", rs.Spec.Name)
	}

	return r.succeed(ctx, rs)
}

// fail marks the resource as Failed with the given message and returns an error
// so controller-runtime applies exponential backoff on the next reconcile.
func (r *GithubOrganizationRulesetReconciler) fail(ctx context.Context, rs *v1.GithubOrganizationRuleset, msg string) (reconcile.Result, error) {
	origErr := fmt.Errorf("%s", msg)
	log.FromContext(ctx).Error(origErr, "ruleset reconcile failed")
	_ = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &v1.GithubOrganizationRuleset{}
		if err := r.Get(ctx, types.NamespacedName{Name: rs.Name, Namespace: rs.Namespace}, latest); err != nil {
			return err
		}
		latest.Status.State = v1.GithubOrganizationRulesetStateFailed
		latest.Status.Message = msg
		setRulesetCondition(latest, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "ReconcileError",
			Message:            msg,
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Client.Status().Update(ctx, latest); err != nil {
			return err
		}
		ghmetrics.SetGithubOrganizationRulesetMetrics(latest)
		ghmetrics.IncRulesetSyncFailure(
			strings.TrimSpace(latest.Spec.Github),
			strings.TrimSpace(latest.Spec.Organization),
			strings.TrimSpace(latest.Spec.Name),
		)
		return nil
	})
	// Return the original error so controller-runtime applies retry backoff.
	return reconcile.Result{}, origErr
}

// succeed marks the resource as Ready.
func (r *GithubOrganizationRulesetReconciler) succeed(ctx context.Context, rs *v1.GithubOrganizationRuleset) (reconcile.Result, error) {
	updateErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &v1.GithubOrganizationRuleset{}
		if err := r.Get(ctx, types.NamespacedName{Name: rs.Name, Namespace: rs.Namespace}, latest); err != nil {
			return err
		}
		latest.Status.State = v1.GithubOrganizationRulesetStateReady
		latest.Status.Message = "ruleset synced"
		setRulesetCondition(latest, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Synced",
			Message:            "ruleset synced successfully",
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Client.Status().Update(ctx, latest); err != nil {
			return err
		}
		ghmetrics.SetGithubOrganizationRulesetMetrics(latest)
		return nil
	})
	return reconcile.Result{}, updateErr
}

// setRulesetID persists the adopted/created GitHub ruleset ID to status.
// Pass id=0 to clear a stale ID (sets status.rulesetId to nil).
func (r *GithubOrganizationRulesetReconciler) setRulesetID(ctx context.Context, rs *v1.GithubOrganizationRuleset, id int64) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &v1.GithubOrganizationRuleset{}
		if err := r.Get(ctx, types.NamespacedName{Name: rs.Name, Namespace: rs.Namespace}, latest); err != nil {
			return err
		}
		if id == 0 {
			latest.Status.RulesetID = nil
		} else {
			latest.Status.RulesetID = &id
		}
		if err := r.Client.Status().Update(ctx, latest); err != nil {
			return err
		}
		// Keep the in-memory copy in sync so subsequent status writes use the ID.
		rs.Status.RulesetID = latest.Status.RulesetID
		return nil
	})
}

// setRulesetCondition upserts a condition on the status by type.
// All fields are always refreshed; LastTransitionTime is preserved only when
// the Status value is unchanged (standard Kubernetes condition semantics).
func setRulesetCondition(rs *v1.GithubOrganizationRuleset, cond metav1.Condition) {
	for i, c := range rs.Status.Conditions {
		if c.Type == cond.Type {
			if c.Status == cond.Status {
				// Status unchanged — keep the original transition time.
				cond.LastTransitionTime = c.LastTransitionTime
			}
			rs.Status.Conditions[i] = cond
			return
		}
	}
	rs.Status.Conditions = append(rs.Status.Conditions, cond)
}

func (r *GithubOrganizationRulesetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.GithubOrganizationRuleset{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Complete(r)
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) []string {
	out := slice[:0:0]
	for _, v := range slice {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}
