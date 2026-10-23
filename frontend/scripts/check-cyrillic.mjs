#!/usr/bin/env node
// i18n gate (INV-7): no user-facing string may live outside the locale catalog.
// Every Cyrillic character found in src/**/*.{vue,ts} is treated as a hardcoded
// user-facing string and fails the check. Excluded: src/i18n/locales (the
// catalog itself) and src/mocks (development-only fixture data).
// Reports one line per offending file.

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { resolve, join, relative } from 'node:path'

const ROOT = resolve(process.cwd(), 'src')
const EXCLUDE_DIRS = ['mocks', join('i18n', 'locales')]
const EXTS = ['.vue', '.ts']
const CYRILLIC = /[Ѐ-ӿ]/

function isExcluded(absPath) {
  const rel = relative(ROOT, absPath)
  return EXCLUDE_DIRS.some((d) => rel === d || rel.startsWith(d + '/'))
}

function walk(dir, out) {
  for (const entry of readdirSync(dir)) {
    const abs = join(dir, entry)
    if (isExcluded(abs)) continue
    if (statSync(abs).isDirectory()) {
      walk(abs, out)
    } else if (EXTS.some((e) => abs.endsWith(e))) {
      out.push(abs)
    }
  }
  return out
}

const files = walk(ROOT, [])
const offenders = files.filter((f) => CYRILLIC.test(readFileSync(f, 'utf8')))

if (offenders.length > 0) {
  console.error(`[check-cyrillic] ${offenders.length} in-scope file(s) still contain Cyrillic:`)
  for (const f of offenders) console.error('  -', relative(process.cwd(), f))
  process.exit(1)
}

console.log('[check-cyrillic] OK — no hardcoded Cyrillic in in-scope files.')
