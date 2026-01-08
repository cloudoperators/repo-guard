package controller

import (
	"context"
	"time"

	githubguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	githubAPI "github.com/google/go-github/v81/github"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("GithubTeam TTL labels maintenance", Ordered, func() {

	BeforeAll(func() {
		// Ensure GitHub and Organization prerequisites exist and are ready
		ctx := context.Background()
		github := githubCom.DeepCopy()
		secret := githubComSecret.DeepCopy()
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		Expect(k8sClient.Create(ctx, github)).Should(Succeed())

		org := githubOrganizationGreenhouseSandboxForTeamTests.DeepCopy()
		Expect(k8sClient.Create(ctx, org)).Should(Succeed())

		Eventually(func() githubguardsapv1.GithubState {
			g := &githubguardsapv1.Github{}
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"]}, g)
			Expect(err).NotTo(HaveOccurred())
			return g.Status.State
		}, timeout, interval).Should(BeEquivalentTo(githubguardsapv1.GithubStateRunning))

		Eventually(func() githubguardsapv1.GithubOrganizationState {
			o := &githubguardsapv1.GithubOrganization{}
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["ORGANIZATION_KUBERNETES_RESOURCE_NAME"]}, o)
			Expect(err).NotTo(HaveOccurred())
			return o.Status.OrganizationStatus
		}, timeout, interval).Should(BeEquivalentTo(githubguardsapv1.GithubOrganizationStateComplete))
	})

	AfterAll(func() {

		ctx := context.Background()

		org := githubOrganizationGreenhouseSandboxForTeamTests.DeepCopy()
		Expect(k8sClient.Delete(ctx, org)).Should(Succeed())

		github := githubCom.DeepCopy()
		secret := githubComSecret.DeepCopy()
		Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, github)).Should(Succeed())

		client := githubAPI.NewClient(nil).WithAuthToken(TEST_ENV["GITHUB_TOKEN"])
		deleteIfExists := func(slug string) {
			resp, err := client.Teams.DeleteTeamBySlug(ctx, TEST_ENV["ORGANIZATION"], slug)
			if err != nil {
				// treat 404 as already deleted and not an error for cleanup
				if resp != nil && resp.StatusCode == 404 {
					return
				}
				Expect(err).NotTo(HaveOccurred())
			} else {
				Expect(resp.StatusCode).To(Equal(204))
			}
		}
		deleteIfExists("team-failed-ttl")
		deleteIfExists("team-completed-ttl")
		deleteIfExists("team-notfound-ttl")
		deleteIfExists("team-skipped-ttl")
	})

	It("clears failed status and failed user operations after failedTTL expires", func() {
		ctx := context.Background()
		name := "team-failed-ttl"

		t := &githubguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: TEST_ENV["NAMESPACE"],
				Labels: map[string]string{
					GITHUB_TEAM_LABEL_FAILED_TTL: "1s",
					"githubguard.sap/addUser":    "false",
					"githubguard.sap/removeUser": "false",
				},
			},
			Spec: githubguardsapv1.GithubTeamSpec{
				Github:       TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
				Organization: TEST_ENV["ORGANIZATION"],
				Team:         "ttl-team",
			},
			Status: githubguardsapv1.GithubTeamStatus{
				TeamStatus:          githubguardsapv1.GithubTeamStateFailed,
				TeamStatusError:     "some failure",
				TeamStatusTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Second)),
				Operations: []githubguardsapv1.GithubUserOperation{{
					Operation: githubguardsapv1.GithubUserOperationTypeAdd,
					User:      "user1",
					State:     githubguardsapv1.GithubUserOperationStateFailed,
					Timestamp: metav1.Now(),
				}},
			},
		}
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		// concurrent controller updates may cause conflicts; retry until it succeeds
		Eventually(func() error {
			latest := &githubguardsapv1.GithubTeam{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: name}, latest); err != nil {
				return err
			}
			latest.Status = t.Status
			return k8sClient.Status().Update(ctx, latest)
		}, timeout, interval).Should(Succeed())

		Eventually(func() string {
			current := &githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: name}, current)).To(Succeed())
			return current.Status.TeamStatusError
		}, timeout, interval).Should(BeEmpty())

		Eventually(func() bool {
			current := &githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: name}, current)).To(Succeed())
			for _, op := range current.Status.Operations {
				if op.State == githubguardsapv1.GithubUserOperationStateFailed {
					return false
				}
			}
			return true
		}, timeout, interval).Should(BeTrue())

		Expect(k8sClient.Delete(ctx, t)).To(Succeed())

	})

	It("clears completed user operations after completedTTL expires", func() {
		ctx := context.Background()
		name := "team-completed-ttl"

		t := &githubguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: TEST_ENV["NAMESPACE"],
				Labels: map[string]string{
					GITHUB_TEAM_LABEL_COMPLETED_TTL: "1s",
					"githubguard.sap/addUser":       "false",
					"githubguard.sap/removeUser":    "false",
				},
			},
			Spec: githubguardsapv1.GithubTeamSpec{
				Github:       TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
				Organization: TEST_ENV["ORGANIZATION"],
				Team:         "ttl-team-2",
			},
			Status: githubguardsapv1.GithubTeamStatus{
				TeamStatus:          githubguardsapv1.GithubTeamStateComplete,
				TeamStatusTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Second)),
				Operations: []githubguardsapv1.GithubUserOperation{{
					Operation: githubguardsapv1.GithubUserOperationTypeAdd,
					User:      "user2",
					State:     githubguardsapv1.GithubUserOperationStateComplete,
					Timestamp: metav1.Now(),
				}},
			},
		}
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		Eventually(func() error {
			latest := &githubguardsapv1.GithubTeam{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: name}, latest); err != nil {
				return err
			}
			latest.Status = t.Status
			return k8sClient.Status().Update(ctx, latest)
		}, timeout, interval).Should(Succeed())

		Eventually(func() int {
			current := &githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: name}, current)).To(Succeed())
			return len(current.Status.Operations)
		}, timeout, interval).Should(Equal(0))

		Expect(k8sClient.Delete(ctx, t)).To(Succeed())

	})

	It("clears notfound user operations after notfoundTTL expires", func() {
		ctx := context.Background()
		name := "team-notfound-ttl"

		t := &githubguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: TEST_ENV["NAMESPACE"],
				Labels: map[string]string{
					GITHUB_TEAM_LABEL_NOTFOUND_TTL: "1s",
					"githubguard.sap/addUser":      "false",
					"githubguard.sap/removeUser":   "false",
				},
			},
			Spec: githubguardsapv1.GithubTeamSpec{
				Github:       TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
				Organization: TEST_ENV["ORGANIZATION"],
				Team:         "ttl-team-3",
			},
			Status: githubguardsapv1.GithubTeamStatus{
				TeamStatus:          githubguardsapv1.GithubTeamStateComplete,
				TeamStatusTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Second)),
				Operations: []githubguardsapv1.GithubUserOperation{{
					Operation: githubguardsapv1.GithubUserOperationTypeAdd,
					User:      "user3",
					State:     githubguardsapv1.GithubUserOperationStateNotFound,
					Timestamp: metav1.Now(),
				}},
			},
		}
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		Eventually(func() error {
			latest := &githubguardsapv1.GithubTeam{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: name}, latest); err != nil {
				return err
			}
			latest.Status = t.Status
			return k8sClient.Status().Update(ctx, latest)
		}, timeout, interval).Should(Succeed())

		Eventually(func() int {
			current := &githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: name}, current)).To(Succeed())
			return len(current.Status.Operations)
		}, timeout, interval).Should(Equal(0))

		Expect(k8sClient.Delete(ctx, t)).To(Succeed())

	})

	It("clears skipped user operations after skippedTTL expires", func() {
		ctx := context.Background()
		name := "team-skipped-ttl"

		t := &githubguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: TEST_ENV["NAMESPACE"],
				Labels: map[string]string{
					GITHUB_TEAM_LABEL_SKIPPED_TTL: "1s",
					"githubguard.sap/addUser":     "false",
					"githubguard.sap/removeUser":  "false",
				},
			},
			Spec: githubguardsapv1.GithubTeamSpec{
				Github:       TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"],
				Organization: TEST_ENV["ORGANIZATION"],
				Team:         "ttl-team-4",
			},
			Status: githubguardsapv1.GithubTeamStatus{
				TeamStatus:          githubguardsapv1.GithubTeamStateComplete,
				TeamStatusTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Second)),
				Operations: []githubguardsapv1.GithubUserOperation{{
					Operation: githubguardsapv1.GithubUserOperationTypeAdd,
					User:      "user4",
					State:     githubguardsapv1.GithubUserOperationStateSkipped,
					Timestamp: metav1.Now(),
				}},
			},
		}
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		Eventually(func() error {
			latest := &githubguardsapv1.GithubTeam{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: name}, latest); err != nil {
				return err
			}
			latest.Status = t.Status
			return k8sClient.Status().Update(ctx, latest)
		}, timeout, interval).Should(Succeed())

		Eventually(func() int {
			current := &githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: name}, current)).To(Succeed())
			return len(current.Status.Operations)
		}, timeout, interval).Should(Equal(0))

		Expect(k8sClient.Delete(ctx, t)).To(Succeed())
	})
})
