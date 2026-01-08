// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

/*
Copyright 2023 cc.
*/

package controller

import (
	"context"
	"fmt"

	"github.com/palantir/go-githubapp/githubapp"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1 "k8s.io/api/core/v1"

	githubguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	ghmetrics "github.com/cloudoperators/repo-guard/internal/metrics"
)

var GithubClients map[string]githubapp.ClientCreator

func init() {
	GithubClients = make(map[string]githubapp.ClientCreator)
}

// GithubReconciler reconciles a Github object
type GithubReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=githubguard.sap,resources=githubs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=githubguard.sap,resources=githubs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=githubguard.sap,resources=githubs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
func (r *GithubReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	l := log.FromContext(ctx)
	done := ghmetrics.StartReconcileTimer("Github")
	defer func() {
		result := "success"
		if err != nil {
			result = "error"
		} else if res.RequeueAfter > 0 {
			result = "requeue"
		}
		done(result)
	}()

	github := &githubguardsapv1.Github{}
	err = r.Get(ctx, req.NamespacedName, github)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Error(err, "resource not found in kubernetes: reconcile is skipped")
			// if not found -- skip
			//delete(GithubClients, req.NamespacedName.String())
			return ctrl.Result{}, nil
		}
		l.Error(err, "error during getting the resource")
		return reconcile.Result{}, err
	}

	// get secret for credentials
	githubSecret := &corev1.Secret{}
	err = r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: github.Spec.Secret}, githubSecret)
	if err != nil {
		l.Error(err, "error during getting the secret for github")
		github.Status.State = githubguardsapv1.GithubStateFailed
		github.Status.Error = fmt.Sprintf("error in getting secret: %v", err)
		github.Status.Timestamp = metav1.Now()
		err = r.Status().Update(ctx, github)
		if err != nil {
			l.Error(err, "error during status update")
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	cfg := githubapp.Config{
		WebURL:   github.Spec.WebURL,
		V3APIURL: github.Spec.V3APIURL,
	}
	cfg.App.IntegrationID = github.Spec.IntegrationID

	cfg.OAuth.ClientID = string(githubSecret.Data[githubguardsapv1.SECRET_CLIENT_ID_KEY])
	cfg.OAuth.ClientSecret = string(githubSecret.Data[githubguardsapv1.SECRET_CLIENT_SECRET_KEY])
	cfg.App.PrivateKey = string(githubSecret.Data[githubguardsapv1.SECRET_PRIVATE_KEY_KEY])
	cc, err := githubapp.NewDefaultCachingClientCreator(
		cfg,
		githubapp.WithClientUserAgent(github.Spec.ClientUserAgent),
	)
	if err != nil {
		l.Error(err, "error during github client creation")
		github.Status.State = githubguardsapv1.GithubStateFailed
		github.Status.Error = fmt.Sprintf("error in github client creation: %v", err)
		github.Status.Timestamp = metav1.Now()
		updateErr := r.Status().Update(ctx, github)
		if updateErr != nil {
			l.Error(updateErr, "error during status update")
			return reconcile.Result{}, updateErr
		}
		return reconcile.Result{}, nil
	}

	// create a test client
	_, err = cc.NewAppClient()
	if err != nil {
		l.Error(err, "error during github app client creation")
		github.Status.State = githubguardsapv1.GithubStateFailed
		github.Status.Error = fmt.Sprintf("error in github app client creation: %v", err)
		github.Status.Timestamp = metav1.Now()
		updateErr := r.Status().Update(ctx, github)
		if updateErr != nil {
			l.Error(updateErr, "error during status update")
			return reconcile.Result{}, updateErr
		}
		return reconcile.Result{}, nil
	}

	GithubClients[github.Name] = cc

	// update status to running
	github.Status.State = githubguardsapv1.GithubStateRunning
	github.Status.Timestamp = metav1.Now()
	err = r.Status().Update(ctx, github)
	if err != nil {
		l.Error(err, "error during status update")
		return reconcile.Result{}, err
	}
	l.Info("github is configured and running as part of controller")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GithubReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&githubguardsapv1.Github{}).
		Complete(r)
}
