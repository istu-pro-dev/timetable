// Small fetch wrapper for the REST API (backend/api/openapi.yaml).
//
// Sessions live in HttpOnly cookies (tt_access, tt_refresh), so requests only need
// same-origin credentials. A 401 triggers one POST /api/auth/refresh (shared by concurrent
// requests) and a retry; if the session cannot be refreshed the unauthorized handler runs
// (the auth provider clears the user, which redirects to /login).
import type { ApiErrorBody } from './types.ts'

export type HttpMethod = 'GET' | 'POST' | 'PUT' | 'DELETE'
export type QueryParams = Record<string, string | number | boolean | null | undefined>

export interface RequestOptions {
  method?: HttpMethod
  body?: unknown
  query?: QueryParams
  signal?: AbortSignal
  /** Do not try to refresh the session on 401 (auth endpoints). */
  noRefresh?: boolean
}

/** An API failure: the HTTP status plus `{"error":{"code","message"}}` from the body. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  /** Seconds from the Retry-After header (429). */
  readonly retryAfter: number | undefined

  constructor(status: number, code: string, message: string, retryAfter?: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.retryAfter = retryAfter
  }
}

export function isApiError(err: unknown, status?: number): err is ApiError {
  return err instanceof ApiError && (status === undefined || err.status === status)
}

let onUnauthorized: (() => void) | undefined

/** Registers the callback run when a request stays unauthorized after a refresh attempt. */
export function setUnauthorizedHandler(handler: (() => void) | undefined): void {
  onUnauthorized = handler
}

let refreshing: Promise<boolean> | undefined

/** Rotates the refresh session; concurrent callers share one request. Resolves to success. */
export function refreshSession(): Promise<boolean> {
  refreshing ??= fetch('/api/auth/refresh', { method: 'POST', credentials: 'same-origin' })
    .then((res) => res.ok)
    .catch(() => false)
    .finally(() => {
      refreshing = undefined
    })
  return refreshing
}

function buildURL(path: string, query?: QueryParams): string {
  if (!query) return path
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== null && value !== '') params.set(key, String(value))
  }
  const qs = params.toString()
  return qs ? `${path}?${qs}` : path
}

function send(path: string, opts: RequestOptions): Promise<Response> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  let body: string | undefined
  if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(opts.body)
  }
  return fetch(buildURL(path, opts.query), {
    method: opts.method ?? 'GET',
    headers,
    body,
    signal: opts.signal,
    credentials: 'same-origin',
  })
}

async function toApiError(res: Response): Promise<ApiError> {
  let code = `http_${res.status}`
  let message = res.statusText || `HTTP ${res.status}`
  try {
    const data = (await res.json()) as Partial<ApiErrorBody>
    if (data.error?.code) code = data.error.code
    if (data.error?.message) message = data.error.message
  } catch {
    // Not a JSON error body (proxy error page etc.) — keep the HTTP status.
  }
  const retry = Number(res.headers.get('Retry-After'))
  return new ApiError(
    res.status,
    code,
    message,
    Number.isFinite(retry) && retry > 0 ? retry : undefined,
  )
}

/** Performs a request and decodes the JSON response; throws ApiError on a non-2xx status. */
export async function apiFetch<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  let res = await send(path, opts)
  if (res.status === 401 && !opts.noRefresh) {
    if (await refreshSession()) res = await send(path, opts)
    if (res.status === 401) onUnauthorized?.()
  }
  if (!res.ok) throw await toApiError(res)
  if (res.status === 204 || res.headers.get('Content-Length') === '0') return undefined as T
  return (await res.json()) as T
}

export const api = {
  get: <T>(path: string, opts?: Omit<RequestOptions, 'method' | 'body'>) =>
    apiFetch<T>(path, { ...opts, method: 'GET' }),
  post: <T>(path: string, body?: unknown, opts?: Omit<RequestOptions, 'method' | 'body'>) =>
    apiFetch<T>(path, { ...opts, method: 'POST', body }),
  put: <T>(path: string, body?: unknown, opts?: Omit<RequestOptions, 'method' | 'body'>) =>
    apiFetch<T>(path, { ...opts, method: 'PUT', body }),
  delete: (path: string, opts?: Omit<RequestOptions, 'method' | 'body'>) =>
    apiFetch<void>(path, { ...opts, method: 'DELETE' }),
}
