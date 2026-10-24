// Role matrix of the cabinet (research 2026-10-24 §4, api_contract_auth.md).
// Pure, types-only imports: the node tests load it directly.
import type { Role } from '@/types/api'

// null — auth is off on the server (FEATURE_AUTH=false): the cabinet works as
// before, without the screens that need a signed-in user.
export type Viewer = Role | null

// "Today" is per role; "Team" reads /hrlead/* — both need a signed-in HR lead or recruiter.
const NEEDS_USER = new Set(['dashboard', 'team'])

// Route names a role may open. `undefined` in the table means "everything".
const ONLY: Partial<Record<Role, ReadonlySet<string>>> = {
  manager: new Set(['manager']),
}
const DENY: Partial<Record<Role, ReadonlySet<string>>> = {
  // Users are managed in Settings and the team on "Team", by the HR lead only.
  // The head of unit's view ("manager") is not a recruiter's screen, nor are
  // "Indicators" (route "overview"): the HR lead's period outcomes.
  recruiter: new Set(['settings', 'team', 'manager', 'overview']),
}
// Open by link but not in the menu: the HR lead reaches the head of unit's view
// from Team → Heads of units and from the newcomer card, not as "My employees".
const NOT_IN_MENU: Partial<Record<Role, ReadonlySet<string>>> = {
  hr_lead: new Set(['manager']),
}

export function canOpen(viewer: Viewer, routeName: string): boolean {
  if (viewer === null) return !NEEDS_USER.has(routeName)
  const only = ONLY[viewer]
  if (only) return only.has(routeName)
  return !DENY[viewer]?.has(routeName)
}

// Whether the sidebar shows the item: openable and not a by-link-only screen.
export function inMenu(viewer: Viewer, routeName: string): boolean {
  if (!canOpen(viewer, routeName)) return false
  return viewer === null || !NOT_IN_MENU[viewer]?.has(routeName)
}

// Where a role lands after sign-in and on the bare "/".
export function homeRoute(viewer: Viewer): string {
  if (viewer === null) return 'overview'
  if (viewer === 'manager') return 'manager'
  // Recruiter and HR lead start on their "Today" (research role-dashboards).
  return 'dashboard'
}
