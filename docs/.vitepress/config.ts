import { defineConfig } from 'vitepress'
import { withMermaid } from 'vitepress-plugin-mermaid'

export default withMermaid(defineConfig({
  title: 'Repo Guard',
  description: 'Kubernetes operator for automated GitHub organization management',
  base: '/repo-guard/',

  markdown: {
    theme: { light: 'github-light', dark: 'github-dark' },
  },

  themeConfig: {
    nav: [
      { text: 'Guide', link: '/guide/getting-started' },
      { text: 'CRDs', link: '/crds/github' },
      { text: 'Operations', link: '/operations/labels' },
      { text: 'Contributing', link: '/contributing' },
    ],

    sidebar: [
      {
        text: 'Guide',
        items: [
          { text: 'Getting Started', link: '/guide/getting-started' },
          { text: 'Architecture', link: '/guide/architecture' },
        ],
      },
      {
        text: 'Custom Resources',
        items: [
          { text: 'Github', link: '/crds/github' },
          { text: 'GithubOrganization', link: '/crds/github-organization' },
          { text: 'GithubTeam', link: '/crds/github-team' },
          { text: 'GithubTeamRepository', link: '/crds/github-team-repository' },
          { text: 'GithubAccountLink', link: '/crds/github-account-link' },
          { text: 'Member Providers', link: '/crds/member-providers' },
        ],
      },
      {
        text: 'Operations',
        items: [
          { text: 'Labels Reference', link: '/operations/labels' },
          { text: 'Metrics & Monitoring', link: '/operations/metrics' },
        ],
      },
      { text: 'Contributing', link: '/contributing' },
    ],

    search: {
      provider: 'local',
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/cloudoperators/repo-guard' },
    ],

    footer: {
      message: 'Released under the Apache 2.0 License.',
      copyright: 'Copyright © 2025 SAP SE or an SAP affiliate company and repo-guard contributors.',
    },
  },
}))
