// Russian pluralization: pick the right form for a count.
// forms = [one, few, many] — e.g. the three noun forms for "day".
//   1, 21, 101 -> one;  2-4, 22-24 -> few;  0, 5-20, 11-14 -> many.
export function pluralRu(n: number, forms: [string, string, string]): string {
  const abs = Math.abs(n) % 100
  const last = abs % 10
  if (abs > 10 && abs < 20) return forms[2]
  if (last > 1 && last < 5) return forms[1]
  if (last === 1) return forms[0]
  return forms[2]
}
