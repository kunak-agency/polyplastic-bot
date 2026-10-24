import { http } from '@/api/http'
import { isApiError } from '@/lib/apiError'
import type { LoginRequest, User } from '@/types/api'

// api_contract_auth.md v5. The session is an HttpOnly cookie set by the
// server: nothing to store on this side.

// `{ user }` per the contract; a bare user object is accepted too.
function unwrap(data: unknown): User {
  const d = data as { user?: User } & Partial<User>
  return (d.user ?? d) as User
}

export async function apiLogin(body: LoginRequest): Promise<User> {
  const r = await http.post('/auth/login', body)
  return unwrap(r.data)
}

export async function apiLogout(): Promise<void> {
  await http.post('/auth/logout')
}

// 'disabled' — the server has no /auth routes (FEATURE_AUTH=false, 404);
// null — no session (401).
export async function apiMe(): Promise<User | null | 'disabled'> {
  try {
    const r = await http.get('/auth/me')
    return unwrap(r.data)
  } catch (e) {
    if (isApiError(e) && e.status === 404) return 'disabled'
    if (isApiError(e) && e.status === 401) return null
    throw e
  }
}
