// Media library rules (api_contract.md v4, branching verdict Q5): upload
// pre-checks, per-site file slots, filters and order of the table. Pure,
// types-only imports, so the node tests load it directly.
import type { MediaFile, MediaItem, MediaKind, SiteCode } from '@/types/api'

// What the backend accepts, by extension; it re-checks the file signature.
export const MEDIA_EXTENSIONS: Record<string, MediaKind> = {
  '.png': 'image',
  '.jpg': 'image',
  '.jpeg': 'image',
  '.gif': 'image',
  '.webp': 'image',
  '.pdf': 'file',
  '.docx': 'file',
  '.xlsx': 'file',
  '.pptx': 'file',
}
export const MEDIA_ACCEPT = Object.keys(MEDIA_EXTENSIONS).join(',')
// MEDIA_MAX_MB on the backend; the server re-checks.
export const MAX_MEDIA_MB = 20

// The table's file columns: the common file first, then one per site.
export const MEDIA_SLOTS: readonly (SiteCode | null)[] = [null, 'GrPP', 'Luzhniki', 'GTP']

export type MediaFileProblem = 'format' | 'size' | 'empty' | 'kind' | null

function extensionOf(name: string): string {
  const dot = name.lastIndexOf('.')
  return dot < 0 ? '' : name.slice(dot).toLowerCase()
}

export function mediaKindOfFile(name: string): MediaKind | null {
  return MEDIA_EXTENSIONS[extensionOf(name)] ?? null
}

// `item` given: the file goes into that entry, and an entry that already has
// files of the other kind refuses it (the bot sends images and documents
// differently). Replacing the only file of a slot may change the kind.
export function mediaFileProblem(
  name: string,
  size: number,
  item: Pick<MediaItem, 'kind' | 'files'> | null = null,
  site: SiteCode | null = null,
  maxMb = MAX_MEDIA_MB,
): MediaFileProblem {
  const kind = mediaKindOfFile(name)
  if (!kind) return 'format'
  if (size <= 0) return 'empty'
  if (size > maxMb * 1024 * 1024) return 'size'
  if (item && item.kind !== kind && item.files.some((f) => f.site !== site)) return 'kind'
  return null
}

// The entry's name when none is given: the server does the same.
export function mediaNameFromFile(name: string): string {
  const dot = name.lastIndexOf('.')
  return (dot > 0 ? name.slice(0, dot) : name).replace(/\s+/g, ' ').trim()
}

// Names compare case-insensitively with collapsed spaces (the backend's key).
export function mediaNameKey(name: string): string {
  return name.replace(/\s+/g, ' ').trim().toLowerCase()
}

export function nameTaken(list: MediaItem[], name: string, exceptId?: string): boolean {
  const key = mediaNameKey(name)
  return key !== '' && list.some((m) => m.id !== exceptId && mediaNameKey(m.name) === key)
}

// The file the bot sends to a newcomer of `site`: own, else the common one.
export function fileFor(item: Pick<MediaItem, 'files'>, site: SiteCode | null): MediaFile | null {
  const own = site ? item.files.find((f) => f.site === site) : undefined
  return own ?? item.files.find((f) => f.site === null) ?? null
}

export interface MediaSlot {
  site: SiteCode | null
  file: MediaFile | null
}

export function mediaSlots(item: Pick<MediaItem, 'files'>): MediaSlot[] {
  return MEDIA_SLOTS.map((site) => ({
    site,
    file: item.files.find((f) => f.site === site) ?? null,
  }))
}

// Sites whose newcomers get nothing: no file of their own and no common one.
export function uncoveredSites(item: Pick<MediaItem, 'files'>): SiteCode[] {
  if (item.files.length === 0 || item.files.some((f) => f.site === null)) return []
  return (MEDIA_SLOTS.filter((s) => s !== null) as SiteCode[]).filter(
    (s) => !item.files.some((f) => f.site === s),
  )
}

export interface MediaFilter {
  query?: string
  missingOnly?: boolean
  kind?: MediaKind | null
}

export function filterMedia(list: MediaItem[], f: MediaFilter): MediaItem[] {
  const q = mediaNameKey(f.query ?? '')
  return list.filter((m) => {
    if (f.missingOnly && m.status !== 'missing') return false
    if (f.kind && m.kind !== f.kind) return false
    if (!q) return true
    return (
      mediaNameKey(m.name).includes(q) ||
      m.aliases.some((a) => a.includes(q)) ||
      m.files.some((file) => file.filename.toLowerCase().includes(q))
    )
  })
}

// Missing first (they block messages), then by name.
export function sortMedia(list: MediaItem[]): MediaItem[] {
  return [...list].sort((a, b) => {
    if (a.status !== b.status) return a.status === 'missing' ? -1 : 1
    return a.name.localeCompare(b.name, 'ru', { numeric: true })
  })
}

export function countMissing(list: MediaItem[]): number {
  return list.filter((m) => m.status === 'missing').length
}

export function canDelete(item: Pick<MediaItem, 'used_in'>): boolean {
  return item.used_in === 0
}

export type SizeUnit = 'b' | 'kb' | 'mb'

export function mediaSize(bytes: number): { value: string; unit: SizeUnit } {
  if (bytes < 1024) return { value: String(bytes), unit: 'b' }
  if (bytes < 1024 * 1024) return { value: (bytes / 1024).toFixed(0), unit: 'kb' }
  const mb = bytes / (1024 * 1024)
  return { value: mb < 10 ? mb.toFixed(1) : mb.toFixed(0), unit: 'mb' }
}

// "PNG", "PDF": format codes are data and stay Latin.
export function formatLabel(file: Pick<MediaFile, 'filename'>): string {
  const ext = extensionOf(file.filename).slice(1)
  return ext === 'jpeg' ? 'JPG' : ext.toUpperCase()
}

// The picker's options: matches first by name, missing ones last.
export function pickerOptions(
  list: MediaItem[],
  query: string,
  kind?: MediaKind | null,
): MediaItem[] {
  return sortMedia(filterMedia(list, { query, kind })).sort((a, b) => {
    if (a.status !== b.status) return a.status === 'missing' ? 1 : -1
    return 0
  })
}
