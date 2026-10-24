// Copied from hack-ru-clinik frontend/src/lib/wsUrl.ts (unchanged in substance).
// ws(s) address of an API path: the API base with its scheme mapped
// http→ws / https→wss (the same origin the CSP lists in connect-src). A relative
// base such as `/api` resolves against the page origin first.
export function apiWsUrl(apiBase: string, path: string, pageOrigin: string): string {
  const url = new URL(apiBase, pageOrigin)
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  url.pathname = `${url.pathname.replace(/\/+$/, '')}/${path.replace(/^\/+/, '')}`
  url.search = ''
  url.hash = ''
  return url.toString()
}
