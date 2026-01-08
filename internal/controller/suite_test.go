package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	// +kubebuilder:scaffold:imports
	greenhousesapv1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	githubguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
)

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
	// dummy EMP HTTP server for tests
	empHTTPServer *empHTTPTestServer
	// dummy LDAP server for tests
	ldapServer *ldapTestServer
)

const (
	timeout  = time.Second * 60
	interval = time.Second
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	ctx, cancel = context.WithCancel(t.Context())

	RunSpecs(t, "Controller Suite")

}

var _ = BeforeSuite(func() {

	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	assets := os.Getenv("KUBEBUILDER_ASSETS")
	fmt.Fprintf(GinkgoWriter, "KUBEBUILDER_ASSETS: %s\n", assets)
	if assets != "" {
		files, _ := os.ReadDir(assets)
		fmt.Fprintf(GinkgoWriter, "Assets in %s:\n", assets)
		for _, f := range files {
			fmt.Fprintf(GinkgoWriter, "  - %s\n", f.Name())
		}
	}

	// Dummy mode
	username := TEST_ENV["EMP_HTTP_USERNAME"]
	password := TEST_ENV["EMP_HTTP_PASSWORD"]
	// Prefer dummy-specific overrides, fall back to generic if not provided
	groupID := TEST_ENV["EMP_HTTP_GROUP_ID"]
	userID := TEST_ENV["EMP_HTTP_USER_INTERNAL_USERNAME"]
	empHTTPServer = newEMPHTTPTestServer(username, password, groupID, userID)

	// Override endpoints and credentials to point to local server
	base := empHTTPServer.baseURL()
	TEST_ENV["EMP_HTTP_ENDPOINT"] = fmt.Sprintf("%s/api/sp/groups/{group}/users.json", base)
	TEST_ENV["EMP_HTTP_TEST_CONNECTION_URL"] = fmt.Sprintf("%s/api/sp/search.json", base)
	// Also update already-initialized test fixtures that copied old TEST_ENV values at package init
	if empHTTP != nil {
		empHTTP.Spec.Endpoint = TEST_ENV["EMP_HTTP_ENDPOINT"]
		empHTTP.Spec.TestConnectionURL = TEST_ENV["EMP_HTTP_TEST_CONNECTION_URL"]
	}

	// Start local LDAP test server and override related TEST_ENV values
	ldapBaseDN := TEST_ENV["LDAP_GROUP_PROVIDER_BASE_DN"]
	ldapBindDN := TEST_ENV["LDAP_GROUP_PROVIDER_BIND_DN"]
	ldapBindPW := TEST_ENV["LDAP_GROUP_PROVIDER_BIND_PW"]
	ldapGroup := TEST_ENV["LDAP_GROUP_PROVIDER_GROUP_NAME"]
	ldapUser := TEST_ENV["LDAP_GROUP_PROVIDER_USER_INTERNAL_USERNAME"]
	ldapServer = newLDAPTestServer(ldapBindDN, ldapBindPW, ldapBaseDN, ldapGroup, []string{ldapUser})
	// Point host to local plaintext ldap server
	TEST_ENV["LDAP_GROUP_PROVIDER_HOST"] = fmt.Sprintf("ldap://%s", ldapServer.host())
	// Update already-initialized LDAP fixtures
	if ldapGroupProvider != nil {
		ldapGroupProvider.Spec.Host = TEST_ENV["LDAP_GROUP_PROVIDER_HOST"]
		ldapGroupProvider.Spec.BaseDN = ldapBaseDN
	}
	if ldapGroupProviderSecret != nil {
		ldapGroupProviderSecret.StringData["bindDN"] = ldapBindDN
		ldapGroupProviderSecret.StringData["bindPW"] = ldapBindPW
	}

	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "config", "crd", "bases"), filepath.Join("..", "..", "config", "crd", "external")}, ErrorIfCRDPathMissing: true,
		ControlPlaneStartTimeout: 120 * time.Second,
	}

	// Force IPv4 for kube-apiserver to avoid issues in environments where IPv6 is partially enabled (like act)
	testEnv.ControlPlane.GetAPIServer().Configure().Set("bind-address", "127.0.0.1")
	testEnv.ControlPlane.GetAPIServer().Configure().Set("service-cluster-ip-range", "10.0.0.0/24")

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = githubguardsapv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = greenhousesapv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	err = (&GithubReconciler{
		Client: k8sManager.GetClient(),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&GithubOrganizationReconciler{
		Client: k8sManager.GetClient(),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&GithubTeamReconciler{
		Client: k8sManager.GetClient(),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&LDAPGroupProviderReconciler{
		Client: k8sManager.GetClient(),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	/* Disabled intentionally since it requires an enterprise GitHub organization to check verified domain emails
	err = (&GithubAccountLinkReconciler{
		Client: k8sManager.GetClient(),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())
	*/
	err = (&GenericExternalMemberProviderReconciler{
		Client: k8sManager.GetClient(),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&StaticMemberProviderReconciler{
		Client: k8sManager.GetClient(),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
	if empHTTPServer != nil {
		empHTTPServer.Close()
	}
	if ldapServer != nil {
		ldapServer.Close()
	}
})
