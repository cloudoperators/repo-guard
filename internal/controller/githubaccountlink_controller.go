// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"time"

	v1 "github.com/cloudoperators/repo-guard/api/v1"
	"github.com/cloudoperators/repo-guard/internal/github"
	ghmetrics "github.com/cloudoperators/repo-guard/internal/metrics"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// GithubAccountLinkReconciler reconciles a GithubAccountLink object
type GithubAccountLinkReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=githubguard.sap,resources=githubaccountlinks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=githubguard.sap,resources=githubaccountlinks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=githubguard.sap,resources=githubaccountlinks/finalizers,verbs=update
func (r *GithubAccountLinkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	l := log.FromContext(ctx)
	done := ghmetrics.StartReconcileTimer("GithubAccountLink")
	defer func() {
		result := "success"
		if err != nil {
			result = "error"
		} else if res.RequeueAfter > 0 {
			result = "requeue"
		}
		done(result)
	}()

	githubAccountLink := &v1.GithubAccountLink{}
	err = r.Get(ctx, req.NamespacedName, githubAccountLink)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Error(err, "resource not found in kubernetes: reconcile is skipped")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Determine which checks to run
	needEmailDomainCheck := false
	hasMultiOrgConfig := false
	var multiOrgConfig map[string]struct {
		Domain  string `json:"domain"`
		Enabled bool   `json:"enabled"`
		TTL     string `json:"ttl"`
	}
	if githubAccountLink.Annotations != nil {
		if raw, ok := githubAccountLink.Annotations[v1.GITHUB_ACCOUNT_LINK_EMAIL_CHECK_CONFIG]; ok && raw != "" {
			tmp := make(map[string]struct {
				Domain  string `json:"domain"`
				Enabled bool   `json:"enabled"`
				TTL     string `json:"ttl"`
			})
			if err := json.Unmarshal([]byte(raw), &tmp); err == nil {
				if len(tmp) > 0 {
					hasMultiOrgConfig = true
					multiOrgConfig = tmp
					// run checks only when at least one entry is enabled and has domain
					for _, cfg := range multiOrgConfig {
						if cfg.Enabled && cfg.Domain != "" {
							needEmailDomainCheck = true
							break
						}
					}
				}
			}
		}
	}

	// If no checks are requested, do nothing
	if !needEmailDomainCheck {
		return reconcile.Result{}, nil
	}

	// Track whether we updated anything, and what requeue-after to use
	updated := false
	var minRequeueAfter time.Duration

	// Email domain verification with TTL (via GitHub API)
	if hasMultiOrgConfig {
		// Load existing results
		type resultEntry struct {
			Domain    string `json:"domain"`
			Verified  bool   `json:"verified"`
			Timestamp string `json:"timestamp"`
		}
		results := make(map[string]resultEntry)
		if githubAccountLink.Annotations != nil {
			if raw, ok := githubAccountLink.Annotations[v1.GITHUB_ACCOUNT_LINK_EMAIL_CHECK_RESULTS]; ok && raw != "" {
				_ = json.Unmarshal([]byte(raw), &results)
			}
		}

		// Iterate per org config
		for orgName, cfg := range multiOrgConfig {
			if !cfg.Enabled || cfg.Domain == "" {
				continue
			}

			// TTL handling
			needCheck := false
			var ttlDur time.Duration
			hasTTL := false
			if cfg.TTL != "" {
				if d, err := time.ParseDuration(cfg.TTL); err == nil {
					ttlDur = d
					hasTTL = true
					if minRequeueAfter == 0 || d < minRequeueAfter {
						minRequeueAfter = d
					}
				}
			}
			if prev, ok := results[orgName]; !ok || prev.Timestamp == "" {
				needCheck = true
			} else if hasTTL {
				if ts, err := time.Parse(time.RFC3339, prev.Timestamp); err == nil {
					if time.Now().After(ts.Add(ttlDur)) {
						needCheck = true
					}
				} else {
					needCheck = true
				}
			}

			if !needCheck {
				continue
			}

			// Resolve installation for this org under the same Github instance
			githubName := githubAccountLink.Spec.Github
			githubClient, okClient := GithubClients[githubName]
			if !okClient {
				l.Info("waiting for github to be initialized", "github", githubName)
				return reconcile.Result{RequeueAfter: time.Second}, nil
			}
			var orgList v1.GithubOrganizationList
			if err := r.List(ctx, &orgList); err != nil {
				l.Error(err, "listing GithubOrganization to resolve org for email check")
				return ctrl.Result{}, err
			}
			var installationID int64
			for _, goItem := range orgList.Items {
				if goItem.Spec.Github == githubName && goItem.Spec.Organization == orgName {
					installationID = goItem.Spec.InstallationID
					break
				}
			}

			var ok bool
			if installationID == 0 {
				l.Info("installation not resolved for email check; skipping GitHub call and marking as false",
					"github", githubName, "org", orgName)
				ok = false
			} else {
				usersProvider, err := github.NewUsersProvider(githubClient, installationID)
				if err != nil {
					l.Error(err, "error during creating the users provider")
					return ctrl.Result{}, err
				}
				var checkErr error
				ok, checkErr = usersProvider.HasVerifiedEmailDomainForGithubUID(ctx, orgName, githubAccountLink.Spec.GithubUserID, cfg.Domain)
				if checkErr != nil {
					l.Error(checkErr, "error verifying email domain via GitHub API", "uid", githubAccountLink.Spec.GithubUserID, "domain", cfg.Domain, "org", orgName)
					return ctrl.Result{}, checkErr
				}
			}

			if githubAccountLink.Annotations == nil {
				githubAccountLink.Annotations = make(map[string]string)
			}
			// update results map
			results[orgName] = resultEntry{
				Domain:    cfg.Domain,
				Verified:  ok,
				Timestamp: time.Now().Format(time.RFC3339),
			}
			// write back annotation after loop
			updated = true
		}

		if updated {
			if b, err := json.Marshal(results); err == nil {
				githubAccountLink.Annotations[v1.GITHUB_ACCOUNT_LINK_EMAIL_CHECK_RESULTS] = string(b)
			}
		}
	}

	if updated {
		if err := r.Update(ctx, githubAccountLink); err != nil {
			l.Error(err, "error updating GithubAccountLink after checks")
			return ctrl.Result{}, err
		}
	}

	if minRequeueAfter > 0 {
		return ctrl.Result{RequeueAfter: minRequeueAfter}, nil
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GithubAccountLinkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Reconcile only when email-domain check is requested
	pred := predicate.NewPredicateFuncs(func(o client.Object) bool {
		annotations := o.GetAnnotations()
		if annotations == nil {
			return false
		}
		if _, ok := annotations[v1.GITHUB_ACCOUNT_LINK_EMAIL_CHECK_CONFIG]; ok {
			return true
		}
		return false
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.GithubAccountLink{}, builder.WithPredicates(pred)).
		Named("githubaccountlink").
		Complete(r)
}
