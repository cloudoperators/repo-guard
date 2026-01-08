// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	githubguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	genericprovider "github.com/cloudoperators/repo-guard/internal/external-provider/generic-http"
	ghmetrics "github.com/cloudoperators/repo-guard/internal/metrics"
)

// +kubebuilder:rbac:groups=githubguard.sap,resources=genericexternalmemberproviders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=githubguard.sap,resources=genericexternalmemberproviders/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=githubguard.sap,resources=genericexternalmemberproviders/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

type GenericExternalMemberProviderReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *GenericExternalMemberProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	l := log.FromContext(ctx)
	done := ghmetrics.StartReconcileTimer("GenericExternalMemberProvider")
	defer func() {
		result := "success"
		if err != nil {
			result = "error"
		} else if res.RequeueAfter > 0 {
			result = "requeue"
		}
		done(result)
	}()

	emp := &githubguardsapv1.GenericExternalMemberProvider{}
	if err := r.Get(ctx, req.NamespacedName, emp); err != nil {
		if errors.IsNotFound(err) {
			l.Error(err, "resource not found in kubernetes: reconcile is skipped")
			return ctrl.Result{}, nil
		}
		l.Error(err, "error during getting the resource")
		return reconcile.Result{}, err
	}

	// credentials
	var username, password, token string
	if emp.Spec.Secret != "" {
		sec := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: emp.Spec.Secret}, sec); err != nil {
			l.Error(err, "error during getting the secret")
			emp.Status.State = githubguardsapv1.ExternalMemberProviderStateFailed
			emp.Status.Error = fmt.Sprintf("error in getting secret: %v", err)
			emp.Status.Timestamp = metav1.Now()
			if uerr := r.Status().Update(ctx, emp); uerr != nil {
				l.Error(uerr, "error during status update")
				return reconcile.Result{}, uerr
			}
			return reconcile.Result{}, nil
		}
		username = string(sec.Data[githubguardsapv1.SECRET_USERNAME_KEY])
		password = string(sec.Data[githubguardsapv1.SECRET_PASSWORD_KEY])
		token = string(sec.Data["token"]) // optional
	}

	cfg := &genericprovider.HTTPConfig{
		ResultsField:      emp.Spec.ResultsField,
		IDField:           emp.Spec.IDField,
		Paginated:         emp.Spec.Paginated,
		TotalPagesField:   emp.Spec.TotalPagesField,
		PageParam:         emp.Spec.PageParam,
		TestConnectionURL: emp.Spec.TestConnectionURL,
	}
	c := genericprovider.NewHTTPClient(emp.Spec.Endpoint, username, password, token, cfg)

	start := time.Now()
	if err = c.TestConnection(ctx); err != nil {
		ghmetrics.ObserveExternalRequest("generic_http_provider", "test_connection", "error", start)
		l.Error(err, "error during client creation")
		emp.Status.State = githubguardsapv1.ExternalMemberProviderStateFailed
		emp.Status.Error = fmt.Sprintf("error during client creation: %v", err)
		emp.Status.Timestamp = metav1.Now()
		if uerr := r.Status().Update(ctx, emp); uerr != nil {
			l.Error(uerr, "error during status update")
			return reconcile.Result{}, uerr
		}
		return reconcile.Result{}, nil
	}
	ghmetrics.ObserveExternalRequest("generic_http_provider", "test_connection", "success", start)

	GenericHTTPProviders[emp.Name] = c

	// set running
	emp.Status.State = githubguardsapv1.ExternalMemberProviderStateRunning
	emp.Status.Timestamp = metav1.Now()
	if err := r.Status().Update(ctx, emp); err != nil {
		l.Error(err, "error during status update")
		return reconcile.Result{}, err
	}
	l.Info("generic external member provider is configured and running as part of controller")
	return ctrl.Result{}, nil
}

func (r *GenericExternalMemberProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&githubguardsapv1.GenericExternalMemberProvider{}).
		Complete(r)
}
