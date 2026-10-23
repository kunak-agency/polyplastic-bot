import {
  AxiosError,
  AxiosHeaders,
  type AxiosAdapter,
  type AxiosResponse,
  type InternalAxiosRequestConfig,
} from 'axios'

export interface MockRequest {
  method: string
  path: string
  // Values of the route's `:name` segments, e.g. { id: 'p1' } for /plans/:id.
  params: Record<string, string>
  // Query string of the request, e.g. { page: '1', q: 'text' }.
  // Axios hands the adapter `config.params` UNSERIALISED, so without this the
  // handlers saw no query at all and every GET answered the same fixture
  // whatever the caller asked for. A repeated key keeps its last value: no
  // endpoint of this API takes a list in the query.
  query: Record<string, string>
  body: Record<string, unknown>
  authorization: string | undefined
}

export interface MockResult {
  status: number
  data?: unknown
}

export type MockHandler = (req: MockRequest) => MockResult | Promise<MockResult>

export interface MockRoute {
  method: string
  // Matched against the end of the resolved pathname, so the API's base path
  // prefix (e.g. /v1) does not have to be repeated here. A `:name` segment
  // matches any single segment and lands in MockRequest.params.
  path: string
  handler: MockHandler
}

const MIN_DELAY_MS = 200
const MAX_DELAY_MS = 400

function delay(): Promise<void> {
  const ms = MIN_DELAY_MS + Math.random() * (MAX_DELAY_MS - MIN_DELAY_MS)
  return new Promise((resolve) => setTimeout(resolve, ms))
}

function resolvePath(config: InternalAxiosRequestConfig): string {
  const raw = config.url ?? ''
  try {
    return new URL(raw, config.baseURL || 'http://mock.local').pathname
  } catch {
    return raw
  }
}

// The query the caller asked with: the one already in the url, then
// `config.params` on top — the same precedence axios itself applies when it
// serialises a request.
function resolveQuery(config: InternalAxiosRequestConfig): Record<string, string> {
  const query: Record<string, string> = {}

  const raw = config.url ?? ''
  const mark = raw.indexOf('?')
  if (mark >= 0) {
    for (const [key, value] of new URLSearchParams(raw.slice(mark + 1))) query[key] = value
  }

  const params: unknown = config.params
  if (params instanceof URLSearchParams) {
    for (const [key, value] of params) query[key] = value
  } else if (typeof params === 'object' && params !== null) {
    for (const [key, value] of Object.entries(params as Record<string, unknown>)) {
      // undefined and null are how a caller says "no value": axios leaves such
      // a parameter out of the query string, so the mock must not invent one.
      if (value === undefined || value === null) continue
      query[key] = String(value)
    }
  }

  return query
}

function parseBody(data: unknown): Record<string, unknown> {
  if (typeof data === 'string') {
    try {
      const parsed: unknown = JSON.parse(data)
      return typeof parsed === 'object' && parsed !== null
        ? (parsed as Record<string, unknown>)
        : {}
    } catch {
      return {}
    }
  }
  if (typeof data === 'object' && data !== null) return data as Record<string, unknown>
  return {}
}

// Params of `route` if it matches the tail of `pathname`, otherwise null.
function matchPath(route: string, pathname: string): Record<string, string> | null {
  if (!route.includes(':')) return pathname.endsWith(route) ? {} : null
  const want = route.split('/').filter(Boolean)
  const have = pathname.split('/').filter(Boolean)
  if (have.length < want.length) return null
  const tail = have.slice(have.length - want.length)
  const params: Record<string, string> = {}
  for (const [i, segment] of want.entries()) {
    if (segment.startsWith(':')) params[segment.slice(1)] = decodeURIComponent(tail[i])
    else if (segment !== tail[i]) return null
  }
  return params
}

export function createMockAdapter(routes: MockRoute[]): AxiosAdapter {
  return async function mockAdapter(config: InternalAxiosRequestConfig): Promise<AxiosResponse> {
    await delay()

    const method = (config.method ?? 'get').toUpperCase()
    const path = resolvePath(config)
    const query = resolveQuery(config)
    const headers = AxiosHeaders.from(config.headers)
    const authorization = headers.get('Authorization')

    let route: MockRoute | undefined
    let params: Record<string, string> = {}
    for (const r of routes) {
      const matched = r.method === method ? matchPath(r.path, path) : null
      if (matched) {
        route = r
        params = matched
        break
      }
    }

    const result: MockResult = route
      ? await route.handler({
          method,
          path,
          params,
          query,
          body: parseBody(config.data),
          authorization: typeof authorization === 'string' ? authorization : undefined,
        })
      : {
          status: 404,
          data: { error: { code: 'not_found', message: `No mock for ${method} ${path}` } },
        }

    // Mocked calls never hit the network, so devtools' network tab stays empty —
    // this is the only trace of what the app actually requested, query included.
    const search = new URLSearchParams(query).toString()
    console.info(`[mocks] ${method} ${path}${search ? `?${search}` : ''} → ${result.status}`)

    const response: AxiosResponse = {
      data: result.data ?? null,
      status: result.status,
      statusText: String(result.status),
      headers: new AxiosHeaders({ 'content-type': 'application/json' }),
      config,
      request: { mock: true },
    }

    const isValid = config.validateStatus
      ? config.validateStatus(result.status)
      : result.status < 300
    if (isValid) return response

    // The response interceptor keys off error.response.status and error.config,
    // so the rejection has to look exactly like a real axios failure.
    throw new AxiosError(
      `Request failed with status code ${result.status}`,
      result.status >= 500 ? AxiosError.ERR_BAD_RESPONSE : AxiosError.ERR_BAD_REQUEST,
      config,
      response.request,
      response,
    )
  }
}
