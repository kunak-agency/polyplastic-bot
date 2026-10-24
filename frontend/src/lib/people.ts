// People directory helpers (api_contract_auth.md, "People"): managers and
// mentors are picked from the directory, not typed, so the manager role finds
// its staff by id. Pure, types-only imports: the node tests load it directly.
import type { Person } from '@/types/api'

// Comparable form of a name: case, spaces and the yo letter do not matter.
export function normName(s: string): string {
  return s
    .trim()
    .replace(/\s+/g, ' ')
    .toLowerCase()
    .replace(/\u0451/g, '\u0435')
}

// People whose name contains the query, active first, then by name; the
// selected one stays in the list even when deactivated.
export function filterPeople(list: readonly Person[], query: string, selectedId = ''): Person[] {
  const q = normName(query)
  return list
    .filter((p) => p.id === selectedId || p.active)
    .filter((p) => !q || normName(p.full_name).includes(q))
    .slice()
    .sort((a, b) => Number(b.active) - Number(a.active) || a.full_name.localeCompare(b.full_name))
}

// The person with exactly this name (ignoring case and spaces), if any: the
// "new" form offers to pick them instead of creating a duplicate.
export function exactPerson(list: readonly Person[], name: string): Person | null {
  const n = normName(name)
  if (!n) return null
  return list.find((p) => normName(p.full_name) === n) ?? null
}

// Adds or replaces a person in a list (after POST /api/people), keeping order.
export function upsertPerson(list: readonly Person[], p: Person): Person[] {
  const i = list.findIndex((x) => x.id === p.id)
  if (i < 0) return [...list, p]
  const out = list.slice()
  out[i] = p
  return out
}
