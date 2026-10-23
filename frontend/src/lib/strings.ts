export function toTitleCase(s: string): string {
  if (!s) return ''
  return s.toLowerCase().replace(/(^|[\s\-_])(\p{L})/gu, (_, sep, ch) => sep + ch.toUpperCase())
}
