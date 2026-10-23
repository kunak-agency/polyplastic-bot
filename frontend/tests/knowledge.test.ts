// Knowledge base rules (src/lib/knowledge.ts): upload pre-checks, table order,
// site filter, clause labels.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import type { KnowledgeDocument } from '../src/types/api.ts'
import {
  appliesToSite,
  docNameFromFile,
  fileProblem,
  isNumberedClause,
  kindLabel,
  pagesToReview,
  sizeParts,
  sortDocuments,
  sourceFromDoc,
} from '../src/lib/knowledge.ts'

function doc(id: string, extra: Partial<KnowledgeDocument> = {}): KnowledgeDocument {
  return {
    id,
    rag_doc_id: `r-${id}`,
    doc: `Doc ${id}`,
    filename: `${id}.pdf`,
    kind: 'pdf',
    source: 'upload',
    sites: [],
    version: 1,
    status: 'ready',
    indexed: true,
    chunks: 3,
    clauses: 5,
    low_confidence_pages: [],
    size: 1000,
    uploaded_by: null,
    updated_at: '2026-10-24T08:00:00Z',
    index_error: null,
    ...extra,
  }
}

test('fileProblem: format, empty and size are caught before upload', () => {
  assert.equal(fileProblem('memo.pdf', 10), null)
  assert.equal(fileProblem('DECK.PPTX', 10), null)
  assert.equal(fileProblem('notes.md', 10), null)
  assert.equal(fileProblem('photo.jpg', 10), 'format')
  assert.equal(fileProblem('old.xls', 10), 'format')
  assert.equal(fileProblem('noext', 10), 'format')
  assert.equal(fileProblem('memo.pdf', 0), 'empty')
  assert.equal(fileProblem('memo.pdf', 20 * 1024 * 1024), null)
  assert.equal(fileProblem('memo.pdf', 20 * 1024 * 1024 + 1), 'size')
  assert.equal(fileProblem('memo.pdf', 3 * 1024 * 1024, 2), 'size')
})

test('docNameFromFile drops the extension only', () => {
  assert.equal(docNameFromFile('Safety rules v2.pdf'), 'Safety rules v2')
  assert.equal(docNameFromFile('.hidden'), '.hidden')
  assert.equal(docNameFromFile('plain'), 'plain')
})

test('kindLabel and sizeParts', () => {
  assert.equal(kindLabel('pptx', 'a.pptx'), 'PPTX')
  assert.equal(kindLabel('text', 'a.md'), 'MD')
  assert.equal(kindLabel('text', 'a.txt'), 'TXT')
  assert.deepEqual(sizeParts(512), { value: '512', unit: 'b' })
  assert.deepEqual(sizeParts(2048), { value: '2', unit: 'kb' })
  assert.deepEqual(sizeParts(1.5 * 1024 * 1024), { value: '1.5', unit: 'mb' })
  assert.deepEqual(sizeParts(15 * 1024 * 1024), { value: '15', unit: 'mb' })
})

test('appliesToSite: empty sites mean every site', () => {
  assert.equal(appliesToSite(doc('a'), 'GTP'), true)
  assert.equal(appliesToSite(doc('a', { sites: ['GrPP'] }), 'GTP'), false)
  assert.equal(appliesToSite(doc('a', { sites: ['GrPP'] }), null), true)
})

test('sortDocuments: attention first, fresh first, corpus last', () => {
  const list = [
    doc('corpus', { source: 'corpus', updated_at: '2026-10-25T00:00:00Z' }),
    doc('old', { updated_at: '2026-10-20T00:00:00Z' }),
    doc('new', { updated_at: '2026-10-23T00:00:00Z' }),
    doc('review', { status: 'needs_review', updated_at: '2026-10-01T00:00:00Z' }),
    doc('lost', { status: 'not_indexed', updated_at: '2026-09-01T00:00:00Z' }),
  ]
  assert.deepEqual(
    sortDocuments(list).map((d) => d.id),
    ['lost', 'review', 'new', 'old', 'corpus'],
  )
})

test('isNumberedClause and sourceFromDoc', () => {
  assert.equal(isNumberedClause('6.7.8'), true)
  assert.equal(isNumberedClause('3'), true)
  assert.equal(isNumberedClause('slide 3'), false)
  assert.equal(sourceFromDoc({ doc: 'Staff / Sheet1' }, 'Staff'), true)
  assert.equal(sourceFromDoc({ doc: 'Staff-2' }, 'Staff'), false)
  assert.equal(pagesToReview(doc('a', { low_confidence_pages: [2, 5] })), 2)
})
