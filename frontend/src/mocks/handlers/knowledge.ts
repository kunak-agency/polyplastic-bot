import type { MockRequest, MockResult, MockRoute } from '../adapter'
import type {
  KnowledgeDocKind,
  KnowledgeDocument,
  KnowledgeDocumentVersion,
  SiteCode,
} from '@/types/api'
import { newId } from '../state'
import { answerFixture, documentSeeds, unansweredSeeds, versionSeeds } from '../fixtures/knowledge'

// Mirrors api_contract.md v3 (база знаний). The mock keeps its own in-memory
// list: an upload with an existing name becomes a new version, as on the
// server. Questions mentioning "дмс" or "парковк" get the refusal, the rest
// get a sourced answer.

const docs = new Map<string, KnowledgeDocument>()
const versions = new Map<string, KnowledgeDocumentVersion[]>()
const KINDS: Record<string, KnowledgeDocKind> = {
  '.pdf': 'pdf',
  '.pptx': 'pptx',
  '.xlsx': 'xlsx',
  '.txt': 'text',
  '.md': 'text',
}
const SITES = new Set(['GrPP', 'Luzhniki', 'GTP'])

function seed(): void {
  if (docs.size > 0) return
  for (const d of documentSeeds) docs.set(d.id, { ...d })
  for (const [id, list] of Object.entries(versionSeeds))
    versions.set(
      id,
      list.map((v) => ({ ...v })),
    )
}

function error(status: number, code: string, message: string, field?: string): MockResult {
  const details = field ? [{ field, code: 'INVALID_FORMAT', message }] : undefined
  return { status, data: { error: { code, message, traceId: `mock-${Date.now()}`, details } } }
}

function list(): MockResult {
  seed()
  const items = [...docs.values()]
  return { status: 200, data: { items, total: items.length, rag: 'ok' } }
}

function upload(req: MockRequest): MockResult {
  seed()
  const form = req.body as unknown
  const file = form instanceof FormData ? form.get('file') : null
  if (!(file instanceof File))
    return error(422, 'VALIDATION_FAILED', 'Нужен файл в поле file', 'file')
  const ext = file.name.slice(file.name.lastIndexOf('.')).toLowerCase()
  const kind = KINDS[ext]
  if (!kind)
    return error(422, 'VALIDATION_FAILED', 'Нужен файл PDF, PPTX, XLSX, TXT или MD', 'file')
  if (file.size > 20 * 1024 * 1024)
    return error(422, 'VALIDATION_FAILED', 'Файл больше 20 МБ', 'file')
  const get = (k: string) => (form instanceof FormData ? String(form.get(k) ?? '').trim() : '')
  const name = get('doc') || file.name.replace(/\.[^.]+$/, '')
  const sites = get('sites')
    .split(',')
    .map((s) => s.trim())
    .filter((s) => SITES.has(s)) as SiteCode[]
  const now = new Date().toISOString()
  const prev = [...docs.values()].find((d) => d.doc === name && d.source === 'upload')
  // A scan-like name gets pages to proofread, so the state is visible in mocks.
  const pages = /scan|скан/i.test(file.name) ? [2] : []
  const doc: KnowledgeDocument = {
    id: prev?.id ?? newId(),
    rag_doc_id: Math.random().toString(16).slice(2, 18).padEnd(16, '0'),
    doc: name,
    filename: file.name,
    kind,
    source: 'upload',
    sites,
    version: (prev?.version ?? 0) + 1,
    status: pages.length > 0 ? 'needs_review' : 'ready',
    indexed: true,
    chunks: 3 + (file.size % 7),
    clauses: 5 + (file.size % 11),
    low_confidence_pages: pages,
    size: file.size,
    uploaded_by: get('uploaded_by') || null,
    updated_at: now,
    index_error: null,
  }
  // Replacing the corpus document by name drops its corpus row.
  for (const [id, d] of docs) if (d.source === 'corpus' && d.doc === name) docs.delete(id)
  docs.set(doc.id, doc)
  const history = (versions.get(doc.id) ?? []).map((v) => ({ ...v, current: false }))
  history.unshift({
    version: doc.version,
    current: true,
    filename: file.name,
    kind,
    size: file.size,
    sha256: '0'.repeat(64),
    chunks: doc.chunks,
    clauses: doc.clauses,
    low_confidence_pages: pages,
    uploaded_by: doc.uploaded_by,
    uploaded_at: now,
  })
  versions.set(doc.id, history)
  return { status: 201, data: doc }
}

function history(req: MockRequest): MockResult {
  seed()
  const d = docs.get(req.params.id)
  if (!d || d.source !== 'upload') return error(404, 'NOT_FOUND', 'Документ не найден')
  const items = versions.get(d.id) ?? []
  return { status: 200, data: { items, total: items.length } }
}

function ask(req: MockRequest): MockResult {
  const question = typeof req.body.question === 'string' ? req.body.question.trim() : ''
  if (question.length < 3)
    return error(422, 'VALIDATION_FAILED', 'Задайте вопрос: хотя бы несколько слов', 'question')
  const doc = typeof req.body.doc === 'string' ? req.body.doc : null
  if (/дмс|парковк/i.test(question)) {
    return {
      status: 200,
      data: {
        answer: null,
        sources: [],
        confidence: 'low',
        needs_escalation: true,
        topic: 'other',
        abstain_reason: 'no_source',
        from_doc: doc === null ? null : false,
      },
    }
  }
  const fromDoc = doc === null ? null : answerFixture.sources.some((s) => s.doc === doc)
  return { status: 200, data: { ...answerFixture, from_doc: fromDoc } }
}

function unanswered(req: MockRequest): MockResult {
  const days = Number(req.query.days || 30)
  const items = unansweredSeeds
    .filter((c) => Date.parse(c.last_at) >= Date.now() - days * 86_400_000 || days >= 30)
    .map((c) => ({ ...c }))
  const questions = items.reduce((n, c) => n + c.count, 0)
  return {
    status: 200,
    data: { items, total: items.length, questions, since: new Date().toISOString() },
  }
}

export const knowledgeRoutes: MockRoute[] = [
  { method: 'GET', path: '/documents', handler: list },
  { method: 'POST', path: '/documents/ask', handler: ask },
  { method: 'POST', path: '/documents', handler: upload },
  { method: 'GET', path: '/documents/:id/versions', handler: history },
  { method: 'GET', path: '/knowledge/unanswered', handler: unanswered },
]
