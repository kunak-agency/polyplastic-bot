import '@/lib/preboot'
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { i18n } from '@/i18n'
import { router } from '@/router'
import { useThemeStore } from '@/stores/theme'
import { http, onUnauthorized } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { installMocks } from '@/mocks'
import '@/styles/global.css'

// Mocks must be wired before the first request leaves the app
// (no-op unless VITE_USE_MOCKS is exactly "true").
installMocks(http)

const app = createApp(App)
const pinia = createPinia()

app.use(pinia)
app.use(i18n)

// Theme store: applies persisted DOM attrs before mount.
useThemeStore()

// Any 401 outside /auth/*: the session is gone — back to the login screen,
// returning to the same place afterwards.
onUnauthorized(() => {
  useAuthStore().dropSession()
  const current = router.currentRoute.value
  if (current.meta.public) return
  void router.replace({ name: 'login', query: { next: current.fullPath } })
})

app.use(router)
app.mount('#app')
