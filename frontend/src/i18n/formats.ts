import type { AppLocale } from './index'

type DateFormats = Record<string, Intl.DateTimeFormatOptions>
type NumberFormats = Record<string, Intl.NumberFormatOptions>

// Date/time presets consumed via $d('short'|'long', date).
export const datetimeFormats: Record<AppLocale, DateFormats> = {
  ru: {
    short: { year: 'numeric', month: '2-digit', day: '2-digit' },
    time: { hour: '2-digit', minute: '2-digit' },
    day: { weekday: 'long', day: 'numeric', month: 'long' },
    // Plan preview and timeline rows: short weekday plus dd.mm.
    weekdayShort: { weekday: 'short', day: '2-digit', month: '2-digit' },
    dayMonth: { day: '2-digit', month: '2-digit' },
    long: {
      year: 'numeric',
      month: 'long',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    },
  },
}

// signDisplay 'negative' is ES2023 Intl (every browser the app targets has
// it); the project's lib is ES2022, so the value is registered here exactly the
// way lib.es2023.intl.d.ts registers it.
declare global {
  // eslint-disable-next-line @typescript-eslint/no-namespace
  namespace Intl {
    interface NumberFormatOptionsSignDisplayRegistry {
      negative: never
    }
  }
}

// Numbers are shown at one fixed precision, so the same value reads the same
// on every screen. signDisplay 'negative'
// keeps float noise (-1e-15) from rendering as "-0,0"; the *Delta presets sign
// both directions and print an exact zero unsigned.
const oneDecimal: Intl.NumberFormatOptions = {
  minimumFractionDigits: 1,
  maximumFractionDigits: 1,
  signDisplay: 'negative',
}
const oneDecimalDelta: Intl.NumberFormatOptions = { ...oneDecimal, signDisplay: 'exceptZero' }

// Number presets consumed via $n('decimal'|'percent'|'currency'|…, n).
export const numberFormats: Record<AppLocale, NumberFormats> = {
  ru: {
    decimal: { style: 'decimal', maximumFractionDigits: 2 },
    percent: { style: 'percent', maximumFractionDigits: 1 },
    currency: { style: 'currency', currency: 'RUB', currencyDisplay: 'symbol' },
    // Whole rubles for scenario totals: kopecks there would be false precision.
    rub: {
      style: 'currency',
      currency: 'RUB',
      currencyDisplay: 'symbol',
      maximumFractionDigits: 0,
    },
    // A share (0..1) printed as a percentage: formatting, not arithmetic.
    share: { ...oneDecimal, style: 'percent' },
    shareDelta: { ...oneDecimalDelta, style: 'percent' },
    count: { maximumFractionDigits: 0, signDisplay: 'negative' },
    // Durations in seconds: «0,4 s».
    seconds: oneDecimal,
    countDelta: { maximumFractionDigits: 0, signDisplay: 'exceptZero' },
    // Years and week numbers are labels: no digit grouping ("2 026").
    ordinal: { maximumFractionDigits: 0, useGrouping: false },
  },
}
