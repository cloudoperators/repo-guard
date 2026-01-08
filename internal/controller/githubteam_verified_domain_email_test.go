package controller

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	greenhousesapv1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	githubguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// This test validates GithubTeam member filtering when a domain-valued label
// is set and GithubAccountLink resources carry multi-org email check results
// as annotations. The GithubAccountLink reconciler is intentionally NOT running
// in the suite; we fabricate annotations directly in the test.
var _ = Describe("Github Team verified domain email filtering", Ordered, func() {

	// Use dedicated resource names to avoid interfering with other tests
	const (
		teamName       = "team-verified-domain"
		ghTeamK8sName  = "com--greenhouse-sandbox--team-verified-domain"
		domainTestCom  = "test.com"
		domainOtherCom = "other.com"
	)

	var (
		// computed names
		ns         = TEST_ENV["NAMESPACE"]
		githubName = TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"]
		secretName = TEST_ENV["GITHUB_KUBERNETES_SECRET_RESOURCE_NAME"]
		orgK8sName = TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"] + "--" + TEST_ENV["ORGANIZATION"]

		githubTeam = &githubguardsapv1.GithubTeam{}

		galUser0 = &githubguardsapv1.GithubAccountLink{}
		galUser1 = &githubguardsapv1.GithubAccountLink{}
	)

	BeforeAll(func() {
		ctx := context.Background()

		// Secret for the dedicated Github instance
		secret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: ns,
			},
			StringData: map[string]string{
				"clientID":     TEST_ENV["GITHUB_CLIENT_ID"],
				"clientSecret": TEST_ENV["GITHUB_CLIENT_SECRET"],
				"privateKey":   TEST_ENV["GITHUB_PRIVATE_KEY"],
			},
		}
		if err := k8sClient.Create(ctx, secret); err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}

		// Github instance
		integrationID, err := strconv.ParseInt(TEST_ENV["GITHUB_INTEGRATION_ID"], 10, 64)
		Expect(err).NotTo(HaveOccurred())
		gh := &githubguardsapv1.Github{
			ObjectMeta: metav1.ObjectMeta{
				Name:      githubName,
				Namespace: ns,
			},
			Spec: githubguardsapv1.GithubSpec{
				WebURL:          TEST_ENV["GITHUB_WEB_URL"],
				V3APIURL:        TEST_ENV["GITHUB_V3_API_URL"],
				IntegrationID:   integrationID,
				ClientUserAgent: "greenhouse-github-guard",
				Secret:          secretName,
			},
		}
		if err := k8sClient.Create(ctx, gh); err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}

		// Organization under that Github
		installationID, err := strconv.ParseInt(TEST_ENV["GITHUB_INSTALLATION_ID"], 10, 64)
		Expect(err).NotTo(HaveOccurred())
		org := &githubguardsapv1.GithubOrganization{
			ObjectMeta: metav1.ObjectMeta{
				Name:      orgK8sName,
				Namespace: ns,
				Labels: map[string]string{
					"githubguard.sap/addOrganizationOwner":    "false",
					"githubguard.sap/removeOrganizationOwner": "false",
					"githubguard.sap/addTeam":                 "false",
					"githubguard.sap/removeTeam":              "false",
					"githubguard.sap/addRepositoryTeam":       "false",
					"githubguard.sap/removeRepositoryTeam":    "false",
				},
			},
			Spec: githubguardsapv1.GithubOrganizationSpec{
				Github:         githubName,
				Organization:   TEST_ENV["ORGANIZATION"],
				InstallationID: installationID,
			},
		}
		if err := k8sClient.Create(ctx, org); err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}

		// Wait for Github and Org readiness
		Eventually(func() githubguardsapv1.GithubState {
			cur := &githubguardsapv1.Github{}
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: githubName}, cur)
			Expect(err).NotTo(HaveOccurred())
			return cur.Status.State
		}, timeout, interval).Should(BeEquivalentTo(githubguardsapv1.GithubStateRunning))

		Eventually(func() githubguardsapv1.GithubOrganizationState {
			cur := &githubguardsapv1.GithubOrganization{}
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: orgK8sName}, cur)
			Expect(err).NotTo(HaveOccurred())

			return cur.Status.OrganizationStatus
		}, timeout, interval).Should(BeEquivalentTo(githubguardsapv1.GithubOrganizationStateComplete))

		// Seed GithubAccountLinks with fabricated email-check results annotations
		now := time.Now().Format(time.RFC3339)
		resultsUser1 := map[string]map[string]interface{}{
			TEST_ENV["ORGANIZATION"]: {"domain": domainTestCom, "verified": true, "timestamp": now},
		}
		resultsUser0 := map[string]map[string]interface{}{
			TEST_ENV["ORGANIZATION"]: {"domain": domainTestCom, "verified": false, "timestamp": now},
		}
		res1, _ := json.Marshal(resultsUser1)
		res0, _ := json.Marshal(resultsUser0)

		galUser0 = &githubguardsapv1.GithubAccountLink{
			ObjectMeta: metav1.ObjectMeta{
				Name:      githubName + "-" + "user0",
				Namespace: ns,
				Annotations: map[string]string{
					githubguardsapv1.GITHUB_ACCOUNT_LINK_EMAIL_CHECK_RESULTS: string(res0),
				},
			},
			Spec: githubguardsapv1.GithubAccountLinkSpec{
				GreenhouseUserID: TEST_ENV["USER_0_GREENHOUSE_ID"],
				GithubUserID:     TEST_ENV["USER_0_GITHUB_USERID"],
				Github:           githubName,
			},
		}
		galUser1 = &githubguardsapv1.GithubAccountLink{
			ObjectMeta: metav1.ObjectMeta{
				Name:      githubName + "-" + "user1",
				Namespace: ns,
				Annotations: map[string]string{
					githubguardsapv1.GITHUB_ACCOUNT_LINK_EMAIL_CHECK_RESULTS: string(res1),
				},
			},
			Spec: githubguardsapv1.GithubAccountLinkSpec{
				GreenhouseUserID: TEST_ENV["USER_1_GREENHOUSE_ID"],
				GithubUserID:     TEST_ENV["USER_1_GITHUB_USERID"],
				Github:           githubName,
			},
		}
		Expect(k8sClient.Create(ctx, galUser0)).Should(Succeed())
		Expect(k8sClient.Create(ctx, galUser1)).Should(Succeed())

		// Ensure links are in cache
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: galUser0.Name}, &githubguardsapv1.GithubAccountLink{})
		}, timeout, interval).Should(Succeed())
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: galUser1.Name}, &githubguardsapv1.GithubAccountLink{})
		}, timeout, interval).Should(Succeed())

		// Create the Greenhouse Team with both users as members
		greenhouseTeam := &greenhousesapv1alpha1.Team{
			ObjectMeta: metav1.ObjectMeta{
				Name:      teamName,
				Namespace: ns,
			},
		}
		Expect(k8sClient.Create(ctx, greenhouseTeam)).Should(Succeed())

		// update greenhouse team status - members
		greenhouseTeamToStatusUpdate := &greenhousesapv1alpha1.Team{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: teamName}, greenhouseTeamToStatusUpdate)
		}, timeout, interval).Should(Succeed())
		greenhouseTeamToStatusUpdate.Status = greenhousesapv1alpha1.TeamStatus{
			Members: []greenhousesapv1alpha1.User{
				{ID: TEST_ENV["USER_0_GREENHOUSE_ID"], Email: "user0@example.com", FirstName: "User0", LastName: "Test"},
				{ID: TEST_ENV["USER_1_GREENHOUSE_ID"], Email: "user1@example.com", FirstName: "User1", LastName: "Test"},
			},
		}
		Expect(k8sClient.Status().Update(ctx, greenhouseTeamToStatusUpdate)).Should(Succeed())

		Eventually(func() int {
			ghTeamToStatusCheck := &greenhousesapv1alpha1.Team{}
			_ = k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: teamName}, ghTeamToStatusCheck)
			return len(ghTeamToStatusCheck.Status.Members)
		}, timeout, interval).Should(Equal(2))

	})

	AfterAll(func() {
		ctx := context.Background()

		// Cleanup GithubTeam
		_ = k8sClient.Delete(ctx, githubTeam)
		// Cleanup Greenhouse team
		//_ = k8sClient.Delete(ctx, greenhouseTeam)
		// Cleanup account links
		_ = k8sClient.Delete(ctx, galUser0)
		_ = k8sClient.Delete(ctx, galUser1)

		// Delete org, github and secret created in this test file
		org := &githubguardsapv1.GithubOrganization{}
		_ = k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: orgK8sName}, org)
		if org.Name != "" {
			_ = k8sClient.Delete(ctx, org)
		}
		gh := &githubguardsapv1.Github{}
		_ = k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: githubName}, gh)
		if gh.Name != "" {
			_ = k8sClient.Delete(ctx, gh)
		}
		secret := &v1.Secret{}
		_ = k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: secretName}, secret)
		if secret.Name != "" {
			_ = k8sClient.Delete(ctx, secret)
		}

		test1 := &greenhousesapv1alpha1.Team{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "team-verified-domain"}, test1)).Should(Succeed())

		test2 := &githubguardsapv1.GithubAccountLinkList{}
		Expect(k8sClient.List(ctx, test2)).Should(Succeed())

	})

	It("filters members by domain using results annotation (test.com)", func() {

		// Create GithubTeam requiring verified domain email for test.com
		githubTeam = &githubguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ghTeamK8sName,
				Namespace: ns,
				Labels: map[string]string{
					"githubguard.sap/addUser":                        "true",
					"githubguard.sap/removeUser":                     "true",
					GITHUB_TEAMS_LABEL_REQUIRE_VERIFIED_DOMAIN_EMAIL: domainTestCom,
				},
			},
			Spec: githubguardsapv1.GithubTeamSpec{
				Github:         githubName,
				Organization:   TEST_ENV["ORGANIZATION"],
				Team:           teamName,
				GreenhouseTeam: teamName,
			},
		}
		Expect(k8sClient.Create(ctx, githubTeam)).Should(Succeed())

		ctx := context.Background()
		Eventually(func() ([]githubguardsapv1.Member, error) {
			gt := &githubguardsapv1.GithubTeam{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: ghTeamK8sName}, gt); err != nil {
				return nil, err
			}
			ght := &greenhousesapv1alpha1.Team{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "team-verified-domain"}, ght); err != nil {
				return nil, err
			}
			return gt.Status.Members, nil
		}, timeout, interval).Should(HaveLen(1))

		// Validate the single included member is USER_1 (verified for test.com)
		gt := &githubguardsapv1.GithubTeam{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: ghTeamK8sName}, gt)).Should(Succeed())
		Expect(gt.Status.Members[0].GreenhouseID).To(Equal(TEST_ENV["USER_1_GREENHOUSE_ID"]))
		// GithubUsername should be resolved (non-empty)
		Expect(gt.Status.Members[0].GithubUsername).NotTo(BeEmpty())
	})

	It("updates membership when domain label changes (other.com -> none verified)", func() {
		ctx := context.Background()

		// Patch team label to other.com which is not verified for either user yet
		Eventually(func() error {
			gt := &githubguardsapv1.GithubTeam{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: ghTeamK8sName}, gt); err != nil {
				return err
			}
			if gt.Labels == nil {
				gt.Labels = map[string]string{}
			}
			gt.Labels[GITHUB_TEAMS_LABEL_REQUIRE_VERIFIED_DOMAIN_EMAIL] = domainOtherCom
			return k8sClient.Update(ctx, gt)
		}, timeout, interval).Should(Succeed())

		Eventually(func() int {
			cur := &githubguardsapv1.GithubTeam{}
			_ = k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: ghTeamK8sName}, cur)
			return len(cur.Status.Members)
		}, timeout, interval).Should(Equal(0))
	})

	It("includes a user once their results annotation shows verified for other.com", func() {
		ctx := context.Background()

		Eventually(func() error {
			g0 := &githubguardsapv1.GithubAccountLink{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: galUser0.Name}, g0); err != nil {
				return err
			}

			// Merge/extend existing results JSON
			results := map[string]map[string]interface{}{}
			if raw := g0.Annotations[githubguardsapv1.GITHUB_ACCOUNT_LINK_EMAIL_CHECK_RESULTS]; raw != "" {
				_ = json.Unmarshal([]byte(raw), &results)
			}
			results[TEST_ENV["ORGANIZATION"]] = map[string]interface{}{
				"domain":    domainOtherCom,
				"verified":  true,
				"timestamp": time.Now().Format(time.RFC3339),
			}
			b, _ := json.Marshal(results)
			if g0.Annotations == nil {
				g0.Annotations = map[string]string{}
			}
			g0.Annotations[githubguardsapv1.GITHUB_ACCOUNT_LINK_EMAIL_CHECK_RESULTS] = string(b)
			return k8sClient.Update(ctx, g0)
		}, timeout, interval).Should(Succeed())

		// Expect USER_0 to be included now (single member)
		Eventually(func() ([]githubguardsapv1.Member, error) {
			cur := &githubguardsapv1.GithubTeam{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: ghTeamK8sName}, cur); err != nil {
				return nil, err
			}
			return cur.Status.Members, nil
		}, timeout, interval).Should(HaveLen(1))

		cur := &githubguardsapv1.GithubTeam{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: ghTeamK8sName}, cur)).Should(Succeed())
		Expect(cur.Status.Members[0].GreenhouseID).To(Equal(TEST_ENV["USER_0_GREENHOUSE_ID"]))
	})

})
