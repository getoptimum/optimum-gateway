import { defineConfigWithTheme, type DefaultTheme } from 'vitepress'
import fs from 'node:fs'
import path from 'node:path'
import { groupIconMdPlugin, groupIconVitePlugin } from 'vitepress-plugin-group-icons'

// Extends DefaultTheme.Config so custom fields (versions / latest) read by
// our VersionSwitcher via useData().theme don't fail TS narrowing.
interface ThemeConfig extends DefaultTheme.Config {
  versions: string[]
  latest: string
}

// Keep `NN_foo_bar.md` on disk for ordering. Public URLs drop the prefix and
// use hyphens: 06_block_stream.md → /block-stream
const pretty = (s: string) => s.replace(/(^|\/)\d+_/g, '$1').replaceAll('_', '-')

// --- discover versions ---
const DOCS_ROOT = path.resolve(__dirname, '..', 'versions')
const versionDirs = fs
  .readdirSync(DOCS_ROOT, { withFileTypes: true })
  .filter(d => d.isDirectory())
  .map(d => d.name)

const versions = versionDirs
  .filter(v => v !== 'latest')
  .sort((a, b) => a.localeCompare(b, undefined, { numeric: true }))

const latest = versions.at(-1) ?? ''
const base = process.env.BASE ?? '/'
const withBase = (path: string) => `${base}${path.replace(/^\//, '')}`

const securityAudit = {
  text: 'Security Audit',
  link: 'https://cdn.probelab.io/media/documents/2026-08-ProbeLab-Security_Audit_Report_Optimum_Gateway.pdf',
  target: '_blank' as const,
  rel: 'noopener',
}

// --- sidebar per version (install order; metrics tables stay linked from telemetry) ---
const possibleItems = [
  { text: 'Network Requirements', file: '00_network_requirements.md' },
  { text: 'Quick Start', file: '01_quick_start.md' },
  { text: 'Kubernetes (Helm)', file: '05_kubernetes.md' },
  { text: 'Configuration', file: '02_configuration.md' },
  { text: 'Consumer Block Stream', file: '06_block_stream.md' },
  { text: 'Metrics & Grafana', file: '03_telemetry.md' },
  { text: 'Troubleshooting', file: '04_troubleshoot.md' }
]

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const sidebar: Record<string, any> = {}
for (const v of versionDirs) {
  const versionDir = path.join(DOCS_ROOT, v)
  const items: Array<{
    text: string
    link: string
    target?: string
    rel?: string
  }> = []
  const page = (file: string) => `/versions/${v}/${pretty(file).replace(/\.md$/, '')}`

  items.push({ text: 'Overview', link: `/versions/${v}/` })

  for (const item of possibleItems) {
    if (fs.existsSync(path.join(versionDir, item.file))) {
      items.push({ text: item.text, link: page(item.file) })
    }
  }

  if (fs.existsSync(path.join(versionDir, 'release_notes.md'))) {
    items.push({ text: 'Release Notes', link: page('release_notes.md') })
  }

  items.push({ ...securityAudit })

  sidebar[`/versions/${v}/`] = [{ text: `Gateway (${v})`, items }]
}

export default defineConfigWithTheme<ThemeConfig>({
  lang: 'en-US',
  title: 'Optimum Gateway Docs',
  description: 'Validator integration & operations',
  lastUpdated: true,
  cleanUrls: true,
  ignoreDeadLinks: true,
  base,
  rewrites: pretty,
  markdown: {
    image: { lazyLoading: true },
    config(md) {
      md.use(groupIconMdPlugin)
      // Rewrite in-page .md links so SPA nav hits the pretty route, not a 404.
      md.core.ruler.after('inline', 'pretty-doc-links', (state) => {
        for (const t of state.tokens.flatMap(b => b.children ?? [])) {
          const href = t.type === 'link_open' && t.attrGet('href')
          if (href && !/^(?:https?:|mailto:|#)/.test(href)) t.attrSet('href', pretty(href))
        }
      })
    }
  },
  vite: {
    plugins: [groupIconVitePlugin()]
  },
  // Old numbered/underscore URLs keep working on GitHub Pages.
  buildEnd({ outDir, rewrites }) {
    for (const [src, dest] of Object.entries(rewrites.map)) {
      const to = './' + dest.slice(dest.lastIndexOf('/') + 1).replace(/\.md$/, '')
      fs.writeFileSync(
        path.join(outDir, src.replace(/\.md$/, '.html')),
        `<!doctype html><meta http-equiv="refresh" content="0;url=${to}"><script>location.replace("${to}"+location.search+location.hash)</script>`
      )
    }
  },

  // Published site is only `versions/`
  srcExclude: [
    'adr/**',
    'README.md',
    'application-level-alerting.md',
    'security-hooks.md',
    'contributing.md'
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
        content: 'width=device-width, initial-scale=1.0'
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
      { text: 'Console', link: 'https://console.getoptimum.io/' },
      { text: 'Changelog', link: '/CHANGELOG' },
      {
        text: 'GitHub',
        link: 'https://github.com/getoptimum/optimum-gateway',
        target: '_blank',
        rel: 'noopener',
      },
      { ...securityAudit },
    ],
    // Exposed at runtime via useData().theme — consumed by the dropdown.
    versions,
    latest,
    sidebar,
    outline: { level: [2, 3] },
    search: {
      provider: 'local',
      options: {
        detailedView: true,
        // latest/ is an alias of the current numbered release — skip the duplicate hits.
        _render(src, env, md) {
          if (env.relativePath?.includes('/latest/')) return ''
          return md.render(src, env)
        }
      }
    },
    editLink: {
      pattern: ({ filePath }) =>
        `https://github.com/getoptimum/optimum-gateway/edit/main/docs/${filePath}`
    },
    logo: {
      alt: 'Optimum Logo',
      light: '/logo-light.png',
      dark: '/logo-dark.png'
    },
    logoLink: 'https://docs.getoptimum.xyz/',
    siteTitle: false,
    socialLinks: [
      { icon: 'github', link: 'https://github.com/getoptimum/optimum-gateway' },
      { icon: 'x', link: 'https://x.com/get_optimum' },
      { icon: 'discord', link: 'https://discord.gg/7EwFpu79cZ' }
    ]
  }
})
