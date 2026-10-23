import type { MediaItem } from '@/types/api'

// Synthetic media library: a scenario import left a few stubs without files,
// HR uploaded maps for some sites. No real files or personal data.
const AT = '2026-10-24T07:40:00Z'

function file(site: MediaItem['files'][number]['site'], filename: string, mime = 'image/png') {
  return {
    site,
    filename,
    mime,
    size: 48_000 + filename.length * 911,
    sha256: '0'.repeat(64),
    updated_at: AT,
  }
}

export const mediaSeeds: MediaItem[] = [
  {
    id: 'm-map',
    name: 'Схема проезда',
    kind: 'image',
    files: [
      file(null, 'shema-obshaya.png'),
      file('GTP', 'shema-gtp.png'),
      file('Luzhniki', 'shema-luzh.png'),
    ],
    status: 'ready',
    used_in: 3,
    aliases: ['карта площадки'],
    updated_by: 'Анна HR (синтетика)',
    updated_at: AT,
  },
  {
    id: 'm-pass',
    name: 'Пропуск: как получить',
    kind: 'file',
    files: [
      file('GrPP', 'propusk-grpp.pdf', 'application/pdf'),
      file('GTP', 'propusk-gtp.pdf', 'application/pdf'),
    ],
    status: 'ready',
    used_in: 1,
    aliases: [],
    updated_by: 'Анна HR (синтетика)',
    updated_at: AT,
  },
  {
    id: 'm-14',
    name: 'Изображение 14',
    kind: 'image',
    files: [],
    status: 'missing',
    used_in: 2,
    aliases: [],
    updated_by: 'импорт ТЗ',
    updated_at: AT,
  },
  {
    id: 'm-team',
    name: 'Команда смены',
    kind: 'image',
    files: [],
    status: 'missing',
    used_in: 1,
    aliases: [],
    updated_by: 'импорт ТЗ',
    updated_at: AT,
  },
  {
    id: 'm-canteen',
    name: 'Столовая',
    kind: 'image',
    files: [file(null, 'stolovaya.jpg', 'image/jpeg')],
    status: 'ready',
    used_in: 0,
    aliases: [],
    updated_by: null,
    updated_at: AT,
  },
]

// A colored tile with the entry's initials stands in for the image bytes.
const COLORS = ['#3b82f6', '#10b981', '#f59e0b', '#8b5cf6', '#ef4444']

export function mockImage(name: string, site: string | null): Blob {
  let h = 0
  for (const ch of name + (site ?? '')) h = (h * 31 + ch.charCodeAt(0)) >>> 0
  const color = COLORS[h % COLORS.length]
  const label = (site ?? name).slice(0, 3).toUpperCase()
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="96" height="96"><rect width="96" height="96" fill="${color}"/><text x="48" y="56" font-family="sans-serif" font-size="22" fill="#fff" text-anchor="middle">${label}</text></svg>`
  return new Blob([svg], { type: 'image/svg+xml' })
}
