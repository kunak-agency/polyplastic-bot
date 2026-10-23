// Media library rules (src/lib/media.ts): upload pre-checks, per-site slots,
// filters, order, formatting.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import type { MediaFile, MediaItem } from '../src/types/api.ts'
import {
  canDelete,
  countMissing,
  fileFor,
  filterMedia,
  formatLabel,
  mediaFileProblem,
  mediaKindOfFile,
  mediaNameFromFile,
  mediaNameKey,
  mediaSize,
  mediaSlots,
  nameTaken,
  pickerOptions,
  sortMedia,
  uncoveredSites,
} from '../src/lib/media.ts'

function f(site: MediaFile['site'], filename = 'a.png'): MediaFile {
  return {
    site,
    filename,
    mime: 'image/png',
    size: 100,
    sha256: '0',
    updated_at: '2026-10-24T08:00:00Z',
  }
}

function m(id: string, extra: Partial<MediaItem> = {}): MediaItem {
  return {
    id,
    name: `Item ${id}`,
    kind: 'image',
    files: [f(null)],
    status: 'ready',
    used_in: 0,
    aliases: [],
    updated_by: null,
    updated_at: '2026-10-24T08:00:00Z',
    ...extra,
  }
}

test('kind by extension, unknown formats refused', () => {
  assert.equal(mediaKindOfFile('map.PNG'), 'image')
  assert.equal(mediaKindOfFile('photo.jpeg'), 'image')
  assert.equal(mediaKindOfFile('pass.pdf'), 'file')
  assert.equal(mediaKindOfFile('run.exe'), null)
  assert.equal(mediaKindOfFile('noext'), null)
})

test('upload problems: format, empty, size, kind mixing', () => {
  assert.equal(mediaFileProblem('a.png', 10), null)
  assert.equal(mediaFileProblem('a.txt', 10), 'format')
  assert.equal(mediaFileProblem('a.png', 0), 'empty')
  assert.equal(mediaFileProblem('a.png', 21 * 1024 * 1024), 'size')
  assert.equal(mediaFileProblem('a.png', 2 * 1024 * 1024, null, null, 1), 'size')
  const img = m('1', { files: [f(null), f('GTP')] })
  assert.equal(mediaFileProblem('a.pdf', 10, img, 'GrPP'), 'kind')
  // Replacing the only file of the slot may change the kind.
  const one = m('2', { files: [f('GTP')] })
  assert.equal(mediaFileProblem('a.pdf', 10, one, 'GTP'), null)
  // A stub without files takes any kind.
  assert.equal(mediaFileProblem('a.pdf', 10, m('3', { files: [], status: 'missing' }), null), null)
})

test('names: from file, key, taken', () => {
  assert.equal(mediaNameFromFile('Map  GTP.final.png'), 'Map GTP.final')
  assert.equal(mediaNameFromFile('.hidden'), '.hidden')
  assert.equal(mediaNameKey('  Image   14 '), 'image 14')
  const list = [m('1', { name: 'Image 14' }), m('2', { name: 'Map' })]
  assert.equal(nameTaken(list, 'image  14'), true)
  assert.equal(nameTaken(list, 'image 14', '1'), false)
  assert.equal(nameTaken(list, '  '), false)
})

test('file for a site: own, else common, else none', () => {
  const item = m('1', { files: [f(null, 'common.png'), f('GTP', 'gtp.png')] })
  assert.equal(fileFor(item, 'GTP')?.filename, 'gtp.png')
  assert.equal(fileFor(item, 'GrPP')?.filename, 'common.png')
  assert.equal(fileFor(item, null)?.filename, 'common.png')
  assert.equal(fileFor(m('2', { files: [f('GTP')] }), 'GrPP'), null)
})

test('slots and uncovered sites', () => {
  const item = m('1', { files: [f('GTP'), f('GrPP')] })
  const slots = mediaSlots(item)
  assert.deepEqual(
    slots.map((s) => [s.site, s.file !== null]),
    [
      [null, false],
      ['GrPP', true],
      ['Luzhniki', false],
      ['GTP', true],
    ],
  )
  assert.deepEqual(uncoveredSites(item), ['Luzhniki'])
  assert.deepEqual(uncoveredSites(m('2')), [])
  assert.deepEqual(uncoveredSites(m('3', { files: [] })), [])
})

test('filters: missing only, kind, query over name, aliases and filenames', () => {
  const list = [
    m('1', { name: 'Map', aliases: ['old plan'] }),
    m('2', { name: 'Image 14', files: [], status: 'missing' }),
    m('3', { name: 'Pass', kind: 'file', files: [f('GTP', 'pass-gtp.pdf')] }),
  ]
  assert.deepEqual(
    filterMedia(list, { missingOnly: true }).map((x) => x.id),
    ['2'],
  )
  assert.deepEqual(
    filterMedia(list, { kind: 'file' }).map((x) => x.id),
    ['3'],
  )
  assert.deepEqual(
    filterMedia(list, { query: 'PLAN' }).map((x) => x.id),
    ['1'],
  )
  assert.deepEqual(
    filterMedia(list, { query: 'gtp.pdf' }).map((x) => x.id),
    ['3'],
  )
  assert.equal(filterMedia(list, {}).length, 3)
  assert.equal(countMissing(list), 1)
})

test('order: missing first, then name with numbers; picker puts missing last', () => {
  const list = [
    m('a', { name: 'Image 10' }),
    m('b', { name: 'Image 9' }),
    m('c', { name: 'Zeta', files: [], status: 'missing' }),
  ]
  assert.deepEqual(
    sortMedia(list).map((x) => x.id),
    ['c', 'b', 'a'],
  )
  assert.deepEqual(
    pickerOptions(list, '').map((x) => x.id),
    ['b', 'a', 'c'],
  )
  assert.deepEqual(
    pickerOptions(list, 'image 1').map((x) => x.id),
    ['a'],
  )
})

test('delete only when unused; sizes and format labels', () => {
  assert.equal(canDelete({ used_in: 0 }), true)
  assert.equal(canDelete({ used_in: 2 }), false)
  assert.deepEqual(mediaSize(512), { value: '512', unit: 'b' })
  assert.deepEqual(mediaSize(48_000), { value: '47', unit: 'kb' })
  assert.deepEqual(mediaSize(3 * 1024 * 1024), { value: '3.0', unit: 'mb' })
  assert.equal(formatLabel({ filename: 'a.jpeg' }), 'JPG')
  assert.equal(formatLabel({ filename: 'map.pdf' }), 'PDF')
})
