import type { PluralizationRule } from 'vue-i18n'

// Russian 3-form pluralization, behaviorally identical to src/lib/plural.ts
// (pluralRu). vue-i18n calls this with the count and the number of message
// choices, and expects the index (0=one, 1=few, 2=many) into the pipe-separated
// message forms — the catalog writes them as "one | few | many".
//
// Reference mapping (must not regress vs. src/lib/plural.ts):
//   1, 21, 101      → one  (index 0)
//   2–4, 22–24      → few  (index 1)
//   0, 5–20, 11–14  → many (index 2)
export const russianPluralRule: PluralizationRule = (choice, choicesLength) => {
  const abs = Math.abs(choice) % 100
  const last = abs % 10

  let index: number
  if (abs > 10 && abs < 20) index = 2
  else if (last > 1 && last < 5) index = 1
  else if (last === 1) index = 0
  else index = 2

  // Guard against catalogs that ship fewer than 3 forms for a key.
  return index < choicesLength ? index : choicesLength - 1
}
