import type { ApiError } from '@/types/api'
import { i18n } from '@/i18n'
import { jsErrorKey, statusKey } from '@/lib/errorKey'

// The http interceptor rejects with a plain ApiError ({ status, message, ... }),
// which is NOT an Error instance — so `err instanceof Error` misses it. Use this
// helper everywhere we surface a request failure to the user.
export function isApiError(e: unknown): e is ApiError {
  return typeof e === 'object' && e !== null && 'status' in e && 'message' in e
}

// Machine-readable backend error code → i18n key, for codes that deserve copy of
// their own. Application errors already carry a Russian `message`
// (api_contract.md), so the table is empty and the server's own text is shown;
// a code listed here would override it.
const CODE_MESSAGE_KEYS: Record<string, string> = {}

function codeMessage(e: ApiError): string | null {
  const key = e.code ? CODE_MESSAGE_KEYS[e.code] : undefined
  return key ? i18n.global.t(key) : null
}

// `fallbackKey` is an i18n key, not text — user-facing strings live in the
// catalog only.
export function errorMessage(e: unknown, fallbackKey = 'common.errors.generic'): string {
  if (isApiError(e)) {
    // Prefer a friendly code message, then the backend's own text. Never surface
    // the generic axios "Request failed with status code N" — name the situation
    // by status instead.
    const own = codeMessage(e) ?? e.serverMessage
    if (!own) return i18n.global.t(statusKey(e.status, fallbackKey))
    // A 422 says "check the data" and names the field in details: show the
    // first field's own text too, or the user cannot tell what to fix.
    const first = e.fields?.[0]?.message
    return first && !own.includes(first) ? `${own}: ${first}` : own
  }
  // A JS error's own message is English and technical: show the situation.
  return i18n.global.t(jsErrorKey(e, fallbackKey))
}
