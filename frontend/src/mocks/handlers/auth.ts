import type { MockRequest, MockResult, MockRoute } from '../adapter'
import { ensureSeeded, publicUser, users } from '../state'
import type { User } from '@/types/api'

// Login and sessions of the mock (api_contract_auth.md). VITE_MOCK_AUTH=false
// plays FEATURE_AUTH=false: /auth/* answer 404 and nothing asks for a session.
export const MOCK_AUTH = import.meta.env.VITE_MOCK_AUTH !== 'false'

// The "cookie": the user id in sessionStorage, so a reload keeps the session.
const KEY = 'korsik.mock.session'

function readSession(): string | null {
  try {
    return sessionStorage.getItem(KEY)
  } catch {
    return null
  }
}
function writeSession(id: string | null): void {
  try {
    if (id) sessionStorage.setItem(KEY, id)
    else sessionStorage.removeItem(KEY)
  } catch {
    // storage blocked: the session lives until reload
  }
}

// The signed-in user, or null (also null when auth is off).
export function currentUser(): User | null {
  if (!MOCK_AUTH) return null
  ensureSeeded()
  const id = readSession()
  const u = id ? users.get(id) : undefined
  return u && u.active !== false ? publicUser(u) : null
}

function err(status: number, code: string, message: string): MockResult {
  return { status, data: { error: { code, message, traceId: `mock-${Date.now()}` } } }
}

export function forbidden(): MockResult {
  return err(403, 'FORBIDDEN', 'Недостаточно прав')
}

const OPEN = [/\/auth\/login$/, /\/auth\/logout$/, /\/auth\/me$/, /\/health$/]

// 401 for every call without a session while auth is on; manager gets 403
// outside his own view, recruiter inside it (the role matrix of the contract).
export function authGate(req: MockRequest): MockResult | null {
  if (!MOCK_AUTH || OPEN.some((re) => re.test(req.path))) return null
  const me = currentUser()
  if (!me) return err(401, 'UNAUTHENTICATED', 'Войдите в кабинет')
  if (me.role === 'manager' && !/\/manager\/(newcomers|tasks\/[^/]+)$/.test(req.path))
    return forbidden()
  // The head of unit's view is not a recruiter's screen.
  if (me.role === 'recruiter' && /\/manager\/[^/]+(\/[^/]+)?$/.test(req.path)) return forbidden()
  // "Indicators" (and the old overview) are the HR lead's screen.
  if (me.role === 'recruiter' && /\/(indicators|overview)$/.test(req.path)) return forbidden()
  return null
}

function login(req: MockRequest): MockResult {
  ensureSeeded()
  const loginName = typeof req.body.login === 'string' ? req.body.login.trim() : ''
  const password = typeof req.body.password === 'string' ? req.body.password : ''
  const u = [...users.values()].find((x) => x.login === loginName)
  // No password (a person of the directory) or a mentor: never signs in.
  if (!u || !u.password || u.password !== password || u.active === false || u.role === 'mentor')
    return err(401, 'INVALID_CREDENTIALS', 'Неверный логин или пароль')
  writeSession(u.id)
  return { status: 200, data: { user: publicUser(u) } }
}

const routes: MockRoute[] = [
  { method: 'POST', path: '/auth/login', handler: login },
  {
    method: 'POST',
    path: '/auth/logout',
    handler: () => {
      writeSession(null)
      return { status: 204 }
    },
  },
  {
    method: 'GET',
    path: '/auth/me',
    handler: () => {
      const me = currentUser()
      return me ? { status: 200, data: { user: me } } : err(401, 'UNAUTHENTICATED', 'Нет сессии')
    },
  },
]

export const authRoutes: MockRoute[] = MOCK_AUTH ? routes : []
