// Download helpers (src/lib/download.ts): the file name of the scenario export.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { filenameFromDisposition, safeFilename } from '../src/lib/download.ts'

test('filename* (RFC 5987) wins over the ASCII name', () => {
  const name = 'Onboarding — draft.xlsx'
  const header = `attachment; filename="Onboarding  draft.xlsx"; filename*=UTF-8''${encodeURIComponent(name)}`
  assert.equal(filenameFromDisposition(header), name)
})

test('plain filename, quoted and bare', () => {
  assert.equal(filenameFromDisposition('attachment; filename="scenario.xlsx"'), 'scenario.xlsx')
  assert.equal(filenameFromDisposition('attachment; filename=scenario.xlsx'), 'scenario.xlsx')
})

test('no header or no name', () => {
  assert.equal(filenameFromDisposition(undefined), null)
  assert.equal(filenameFromDisposition('attachment'), null)
})

test('broken percent-encoding falls back to filename', () => {
  assert.equal(
    filenameFromDisposition(`attachment; filename="a.xlsx"; filename*=UTF-8''%E0%A4%A`),
    'a.xlsx',
  )
})

test('safeFilename drops separators and keeps a fallback', () => {
  assert.equal(safeFilename('a/b: c.xlsx', 'x.xlsx'), 'a b c.xlsx')
  assert.equal(safeFilename('  ', 'x.xlsx'), 'x.xlsx')
})
