// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	greenhousesapv1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc

	empHTTPServer *empHTTPTestServer
	ldapServer    *ldapTestServer
)

const (
	timeout  = 30 * time.Second
	interval = 1 * time.Second
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

// ---- log capture + dump on failure ----

var (
	logBufMu sync.Mutex
	logBuf   bytes.Buffer
)

type teeWriter struct{}

func (teeWriter) Write(p []byte) (n int, err error) {
	// Always forward to GinkgoWriter.
	_, _ = GinkgoWriter.Write(p)

	// Also capture for failure dump.
	logBufMu.Lock()
	defer logBufMu.Unlock()
	return logBuf.Write(p)
}

func dumpCapturedLogs() {
	logBufMu.Lock()
	defer logBufMu.Unlock()

	if logBuf.Len() == 0 {
		fmt.Fprintln(GinkgoWriter, "\n--- captured controller logs: <empty> ---")
		return
	}

	fmt.Fprintln(GinkgoWriter, "\n--- captured controller logs (begin) ---")
	_, _ = GinkgoWriter.Write(logBuf.Bytes())
	fmt.Fprintln(GinkgoWriter, "\n--- captured controller logs (end) ---")
}

var _ = AfterEach(func() {
	if !CurrentSpecReport().Failed() {
		return
	}

	fmt.Fprintln(GinkgoWriter, "\n================ FAILURE DEBUG DUMP ================")
	dumpCapturedLogs()

	if k8sClient == nil {
		fmt.Fprintln(GinkgoWriter, "k8sClient is nil; cannot dump objects")
		fmt.Fprintln(GinkgoWriter, "====================================================")
		return
	}

	ctx := context.Background()

	var nsList v1.NamespaceList
	if err := k8sClient.List(ctx, &nsList); err == nil {
		fmt.Fprintf(GinkgoWriter, "Namespaces: %d\n", len(nsList.Items))
		for _, n := range nsList.Items {
			fmt.Fprintf(GinkgoWriter, " - %s\n", n.Name)
		}
	}

	var ghList repoguardsapv1.GithubList
	if err := k8sClient.List(ctx, &ghList); err == nil {
		fmt.Fprintf(GinkgoWriter, "Github CRs: %d\n", len(ghList.Items))
		for _, it := range ghList.Items {
			fmt.Fprintf(GinkgoWriter, " - %s state=%s\n", it.Name, it.Status.State)
		}
	}

	var orgList repoguardsapv1.GithubOrganizationList
	if err := k8sClient.List(ctx, &orgList); err == nil {
		fmt.Fprintf(GinkgoWriter, "GithubOrganization CRs: %d\n", len(orgList.Items))
		for _, it := range orgList.Items {
			fmt.Fprintf(GinkgoWriter, " - %s/%s status=%s err=%q\n",
				it.Namespace, it.Name, it.Status.OrganizationStatus, it.Status.OrganizationStatusError)
		}
	}

	var teamList repoguardsapv1.GithubTeamList
	if err := k8sClient.List(ctx, &teamList); err == nil {
		fmt.Fprintf(GinkgoWriter, "GithubTeam CRs: %d\n", len(teamList.Items))
		for _, it := range teamList.Items {
			fmt.Fprintf(GinkgoWriter, " - %s/%s status=%s err=%q members=%d\n",
				it.Namespace, it.Name, it.Status.TeamStatus, it.Status.TeamStatusError, len(it.Status.Members))
		}
	}

	fmt.Fprintln(GinkgoWriter, "====================================================")
})

// ---- suite bootstrap ----

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(teeWriter{}), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.Background())

	TEST_ENV = loadTestEnv()

	seed := int64(GinkgoRandomSeed())
	rand.Seed(seed)
	fmt.Fprintf(GinkgoWriter, "Ginkgo random seed: %d\n", seed)

	// Start dummy external services first, because initSharedResources() consumes
	// values from TEST_ENV (e.g. LDAP host, EMP HTTP endpoint) to build fixtures.
	startDummyExternalSystems()
	initSharedResources()

	// controllers use production global OperatorNamespace; assign, don't redeclare
	OperatorNamespace = TestOperatorNamespace

	By("bootstrapping envtest")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
			filepath.Join("..", "..", "config", "crd", "external"),
		},
		ErrorIfCRDPathMissing:    true,
		ControlPlaneStartTimeout: 30 * time.Second,
		ControlPlaneStopTimeout:  30 * time.Second,
		AttachControlPlaneOutput: false,
		BinaryAssetsDirectory:    os.Getenv("KUBEBUILDER_ASSETS"),
		UseExistingCluster:       ptrBool(false),
	}

	// Keep envtest strictly local.
	testEnv.ControlPlane.GetAPIServer().Configure().Set("bind-address", "127.0.0.1")
	testEnv.ControlPlane.GetAPIServer().Configure().Set("service-cluster-ip-range", "10.0.0.0/24")

	var err error
	cfg, err = startEnvtestWithRetry(testEnv, 2, 700*time.Millisecond)
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	Expect(repoguardsapv1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(greenhousesapv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	Expect(ensureNamespace(context.Background(), TestOperatorNamespace)).To(Succeed())
	Expect(ensureNamespace(context.Background(), nonEmpty(TEST_ENV["NAMESPACE"], "default"))).To(Succeed())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())

	Expect((&GithubReconciler{Client: k8sManager.GetClient()}).SetupWithManager(k8sManager)).To(Succeed())
	Expect((&GithubOrganizationReconciler{Client: k8sManager.GetClient()}).SetupWithManager(k8sManager)).To(Succeed())
	Expect((&GithubTeamReconciler{Client: k8sManager.GetClient()}).SetupWithManager(k8sManager)).To(Succeed())
	Expect((&LDAPGroupProviderReconciler{Client: k8sManager.GetClient()}).SetupWithManager(k8sManager)).To(Succeed())
	Expect((&GenericExternalMemberProviderReconciler{Client: k8sManager.GetClient()}).SetupWithManager(k8sManager)).To(Succeed())
	Expect((&StaticMemberProviderReconciler{Client: k8sManager.GetClient()}).SetupWithManager(k8sManager)).To(Succeed())

	started := make(chan struct{})
	go func() {
		defer GinkgoRecover()
		close(started)
		err := k8sManager.Start(ctx)
		if ctx.Err() == nil {
			Expect(err).To(Succeed(), "controller manager exited unexpectedly")
		}
	}()

	By("waiting for controller-runtime cache to sync")
	Eventually(func() bool {
		var nsList v1.NamespaceList
		return k8sClient.List(context.Background(), &nsList) == nil
	}, 3*timeout, 250*time.Millisecond).Should(BeTrue())

	Eventually(func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	}, 2*time.Second, 50*time.Millisecond).Should(BeTrue())
})

var _ = AfterSuite(func() {
	if cancel != nil {
		cancel()
	}

	By("tearing down envtest")
	if testEnv != nil {
		Expect(testEnv.Stop()).To(Succeed())
	}
	if empHTTPServer != nil {
		empHTTPServer.Close()
	}
	if ldapServer != nil {
		ldapServer.Close()
	}
})

func startDummyExternalSystems() {
	By("starting dummy EMP HTTP and LDAP servers")

	username := TEST_ENV["EMP_HTTP_USERNAME"]
	password := TEST_ENV["EMP_HTTP_PASSWORD"]
	groupID := TEST_ENV["EMP_HTTP_GROUP_ID"]
	userID := TEST_ENV["EMP_HTTP_USER_INTERNAL_USERNAME"]

	empHTTPServer = newEMPHTTPTestServer(username, password, groupID, userID)
	base := empHTTPServer.baseURL()
	TEST_ENV["EMP_HTTP_ENDPOINT"] = fmt.Sprintf("%s/api/sp/groups/{group}/users.json", base)
	TEST_ENV["EMP_HTTP_TEST_CONNECTION_URL"] = fmt.Sprintf("%s/api/sp/search.json", base)

	ldapBaseDN := TEST_ENV["LDAP_GROUP_PROVIDER_BASE_DN"]
	ldapBindDN := TEST_ENV["LDAP_GROUP_PROVIDER_BIND_DN"]
	ldapBindPW := TEST_ENV["LDAP_GROUP_PROVIDER_BIND_PW"]
	ldapGroup := TEST_ENV["LDAP_GROUP_PROVIDER_GROUP_NAME"]
	ldapUser := TEST_ENV["LDAP_GROUP_PROVIDER_USER_INTERNAL_USERNAME"]

	ldapServer = newLDAPTestServer(ldapBindDN, ldapBindPW, ldapBaseDN, ldapGroup, []string{ldapUser})
	TEST_ENV["LDAP_GROUP_PROVIDER_HOST"] = fmt.Sprintf("ldap://%s", ldapServer.host())
}

func startEnvtestWithRetry(env *envtest.Environment, attempts int, backoff time.Duration) (*rest.Config, error) {
	if env == nil {
		return nil, fmt.Errorf("envtest env is nil")
	}
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		cfg, err := env.Start()
		if err == nil && cfg != nil {
			return cfg, nil
		}
		lastErr = err
		_ = env.Stop()
		time.Sleep(backoff)
		backoff *= 2
	}
	return nil, lastErr
}

func ensureNamespace(ctx context.Context, name string) error {
	name = nonEmpty(name, "")
	if name == "" {
		return nil
	}
	nsObj := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	err := k8sClient.Create(ctx, nsObj)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func ensureResourceCreated(ctx context.Context, obj client.Object) error {
	if obj == nil {
		return nil
	}
	err := k8sClient.Create(ctx, obj)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func ptrBool(v bool) *bool { return &v }
