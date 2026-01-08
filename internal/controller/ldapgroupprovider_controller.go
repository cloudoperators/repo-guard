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

	ldapprovider "github.com/cloudoperators/repo-guard/internal/external-provider/ldap"
	ghmetrics "github.com/cloudoperators/repo-guard/internal/metrics"
)

// LDAPGroupProviderReconciler reconciles a LDAPGroupProvider object
type LDAPGroupProviderReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=githubguard.sap,resources=ldapgroupproviders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=githubguard.sap,resources=ldapgroupproviders/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=githubguard.sap,resources=ldapgroupproviders/finalizers,verbs=update
func (r *LDAPGroupProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	l := log.FromContext(ctx)
	done := ghmetrics.StartReconcileTimer("LDAPGroupProvider")
	defer func() {
		result := "success"
		if err != nil {
			result = "error"
		} else if res.RequeueAfter > 0 {
			result = "requeue"
		}
		done(result)
	}()

	ldap := &githubguardsapv1.LDAPGroupProvider{}
	err = r.Get(ctx, req.NamespacedName, ldap)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Error(err, "resource not found in kubernetes: reconcile is skipped")
			return ctrl.Result{}, nil
		}
		l.Error(err, "error during getting the resource")
		return reconcile.Result{}, err
	}

	// get secret for credentials
	ldapSecret := &corev1.Secret{}
	err = r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: ldap.Spec.Secret}, ldapSecret)
	if err != nil {
		l.Error(err, "error during getting the secret")
		ldap.Status.State = githubguardsapv1.LDAPGroupProviderStateFailed
		ldap.Status.Error = fmt.Sprintf("error in getting secret: %v", err)
		ldap.Status.Timestamp = metav1.Now()
		err := r.Status().Update(ctx, ldap)
		if err != nil {
			l.Error(err, "error during status update")
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	bindDN := string(ldapSecret.Data[githubguardsapv1.SECRET_BIND_DN])
	bindPW := string(ldapSecret.Data[githubguardsapv1.SECRET_BIND_PW])

	c, err := ldapprovider.NewLDAPClient(ldap.Spec.Host, bindDN, bindPW, ldap.Spec.BaseDN)
	if err != nil {
		l.Error(err, "error during client creation")
		ldap.Status.State = githubguardsapv1.LDAPGroupProviderStateFailed
		ldap.Status.Error = fmt.Sprintf("error during client creation: %v", err)
		ldap.Status.Timestamp = metav1.Now()
		updateErr := r.Status().Update(ctx, ldap)
		if updateErr != nil {
			l.Error(updateErr, "error during status update")
			return reconcile.Result{}, updateErr
		}
		return reconcile.Result{}, nil
	}
	// test the connection
	start := time.Now()
	err = c.TestConnection(ctx)
	if err != nil {
		ghmetrics.ObserveExternalRequest("ldap_group_provider", "test_connection", "error", start)
		l.Error(err, "error during client creation")
		ldap.Status.State = githubguardsapv1.LDAPGroupProviderStateFailed
		ldap.Status.Error = fmt.Sprintf("error during client creation: %v", err)
		ldap.Status.Timestamp = metav1.Now()
		updateErr := r.Status().Update(ctx, ldap)
		if updateErr != nil {
			l.Error(updateErr, "error during status update")
			return reconcile.Result{}, updateErr
		}
		return reconcile.Result{}, nil
	}
	ghmetrics.ObserveExternalRequest("ldap_group_provider", "test_connection", "success", start)

	LDAPGroupProviders[ldap.Name] = c

	// update status to running
	ldap.Status.State = githubguardsapv1.LDAPGroupProviderStateRunning
	ldap.Status.Timestamp = metav1.Now()
	err = r.Status().Update(ctx, ldap)
	if err != nil {
		l.Error(err, "error during status update")
		return reconcile.Result{}, err
	}
	l.Info("ldap group provider is configured and running as part of controller")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LDAPGroupProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&githubguardsapv1.LDAPGroupProvider{}).
		Complete(r)
}
