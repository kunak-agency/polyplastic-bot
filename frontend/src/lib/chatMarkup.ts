// Bot message markup for the dev chat (ux-spec 2.2): MAX "markdown" and "html"
// formats turned into plain styled spans. Nothing is ever injected as HTML —
// the page renders spans with text interpolation, so a stray tag in a bot text
// stays visible text. Pure, types-only imports, so node tests load it directly.
import type { DevChatButton } from '@/types/devChat'

export interface Span {
  text: string
  bold?: boolean
  italic?: boolean
  underline?: boolean
  strike?: boolean
  code?: boolean
  href?: string
}

type Style = Omit<Span, 'text' | 'href'>
type Flag = keyof Style

// Only web links open from the chat; anything else (javascript:, data:) is text.
export function safeHref(url: string): string | undefined {
  const u = url.trim()
  return /^https?:\/\/[^\s]+$/i.test(u) ? u : undefined
}

function push(out: Span[], text: string, style: Style, href?: string): void {
  if (!text) return
  const last = out[out.length - 1]
  const s: Span = { text }
  for (const k of Object.keys(style) as Flag[]) if (style[k]) s[k] = true
  if (href) s.href = href
  if (last && !last.href && !href && sameStyle(last, s)) {
    last.text += text
    return
  }
  out.push(s)
}

function sameStyle(a: Span, b: Span): boolean {
  const flags: Flag[] = ['bold', 'italic', 'underline', 'strike', 'code']
  return flags.every((k) => !!a[k] === !!b[k])
}

const MD_MARKERS: [string, Flag][] = [
  ['**', 'bold'],
  ['__', 'underline'],
  ['++', 'underline'],
  ['~~', 'strike'],
  ['*', 'italic'],
  ['_', 'italic'],
  ['`', 'code'],
]

// MAX markdown: **bold**, *italic* / _italic_, ++underline++, ~~strike~~,
// `code`, [text](url). A marker without its closing pair stays literal.
export function parseMarkdown(src: string): Span[] {
  const out: Span[] = []
  const style: Style = {}
  const open = new Map<string, boolean>()
  let buf = ''
  let i = 0
  const flush = () => {
    push(out, buf, style)
    buf = ''
  }
  while (i < src.length) {
    if (!style.code && src[i] === '[') {
      const m = /^\[([^\]\n]+)\]\(([^)\s]+)\)/.exec(src.slice(i))
      if (m) {
        flush()
        push(out, m[1], style, safeHref(m[2]))
        i += m[0].length
        continue
      }
    }
    let matched = false
    for (const [mark, flag] of MD_MARKERS) {
      if (!src.startsWith(mark, i)) continue
      if (style.code && flag !== 'code') break
      if (open.get(mark)) {
        flush()
        open.set(mark, false)
        style[flag] = false
      } else if (src.indexOf(mark, i + mark.length) > i + mark.length) {
        flush()
        open.set(mark, true)
        style[flag] = true
      } else {
        break
      }
      i += mark.length
      matched = true
      break
    }
    if (matched) continue
    buf += src[i]
    i++
  }
  flush()
  return out
}

const ENTITIES: Record<string, string> = {
  '&lt;': '<',
  '&gt;': '>',
  '&amp;': '&',
  '&quot;': '"',
  '&#39;': "'",
  '&apos;': "'",
  '&nbsp;': ' ',
}

function decode(s: string): string {
  return s.replace(/&(lt|gt|amp|quot|#39|apos|nbsp);/g, (e) => ENTITIES[e] ?? e)
}

const HTML_TAGS: Record<string, Flag> = {
  b: 'bold',
  strong: 'bold',
  i: 'italic',
  em: 'italic',
  u: 'underline',
  ins: 'underline',
  s: 'strike',
  del: 'strike',
  code: 'code',
  pre: 'code',
}

// MAX html: b/strong, i/em, u/ins, s/del, code/pre, a href, br. Unknown tags
// are shown as text, entities are decoded.
export function parseHtml(src: string): Span[] {
  const out: Span[] = []
  const style: Style = {}
  let href: string | undefined
  const re = /<(\/?)([a-z]+)((?:\s+[a-z-]+="[^"]*")*)\s*(\/?)>/gi
  let last = 0
  for (let m = re.exec(src); m; m = re.exec(src)) {
    const [whole, close, rawName, attrs] = m
    const name = rawName.toLowerCase()
    const flag = HTML_TAGS[name]
    if (!flag && name !== 'a' && name !== 'br') continue
    push(out, decode(src.slice(last, m.index)), style, href)
    last = m.index + whole.length
    if (name === 'br') push(out, '\n', style, href)
    else if (name === 'a') {
      const h = /href="([^"]*)"/i.exec(attrs)
      href = close ? undefined : h ? safeHref(decode(h[1])) : undefined
    } else style[flag] = !close
  }
  push(out, decode(src.slice(last)), style, href)
  return out
}

export function renderMarkup(text: string, format: string): Span[] {
  if (format === 'markdown') return parseMarkdown(text)
  if (format === 'html') return parseHtml(text)
  return text ? [{ text }] : []
}

// ── Keyboard ────────────────────────────────────────────────────────────────

export type ButtonAction = 'press' | 'open' | 'send' | 'none'

export interface ChatButton {
  text: string
  action: ButtonAction
  payload: string
  url?: string
  type: string
}

// MAX inline keyboard by rows: callback presses, link opens a new tab, message
// sends its text as the newcomer; request_contact and friends have no meaning
// without a phone. Empty rows are dropped.
export function buttonRows(rows: DevChatButton[][] | null | undefined): ChatButton[][] {
  const out: ChatButton[][] = []
  for (const row of rows ?? []) {
    const r: ChatButton[] = []
    for (const b of row ?? []) {
      const base = { text: b.text, payload: b.payload ?? '', type: b.type }
      if (b.type === 'callback' && b.payload) r.push({ ...base, action: 'press' })
      else if (b.type === 'link' && b.url && safeHref(b.url))
        r.push({ ...base, action: 'open', url: safeHref(b.url) })
      else if (b.type === 'message') r.push({ ...base, action: 'send' })
      else r.push({ ...base, action: 'none' })
    }
    if (r.length > 0) out.push(r)
  }
  return out
}
