import '@/lib/preboot'
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { i18n } from '@/i18n'
import { router } from '@/router'
import { useThemeStore } from '@/stores/theme'
import { http } from '@/api/http'
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

app.use(router)
app.mount('#app')
