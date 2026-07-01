---
layout: home

hero:
  name: Repo Guard
  text: GitHub Organization Management on Autopilot
  tagline: A Kubernetes operator that automates GitHub teams, memberships, repository permissions, and org ownership via GitOps.
  actions:
    - theme: brand
      text: Get Started
      link: /guide/getting-started
    - theme: alt
      text: View on GitHub
      link: https://github.com/cloudoperators/repo-guard

features:
  - icon: 🤖
    title: GitOps-driven
    details: Declare desired GitHub state as Kubernetes Custom Resources. The operator continuously reconciles reality to match.
  - icon: 🔌
    title: Pluggable Identity Sources
    details: Sync team membership from LDAP/AD, HTTP APIs, static lists, or Greenhouse — namespaced or cluster-wide.
  - icon: 🛡️
    title: Safety Rails Built-in
    details: Dry-run mode, label-gated mutations, rate-limit handling, and protected-member lists prevent accidental changes.
  - icon: 📊
    title: Full Observability
    details: Prometheus metrics, PromQL examples, Perses dashboards, and bundled alerting rules ship out of the box.
  - icon: 🏢
    title: Multi-org Ready
    details: Manage multiple GitHub organizations (cloud and enterprise) from a single operator deployment.
  - icon: ✉️
    title: Email Verification
    details: Enforce verified domain-email requirements per org via GithubAccountLink — no more unverified members.
---
