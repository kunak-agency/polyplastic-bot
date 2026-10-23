// Which catalog message describes a failure that is not a backend answer: a
// JS Error (fetch/WebSocket/runtime) or an ApiError without a server text. The
// raw `message` of such an error is English and technical ("Failed to fetch",
// "Cannot read properties of undefined") — it never reaches the screen; the
// operator gets the situation in Russian from the catalog instead.
// Pure (no i18n import), so the node tests can load it.

const NETWORK_RE = /network|failed to fetch|load failed|fetch failed|err_connection/i

// i18n key for a non-API failure; `fallbackKey` when the situation is unknown.
export function jsErrorKey(e: unknown, fallbackKey: string): string {
  if (!(e instanceof Error)) return fallbackKey
  if (e.name === 'TimeoutError' || /timeout|timed out/i.test(e.message))
    return 'common.errors.timeout'
  if (e.name === 'NetworkError' || NETWORK_RE.test(e.message)) return 'common.errors.network'
  return fallbackKey
}

// i18n key for an API answer without a server text: status 0 means the request
// never reached the server (no network, server down, CORS); otherwise the
// caller's own fallback says best what failed.
export function statusKey(status: number, fallbackKey: string): string {
  return status === 0 ? 'common.errors.network' : fallbackKey
}
