import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import type { ReactElement } from 'react'
import { createMemoryRouter, RouterProvider, type RouteObject } from 'react-router'
import { vi } from 'vitest'
import type { User } from '../api/types.ts'
import { AuthProvider } from '../auth/AuthProvider.tsx'

export interface MockRequest {
  method: string
  url: URL
  body: unknown
}

/** A handler returns a Response, a JSON-able value (200) or undefined (404). */
export type MockHandler = (req: MockRequest) => Response | unknown | Promise<Response | unknown>

/**
 * Stubs global fetch with handlers keyed by "METHOD /path" (query string ignored). Unmatched
 * requests answer 404. Returns the mock so tests can inspect calls.
 */
export function mockFetch(handlers: Record<string, MockHandler>) {
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), 'http://localhost')
    const method = (init?.method ?? 'GET').toUpperCase()
    const handler = handlers[`${method} ${url.pathname}`]
    if (!handler) {
      return Response.json({ error: { code: 'not_found', message: 'no mock' } }, { status: 404 })
    }
    const body = typeof init?.body === 'string' ? (JSON.parse(init.body) as unknown) : undefined
    const result = await handler({ method, url, body })
    if (result instanceof Response) return result
    if (result === undefined) return new Response(null, { status: 204 })
    return Response.json(result)
  })
  vi.stubGlobal('fetch', fn)
  return fn
}

/** The requests a fetch mock received, as "METHOD /path?query". */
export function calls(fn: ReturnType<typeof mockFetch>): string[] {
  return fn.mock.calls.map(([input, init]) => {
    const url = new URL(String(input), 'http://localhost')
    return `${(init?.method ?? 'GET').toUpperCase()} ${url.pathname}${url.search}`
  })
}

export function apiError(status: number, code: string, message = code): Response {
  return Response.json({ error: { code, message } }, { status })
}

export const adminUser: User = {
  id: 1,
  login: 'admin',
  role: 'admin',
  display_name: 'Администратор',
  teacher_id: null,
  group_id: null,
  created_at: '2026-09-01T00:00:00Z',
}

export const studentUser: User = {
  ...adminUser,
  id: 2,
  login: 'student',
  role: 'student',
  display_name: 'Студент',
}

export function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
}

/** Renders routes in a memory router with the query client and auth provider. */
export function renderRoutes(routes: RouteObject[], initialPath = '/') {
  const client = createTestQueryClient()
  const router = createMemoryRouter(routes, { initialEntries: [initialPath] })
  const result = render(
    <QueryClientProvider client={client}>
      <AuthProvider>
        <RouterProvider router={router} />
      </AuthProvider>
    </QueryClientProvider>,
  )
  return { ...result, router, client }
}

/** Renders a single element at `path` (for pages that use router hooks). */
export function renderPage(element: ReactElement, path = '/') {
  return renderRoutes([{ path: '*', element }], path)
}
