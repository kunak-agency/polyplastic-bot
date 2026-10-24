import { createI18n } from 'vue-i18n'
import { russianPluralRule } from '@/i18n/pluralRules'
import { datetimeFormats, numberFormats } from '@/i18n/formats'
import common from '@/i18n/locales/ru/common.json'
import call from '@/i18n/locales/ru/call.json'

// The call page carries only its own catalogue and the shared one (the logo's
// brand word lives in common), not the whole cabinet. Same shape as
// src/i18n/index.ts: locales/ru/<namespace>.json → messages.ru.<namespace>.
export const i18n = createI18n({
  legacy: false,
  locale: 'ru',
  messages: { ru: { common, call } },
  pluralRules: { ru: russianPluralRule },
  datetimeFormats,
  numberFormats,
  missingWarn: import.meta.env.DEV,
  fallbackWarn: import.meta.env.DEV,
})
