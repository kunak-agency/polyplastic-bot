// Role matrix of the cabinet (research 2026-10-24 §4, api_contract_auth.md).
// Pure, types-only imports: the node tests load it directly.
import type { Role } from '@/types/api'

// null — auth is off on the server (FEATURE_AUTH=false): the cabinet works as
// before, without the screens that need a signed-in user.
export type Viewer = Role | null

const NEEDS_USER = new Set(['dashboard'])

// Route names a role may open. `undefined` in the table means "everything".
const ONLY: Partial<Record<Role, ReadonlySet<string>>> = {
  manager: new Set(['manager']),
}
const DENY: Partial<Record<Role, ReadonlySet<string>>> = {
  // Users are managed in Settings, by the HR lead only.
  recruiter: new Set(['settings']),
}

export function canOpen(viewer: Viewer, routeName: string): boolean {
  if (viewer === null) return !NEEDS_USER.has(routeName)
  const only = ONLY[viewer]
  if (only) return only.has(routeName)
  return !DENY[viewer]?.has(routeName)
}

// Where a role lands after sign-in and on the bare "/".
export function homeRoute(viewer: Viewer): string {
  if (viewer === null) return 'overview'
  if (viewer === 'manager') return 'manager'
  // Recruiter and HR lead start on their "Today" (research role-dashboards).
  return 'dashboard'
}
