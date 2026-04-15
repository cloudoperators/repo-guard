// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	genericprovider "github.com/cloudoperators/repo-guard/internal/external-provider/static"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	ghmetrics "github.com/cloudoperators/repo-guard/internal/metrics"
)

type StaticMemberProviderReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *StaticMemberProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	l := log.FromContext(ctx)
	done := ghmetrics.StartReconcileTimer("StaticMemberProvider")
	defer func() {
		result := "success"
		if err != nil {
			result = "error"
		} else if res.RequeueAfter > 0 {
			result = "requeue"
		}
		done(result)
	}()

	emp := &repoguardsapv1.StaticMemberProvider{}
	if err = r.Get(ctx, req.NamespacedName, emp); err != nil {
		// Let controller-runtime handle notfound logging similar to other controllers
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	groups := map[string][]string{}
	for _, g := range emp.Spec.Groups {
		groups[g.Group] = append([]string{}, g.Members...)
	}
	c := genericprovider.NewStaticClient(groups)

	StaticProviders.Store(types.NamespacedName{Name: emp.Name, Namespace: emp.Namespace}, c)

	emp.Status.State = repoguardsapv1.ExternalMemberProviderStateRunning
	emp.Status.Timestamp = metav1.Now()
	if err := r.Status().Update(ctx, emp); err != nil {
		l.Error(err, "error during status update")
		return ctrl.Result{}, err
	}
	l.Info("static member provider is configured and running as part of controller")
	return ctrl.Result{}, nil
}

func (r *StaticMemberProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&repoguardsapv1.StaticMemberProvider{}).
		Complete(r)
}

type ClusterStaticMemberProviderReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *ClusterStaticMemberProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	l := log.FromContext(ctx)
	done := ghmetrics.StartReconcileTimer("ClusterStaticMemberProvider")
	defer func() {
		result := "success"
		if err != nil {
			result = "error"
		} else if res.RequeueAfter > 0 {
			result = "requeue"
		}
		done(result)
	}()

	emp := &repoguardsapv1.ClusterStaticMemberProvider{}
	if err = r.Get(ctx, req.NamespacedName, emp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	groups := map[string][]string{}
	for _, g := range emp.Spec.Groups {
		groups[g.Group] = append([]string{}, g.Members...)
	}
	c := genericprovider.NewStaticClient(groups)

	StaticProviders.Store(types.NamespacedName{Name: emp.Name}, c)

	emp.Status.State = repoguardsapv1.ExternalMemberProviderStateRunning
	emp.Status.Timestamp = metav1.Now()
	if err := r.Status().Update(ctx, emp); err != nil {
		l.Error(err, "error during status update")
		return ctrl.Result{}, err
	}
	l.Info("cluster static member provider is configured and running as part of controller")
	return ctrl.Result{}, nil
}

func (r *ClusterStaticMemberProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&repoguardsapv1.ClusterStaticMemberProvider{}).
		Complete(r)
}
