import { createApp } from 'vue'
import { createPinia } from 'pinia'
import CallApp from './CallApp.vue'
import { i18n } from './i18n'
import '@/styles/global.css'

// Second Vite entry (call.html): the browser call to Korsik on its own domain.
// No router, no auth, no cabinet stores: one page.
document.documentElement.setAttribute('data-theme', 'light')

createApp(CallApp).use(createPinia()).use(i18n).mount('#app')
