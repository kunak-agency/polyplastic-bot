import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { apiLogin, apiLogout, apiMe } from '@/api/auth'
import type { User } from '@/types/api'
import type { Viewer } from '@/lib/roles'

// unknown — not asked yet; disabled — the server runs without auth
// (FEATURE_AUTH=false, /auth/me is 404); anon — auth is on, no session.
export type AuthStatus = 'unknown' | 'disabled' | 'anon' | 'authed'

export const useAuthStore = defineStore('auth', () => {
  const status = ref<AuthStatus>('unknown')
  const user = ref<User | null>(null)
  let pending: Promise<void> | null = null

  // Asked once per page load; the guard awaits it before every navigation.
  function ensure(): Promise<void> {
    if (status.value !== 'unknown') return Promise.resolve()
    pending ??= apiMe()
      .then((me) => {
        if (me === 'disabled') status.value = 'disabled'
        else if (me) {
          user.value = me
          status.value = 'authed'
        } else status.value = 'anon'
      })
      .catch(() => {
        // Network or 5xx: let the cabinet open as before; real calls then
        // show their own errors, and a 401 among them still leads to /login.
        status.value = 'disabled'
      })
      .finally(() => {
        pending = null
      })
    return pending
  }

  async function login(loginName: string, password: string): Promise<User> {
    const me = await apiLogin({ login: loginName, password })
    user.value = me
    status.value = 'authed'
    return me
  }

  async function logout(): Promise<void> {
    try {
      await apiLogout()
    } finally {
      dropSession()
    }
  }

  // The server said 401: the session expired or was revoked.
  function dropSession(): void {
    user.value = null
    status.value = 'anon'
  }

  const enabled = computed(() => status.value === 'anon' || status.value === 'authed')
  // Role for the matrix in lib/roles.ts; null while auth is off.
  const viewer = computed<Viewer>(() => user.value?.role ?? null)

  return { status, user, enabled, viewer, ensure, login, logout, dropSession }
})
