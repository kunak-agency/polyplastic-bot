#!/usr/bin/env node
// Post-build security checks:
//   1. dist/index.html must not contain inline <script>...</script> blocks.
//   2. External <script src="http(s)://..."> or <link rel="stylesheet" href="http(s)://...">
//      must carry both `integrity` and `crossorigin` attributes (SRI).
//   3. A CSP <meta http-equiv="Content-Security-Policy"> tag must be present.

import { readFileSync, existsSync } from 'node:fs'
import { resolve } from 'node:path'

const distHtml = resolve(process.cwd(), 'dist', 'index.html')
if (!existsSync(distHtml)) {
  console.error(`[check-build-output] dist/index.html not found at ${distHtml}`)
  process.exit(2)
}

const html = readFileSync(distHtml, 'utf8')
const errors = []

// (1) No inline <script>...content...</script> blocks.
// A script tag with EITHER `src` OR `type="module" src` (no body) is fine.
const scriptRe = /<script\b([^>]*)>([\s\S]*?)<\/script>/gi
for (const m of html.matchAll(scriptRe)) {
  const attrs = m[1] ?? ''
  const body = (m[2] ?? '').trim()
  if (body.length > 0) {
    errors.push(`Inline <script> with body found: ${m[0].slice(0, 120)}...`)
  }
  // External script must have SRI.
  const srcMatch = attrs.match(/\bsrc\s*=\s*["']([^"']+)["']/i)
  if (srcMatch) {
    const src = srcMatch[1]
    if (/^https?:\/\//i.test(src)) {
      const hasIntegrity = /\bintegrity\s*=/.test(attrs)
      const hasCrossorigin = /\bcrossorigin\s*=/.test(attrs)
      if (!hasIntegrity || !hasCrossorigin) {
        errors.push(`External <script src="${src}"> missing SRI (integrity+crossorigin)`)
      }
    }
  }
}

// (2) External stylesheets must have SRI.
const linkRe = /<link\b([^>]*)\/?>/gi
for (const m of html.matchAll(linkRe)) {
  const attrs = m[1] ?? ''
  const relMatch = attrs.match(/\brel\s*=\s*["']([^"']+)["']/i)
  if (!relMatch) continue
  const rels = relMatch[1].split(/\s+/).map((r) => r.toLowerCase())
  if (!rels.includes('stylesheet')) continue
  const hrefMatch = attrs.match(/\bhref\s*=\s*["']([^"']+)["']/i)
  if (!hrefMatch) continue
  const href = hrefMatch[1]
  if (/^https?:\/\//i.test(href)) {
    const hasIntegrity = /\bintegrity\s*=/.test(attrs)
    const hasCrossorigin = /\bcrossorigin\s*=/.test(attrs)
    if (!hasIntegrity || !hasCrossorigin) {
      errors.push(`External stylesheet href="${href}" missing SRI (integrity+crossorigin)`)
    }
  }
}

// (3) CSP meta tag must be present.
if (!/<meta\s+http-equiv\s*=\s*["']Content-Security-Policy["']/i.test(html)) {
  errors.push('Missing <meta http-equiv="Content-Security-Policy"> in dist/index.html')
}

if (errors.length > 0) {
  console.error('[check-build-output] FAIL:')
  for (const e of errors) console.error('  -', e)
  process.exit(1)
}

console.log('[check-build-output] OK — no inline scripts, SRI clean, CSP meta present.')
