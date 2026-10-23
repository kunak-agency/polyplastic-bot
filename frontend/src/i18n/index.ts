import { createI18n } from 'vue-i18n'
import { russianPluralRule } from './pluralRules'
import { datetimeFormats, numberFormats } from './formats'

// Single-locale app (PRD decision D2): the catalog exists as the one place where
// user-facing copy is edited, not as a translation mechanism. There is no
// fallback locale and no runtime/env switcher — adding a locale later means
// adding a directory under ./locales and widening this type.
export type AppLocale = 'ru'

export const APP_LOCALE: AppLocale = 'ru'

// Recursive message catalog shape (leaves are pipe-plural/interpolated strings).
type MessageTree = { [key: string]: string | MessageTree }

// Each locales/ru/<namespace>.json becomes messages.ru.<namespace>, so a key like
// common.states.loading resolves from locales/ru/common.json.
// import.meta.glob is statically analyzed by Vite and bundled (no runtime fetch).
const modules = import.meta.glob<{ default: MessageTree }>('./locales/ru/*.json', {
  eager: true,
})

const messages: Record<AppLocale, Record<string, MessageTree>> = { ru: {} }
for (const path in modules) {
  const match = path.match(/\.\/locales\/ru\/(.+)\.json$/)
  if (!match) continue
  messages.ru[match[1]] = modules[path].default
}

const dev = import.meta.env.DEV

// Dev-only screens (the /dev/chat emulator) keep their copy in ./locales/dev:
// loaded only in `pnpm dev`, so a production build carries none of it.
if (dev) {
  const devModules = import.meta.glob<{ default: MessageTree }>('./locales/dev/*.json', {
    eager: true,
  })
  for (const path in devModules) {
    const match = path.match(/\.\/locales\/dev\/(.+)\.json$/)
    if (match) messages.ru[match[1]] = devModules[path].default
  }
}

export const i18n = createI18n({
  legacy: false,
  locale: APP_LOCALE,
  messages,
  pluralRules: { ru: russianPluralRule },
  datetimeFormats,
  numberFormats,
  missingWarn: dev,
  fallbackWarn: dev,
})
