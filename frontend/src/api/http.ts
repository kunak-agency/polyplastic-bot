import axios, { AxiosError, type AxiosInstance, type InternalAxiosRequestConfig } from 'axios'
import type { ApiError, FieldProblem } from '@/types/api'

const baseURL = import.meta.env.VITE_API_BASE_URL || '/api'

// The only HTTP client of the app: normalised errors and the mock adapter
// (src/mocks) hang off this instance, so a second axios.create would bypass both.
export const http: AxiosInstance = axios.create({
  baseURL,
  timeout: 15_000,
  headers: { 'Content-Type': 'application/json' },
})

// Dev-only assertion: every request must target the configured API origin — the
// only external origin the CSP allows. A stray absolute URL fails silently in the
// browser otherwise, which is hard to spot.
http.interceptors.request.use((config: InternalAxiosRequestConfig) => {
  if (import.meta.env?.DEV) {
    try {
      const url = new URL(
        config.url ?? '',
        new URL(config.baseURL ?? baseURL, window.location.origin),
      )
      const allowed = new URL(baseURL, window.location.origin)
      if (url.origin !== allowed.origin) {
        console.error(
          `[http] Blocked request to non-configured origin: ${url.origin} (allowed: ${allowed.origin})`,
        )
      }
    } catch {
      // ignore URL parsing issues in dev assertion
    }
  }

  return config
})

// Contract shape: { "error": { "code", "message", "traceId" } }
// (agency/system/api_contract.md, same as clinik/toplivo). Flat and
// problem+json fields are accepted too, so a proxy in front of the API that
// answers in another shape still yields a message.
interface ErrorBody {
  error?: {
    code?: string
    message?: string
    traceId?: string
    details?: unknown
  }
  message?: string
  detail?: string
  code?: string
  traceId?: string
  details?: unknown
  errors?: FieldProblem[]
}

// The backend puts per-field problems into error.details as
// [{ field, code, message }] (422 VALIDATION_FAILED); problem+json uses errors[].
function fieldProblems(raw: unknown): FieldProblem[] | undefined {
  if (!Array.isArray(raw)) return undefined
  const list = raw.filter(
    (p): p is FieldProblem =>
      typeof p === 'object' &&
      p !== null &&
      typeof (p as FieldProblem).field === 'string' &&
      typeof (p as FieldProblem).message === 'string',
  )
  return list.length > 0 ? list : undefined
}

function toApiError(error: AxiosError<ErrorBody>): ApiError {
  const data = error.response?.data
  const env = data?.error
  const serverMessage = env?.message ?? data?.message ?? data?.detail
  const details = env?.details ?? data?.details
  return {
    status: error.response?.status ?? 0,
    message: serverMessage ?? error.message ?? 'Unknown error',
    serverMessage,
    code: env?.code ?? data?.code,
    traceId: env?.traceId ?? data?.traceId,
    details,
    fields: fieldProblems(details) ?? fieldProblems(data?.errors),
  }
}

// Every failure reaches callers as a plain ApiError, never as an AxiosError —
// see lib/apiError.ts for the matching type guard.
http.interceptors.response.use(
  (response) => response,
  (error: AxiosError<ErrorBody>) => Promise.reject(toApiError(error)),
)
