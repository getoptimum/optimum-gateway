import { defineConfigWithTheme, type DefaultTheme } from 'vitepress'
import fs from 'node:fs'
import path from 'node:path'

// Extends DefaultTheme.Config so custom fields (versions / latest) read by
// our SelectedVersionDropdown via useData().theme don't fail TS narrowing.
interface ThemeConfig extends DefaultTheme.Config {
  versions: string[]
  latest: string
}

// --- discover versions ---
const DOCS_ROOT = path.resolve(__dirname, '..', 'versions')
const versions = fs
  .readdirSync(DOCS_ROOT, { withFileTypes: true })
  .filter(d => d.isDirectory())
  .map(d => d.name)
  .sort((a, b) => a.localeCompare(b, undefined, { numeric: true }))

const latest = versions.at(-1) ?? ''
const base = process.env.BASE ?? '/'
const withBase = (path: string) => `${base}${path.replace(/^\//, '')}`

// --- sidebar per version ---
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const sidebar: Record<string, any> = {}
for (const v of versions) {
  const versionDir = path.join(DOCS_ROOT, v)
  const items: Array<{ text: string; link: string }> = []
  
  // Check for Release Notes first (top of sidebar)
  const releaseNotesPath = path.join(versionDir, 'release_notes.md')
  if (fs.existsSync(releaseNotesPath)) {
    items.push({ text: 'Release Notes', link: `/versions/${v}/release_notes` })
  }
  
  // Always include Overview second
  items.push({ text: 'Overview', link: `/versions/${v}/` })
  
  // Add other items only if the file exists
  const possibleItems = [
    { text: 'Network Requirements', file: '00_network_requirements.md' },
    { text: 'Quick Start', file: '01_quick_start.md' },
    { text: 'Configuration', file: '02_configuration.md' },
    { text: 'Metrics & Grafana', file: '03_telemetry.md' },
    { text: 'Troubleshooting', file: '04_troubleshoot.md' },
    { text: 'Metrics', file: 'metrics.md' },
    { text: 'Metrics Methodology', file: 'metrics_methodology.md' }
  ]
  
  for (const item of possibleItems) {
    const filePath = path.join(versionDir, item.file)
    if (fs.existsSync(filePath)) {
      const link = item.file.replace(/\.md$/, '')
      items.push({ text: item.text, link: `/versions/${v}/${link}` })
    }
  }
  
  sidebar[`/versions/${v}/`] = [
    {
      text: `Gateway (${v})`,
      items
    }
  ]
}

export default defineConfigWithTheme<ThemeConfig>({
  lang: 'en-US',
  title: 'Optimum Gateway Docs',
  description: 'Validator integration & operations',
  lastUpdated: true,
  cleanUrls: true,
  ignoreDeadLinks: true,
  base,

  // Published site is only `versions/`
  srcExclude: [
    'ADR/**',
    'README.md',
    'application-level-alerting.md',
    'security-hooks.md',
  ],

  sitemap: {
    hostname: 'https://docs.getoptimum.xyz'
  },

  head: [
    [
      'link',
      { rel: 'icon', href: withBase('/favicons/favicon.svg'), type: 'image/svg+xml' }
    ],
    [
      'link',
      { rel: 'icon', href: withBase('/favicons/favicon-96x96.png'), type: 'image/png' }
    ],
    [
      'link',
      { rel: 'shortcut icon', href: withBase('/favicons/favicon.ico'), type: 'image/x-icon' }
    ],
    ['meta', { name: 'msapplication-TileColor', content: '#fff' }],
    ['meta', { name: 'theme-color', content: '#fff' }],
    [
      'meta',
      {
        name: 'viewport',
        content: 'width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no'
      }
    ],
    ['meta', { property: 'description', content: 'The world\'s first high-performance memory infrastructure for any blockchain.' }],
    ['meta', { httpEquiv: 'Content-Language', content: 'en' }],
    ['meta', { name: 'twitter:card', content: 'summary_large_image' }],
    ['meta', { name: 'twitter:site', content: '@get_optimum' }],
    ['meta', { name: 'twitter:site:domain', content: 'docs.getoptimum.xyz' }],
    ['meta', { name: 'twitter:url', content: 'https://docs.getoptimum.xyz' }],
    ['meta', { name: 'twitter:image:alt', content: 'Optimum Documentation' }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:site_name', content: 'Optimum Docs' }],
    ['meta', { property: 'og:url', content: 'https://docs.getoptimum.xyz' }],
    ['meta', { property: 'og:image:width', content: '1200' }],
    ['meta', { property: 'og:image:height', content: '630' }],
    ['meta', { property: 'og:image:type', content: 'image/png' }]
  ],

  // Font preloads are handled by @fontsource-variable/geist + geist-mono
  // (imported in theme/fonts.css). @fontsource ships its own @font-face
  // declarations with font-display: swap, and Vite hashes the woff2 assets
  // automatically — no manual preload wiring needed.

  themeConfig: {
    nav: [
      { text: 'GitHub', link: 'https://github.com/getoptimum/optimum-gateway' }
    ],
    // Exposed at runtime via useData().theme — consumed by the dropdown.
    versions,
    latest,
    sidebar,
    outline: { level: 'deep' },
    search: { provider: 'local', options: { detailedView: true } },
    logo: {
      alt: 'Optimum Logo',
      light: '/logo-light.png',
      dark: '/logo-dark.png'
    },
    logoLink: "https://docs.getoptimum.xyz/",
    siteTitle: false,
    socialLinks: [
      { icon: 'github', link: 'https://github.com/getoptimum/optimum-gateway' },
      { icon: 'x', link: 'https://x.com/get_optimum' },
      { icon: 'discord', link: 'https://discord.gg/7EwFpu79cZ' }
    ]
  }
})
