import type { MockRequest, MockResult, MockRoute } from '../adapter'
import type { MediaItem, SiteCode } from '@/types/api'
import { newId } from '../state'
import { mediaSeeds, mockImage } from '../fixtures/media'
import { fileFor, mediaKindOfFile, mediaNameFromFile, mediaNameKey } from '@/lib/media'

// Mirrors api_contract.md v4 (медиатека) in memory: per-site files, stubs,
// rename with aliases, delete only when unused.

const items = new Map<string, MediaItem>()
const SITES = new Set(['GrPP', 'Luzhniki', 'GTP'])

function seed(): void {
  if (items.size > 0) return
  for (const m of mediaSeeds) items.set(m.id, structuredClone(m))
}

function error(status: number, code: string, message: string, field?: string): MockResult {
  const details = field ? [{ field, code: 'INVALID_FORMAT', message }] : undefined
  return { status, data: { error: { code, message, traceId: `mock-${Date.now()}`, details } } }
}

function byName(name: string): MediaItem | undefined {
  const key = mediaNameKey(name)
  return [...items.values()].find((m) => mediaNameKey(m.name) === key)
}

function formOf(req: MockRequest) {
  const form = req.body as unknown
  const fd = form instanceof FormData ? form : null
  const file = fd?.get('file')
  const get = (k: string) => String(fd?.get(k) ?? '').trim()
  return { file: file instanceof File ? file : null, get }
}

function addFile(m: MediaItem, file: File, siteRaw: string): MockResult | null {
  const kind = mediaKindOfFile(file.name)
  if (!kind) return error(422, 'VALIDATION_FAILED', 'Нужна картинка или PDF', 'file')
  const site = (SITES.has(siteRaw) ? siteRaw : null) as SiteCode | null
  if (m.files.some((f) => f.site !== site) && m.kind !== kind)
    return error(422, 'VALIDATION_FAILED', 'У записи уже есть файлы другого типа', 'file')
  const now = new Date().toISOString()
  m.files = m.files.filter((f) => f.site !== site)
  m.files.push({
    site,
    filename: file.name,
    mime: file.type || 'application/octet-stream',
    size: file.size,
    sha256: Math.random().toString(16).slice(2).padEnd(64, '0'),
    updated_at: now,
  })
  m.files.sort((a, b) => (a.site ?? '').localeCompare(b.site ?? ''))
  m.kind = kind
  m.status = 'ready'
  m.updated_at = now
  return null
}

function list(req: MockRequest): MockResult {
  seed()
  const all = [...items.values()].filter((m) => !req.query.status || m.status === req.query.status)
  return { status: 200, data: { items: all, total: all.length } }
}

function create(req: MockRequest): MockResult {
  seed()
  const { file, get } = formOf(req)
  if (!file) return error(422, 'VALIDATION_FAILED', 'Нужен файл в поле file', 'file')
  const name = get('name') || mediaNameFromFile(file.name)
  let m = byName(name)
  if (m && m.files.length > 0) return error(409, 'CONFLICT', `В медиатеке уже есть «${m.name}»`)
  if (!m) {
    m = {
      id: newId(),
      name,
      kind: 'image',
      files: [],
      status: 'missing',
      used_in: 0,
      aliases: [],
      updated_by: get('updated_by') || null,
      updated_at: '',
    }
    items.set(m.id, m)
  }
  const err = addFile(m, file, get('site'))
  if (err) return err
  return { status: 201, data: m }
}

function put(req: MockRequest): MockResult {
  seed()
  const m = items.get(req.params.id)
  if (!m) return error(404, 'NOT_FOUND', 'Запись медиатеки не найдена')
  const { file, get } = formOf(req)
  if (!file) return error(422, 'VALIDATION_FAILED', 'Нужен файл в поле file', 'file')
  const err = addFile(m, file, get('site'))
  return err ?? { status: 200, data: m }
}

function removeFile(req: MockRequest): MockResult {
  seed()
  const m = items.get(req.params.id)
  if (!m) return error(404, 'NOT_FOUND', 'Запись медиатеки не найдена')
  const site = req.params.site === 'common' ? null : req.params.site
  m.files = m.files.filter((f) => f.site !== site)
  m.status = m.files.length > 0 ? 'ready' : 'missing'
  return { status: 200, data: m }
}

function rename(req: MockRequest): MockResult {
  seed()
  const m = items.get(req.params.id)
  if (!m) return error(404, 'NOT_FOUND', 'Запись медиатеки не найдена')
  const name = String(req.body.name ?? '')
    .replace(/\s+/g, ' ')
    .trim()
  if (!name) return error(422, 'VALIDATION_FAILED', 'Обязательное поле', 'name')
  const other = byName(name)
  if (other && other.id !== m.id) return error(409, 'CONFLICT', `Имя «${other.name}» уже занято`)
  const old = mediaNameKey(m.name)
  if (old !== mediaNameKey(name) && !m.aliases.includes(old)) m.aliases.push(old)
  m.aliases = m.aliases.filter((a) => a !== mediaNameKey(name))
  m.name = name
  m.updated_at = new Date().toISOString()
  return { status: 200, data: { ...m, updated_refs: m.used_in } }
}

function remove(req: MockRequest): MockResult {
  seed()
  const m = items.get(req.params.id)
  if (!m) return error(404, 'NOT_FOUND', 'Запись медиатеки не найдена')
  if (m.used_in > 0) return error(409, 'CONFLICT', `«${m.name}» используется в сценариях`)
  items.delete(m.id)
  return { status: 204 }
}

function ensure(req: MockRequest): MockResult {
  seed()
  const names = Array.isArray(req.body.names) ? (req.body.names as string[]) : []
  const out: MediaItem[] = []
  let created = 0
  for (const n of names) {
    if (!n.trim()) continue
    let m = byName(n) ?? [...items.values()].find((x) => x.aliases.includes(mediaNameKey(n)))
    if (!m) {
      m = {
        id: newId(),
        name: n.trim(),
        kind: /pdf/i.test(n) ? 'file' : 'image',
        files: [],
        status: 'missing',
        used_in: 0,
        aliases: [],
        updated_by: null,
        updated_at: new Date().toISOString(),
      }
      items.set(m.id, m)
      created++
    }
    if (!out.includes(m)) out.push(m)
  }
  return { status: 200, data: { items: out, total: out.length, created } }
}

function content(req: MockRequest): MockResult {
  seed()
  const m = items.get(req.params.id)
  const site = (SITES.has(req.query.site ?? '') ? req.query.site : null) as SiteCode | null
  const f = m ? (fileFor(m, site) ?? m.files[0]) : null
  if (!m || !f) return error(404, 'NOT_FOUND', 'Файла нет')
  return { status: 200, data: mockImage(m.name, f.site) }
}

export const mediaRoutes: MockRoute[] = [
  { method: 'GET', path: '/media', handler: list },
  { method: 'POST', path: '/media/ensure', handler: ensure },
  { method: 'POST', path: '/media', handler: create },
  { method: 'GET', path: '/media/:id/content', handler: content },
  { method: 'DELETE', path: '/media/:id/files/:site', handler: removeFile },
  { method: 'PUT', path: '/media/:id', handler: put },
  { method: 'PATCH', path: '/media/:id', handler: rename },
  { method: 'DELETE', path: '/media/:id', handler: remove },
]
