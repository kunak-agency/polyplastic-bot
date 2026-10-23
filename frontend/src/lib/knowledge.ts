// Knowledge base rules (ux-spec 3.6, api_contract.md v3, knowledge base):
// upload pre-checks, the table order and the site filter. Pure, types-only
// imports, so the node tests load it directly.
import type { KnowledgeDocument, KnowledgeSource, SiteCode } from '@/types/api'

// The formats the backend (and the rag behind it) accept.
export const ACCEPTED_EXTENSIONS = ['.pdf', '.pptx', '.xlsx', '.txt', '.md'] as const
export const ACCEPT_ATTR = ACCEPTED_EXTENSIONS.join(',')
// KB_MAX_UPLOAD_MB on the backend; the server re-checks, this only saves a
// round trip for an obviously wrong file.
export const MAX_UPLOAD_MB = 20

export type FileProblem = 'format' | 'size' | 'empty' | null

export function extensionOf(name: string): string {
  const dot = name.lastIndexOf('.')
  return dot < 0 ? '' : name.slice(dot).toLowerCase()
}

export function fileProblem(name: string, size: number, maxMb = MAX_UPLOAD_MB): FileProblem {
  if (!(ACCEPTED_EXTENSIONS as readonly string[]).includes(extensionOf(name))) return 'format'
  if (size <= 0) return 'empty'
  if (size > maxMb * 1024 * 1024) return 'size'
  return null
}

// The document's name when the HR leaves it empty: the server does the same.
export function docNameFromFile(name: string): string {
  const dot = name.lastIndexOf('.')
  return (dot > 0 ? name.slice(0, dot) : name).trim()
}

// "PDF" etc. Codes are data, not copy: they stay Latin.
export function kindLabel(kind: KnowledgeDocument['kind'], filename: string | null): string {
  if (kind === 'text') return extensionOf(filename ?? '') === '.md' ? 'MD' : 'TXT'
  if (kind === 'corpus') return 'TXT'
  return kind.toUpperCase()
}

export type SizeUnit = 'b' | 'kb' | 'mb'

export function sizeParts(bytes: number): { value: string; unit: SizeUnit } {
  if (bytes < 1024) return { value: String(bytes), unit: 'b' }
  if (bytes < 1024 * 1024) return { value: (bytes / 1024).toFixed(0), unit: 'kb' }
  const mb = bytes / (1024 * 1024)
  return { value: mb < 10 ? mb.toFixed(1) : mb.toFixed(0), unit: 'mb' }
}

// A document with no sites answers everywhere; the global "all sites" filter
// shows everything.
export function appliesToSite(d: Pick<KnowledgeDocument, 'sites'>, site: SiteCode | null): boolean {
  return site === null || d.sites.length === 0 || d.sites.includes(site)
}

// Needs attention first (not in the index, then pages to proofread), then the
// most recently updated; the rag's own corpus goes last.
const STATUS_ORDER: Record<KnowledgeDocument['status'], number> = {
  not_indexed: 0,
  needs_review: 1,
  ready: 2,
}

export function sortDocuments(list: KnowledgeDocument[]): KnowledgeDocument[] {
  return [...list].sort((a, b) => {
    if (a.source !== b.source) return a.source === 'corpus' ? 1 : -1
    const s = STATUS_ORDER[a.status] - STATUS_ORDER[b.status]
    if (s !== 0) return s
    return (b.updated_at ?? '').localeCompare(a.updated_at ?? '')
  })
}

// A clause id is either a number like 6.7.8 (shown with the "clause" prefix
// from the catalogue) or the rag's own locator for slides, rows and pages,
// which is shown as it came.
export function isNumberedClause(clauseId: string): boolean {
  return /^\d+(\.\d+)*\.?$/.test(clauseId.trim())
}

// Did any quote come from this document (an XLSX sheet arrives as "name / sheet").
export function sourceFromDoc(s: Pick<KnowledgeSource, 'doc'>, doc: string): boolean {
  return s.doc === doc || s.doc.startsWith(`${doc} / `)
}

export function pagesToReview(d: Pick<KnowledgeDocument, 'low_confidence_pages'>): number {
  return d.low_confidence_pages.length
}
