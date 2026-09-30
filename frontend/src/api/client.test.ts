import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiError, calls, mockFetch } from '../test/utils.tsx'
import { api, ApiError, setUnauthorizedHandler } from './client.ts'

describe('api client', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    setUnauthorizedHandler(undefined)
  })

  it('decodes JSON and sends JSON bodies with query params', async () => {
    const fetch = mockFetch({
      'POST /api/buildings': ({ body, url }) => ({ id: 1, ...(body as object), q: url.search }),
    })
    const res = await api.post('/api/buildings', { name: 'A' }, { query: { x: 1, y: undefined } })
    expect(res).toEqual({ id: 1, name: 'A', q: '?x=1' })
    const init = fetch.mock.calls[0]?.[1]
    expect(init?.headers).toMatchObject({ 'Content-Type': 'application/json' })
    expect(init?.credentials).toBe('same-origin')
  })

  it('returns undefined for 204', async () => {
    mockFetch({ 'DELETE /api/buildings/1': () => undefined })
    await expect(api.delete('/api/buildings/1')).resolves.toBeUndefined()
  })

  it('throws ApiError with the code and message of the error body', async () => {
    mockFetch({ 'GET /api/rooms': () => apiError(409, 'in_use', 'room is used') })
    const err = await api.get('/api/rooms').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toMatchObject({ status: 409, code: 'in_use', message: 'room is used' })
  })

  it('keeps the HTTP status when the error body is not JSON', async () => {
    mockFetch({ 'GET /api/rooms': () => new Response('bad gateway', { status: 502 }) })
    await expect(api.get('/api/rooms')).rejects.toMatchObject({ status: 502, code: 'http_502' })
  })

  it('refreshes the session once on 401 and retries', async () => {
    let authorized = false
    const fetch = mockFetch({
      'GET /api/groups': () => (authorized ? [{ id: 1 }] : apiError(401, 'unauthorized')),
      'POST /api/auth/refresh': () => {
        authorized = true
        return { user: {}, access_token: 'x', expires_at: '' }
      },
    })
    await expect(api.get('/api/groups')).resolves.toEqual([{ id: 1 }])
    expect(calls(fetch)).toEqual(['GET /api/groups', 'POST /api/auth/refresh', 'GET /api/groups'])
  })

  it('shares one refresh between concurrent requests', async () => {
    let authorized = false
    const fetch = mockFetch({
      'GET /api/groups': () => (authorized ? [] : apiError(401, 'unauthorized')),
      'GET /api/teachers': () => (authorized ? [] : apiError(401, 'unauthorized')),
      'POST /api/auth/refresh': async () => {
        await new Promise((r) => setTimeout(r, 5))
        authorized = true
        return {}
      },
    })
    await Promise.all([api.get('/api/groups'), api.get('/api/teachers')])
    expect(calls(fetch).filter((c) => c === 'POST /api/auth/refresh')).toHaveLength(1)
  })

  it('calls the unauthorized handler when the refresh fails', async () => {
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)
    mockFetch({
      'GET /api/groups': () => apiError(401, 'unauthorized'),
      'POST /api/auth/refresh': () => apiError(401, 'unauthorized'),
    })
    await expect(api.get('/api/groups')).rejects.toMatchObject({ status: 401 })
    expect(onUnauthorized).toHaveBeenCalledOnce()
  })

  it('does not refresh for auth endpoints', async () => {
    const fetch = mockFetch({ 'POST /api/auth/login': () => apiError(401, 'unauthorized') })
    await expect(
      api.post('/api/auth/login', { login: 'a', password: 'b' }, { noRefresh: true }),
    ).rejects.toMatchObject({ status: 401 })
    expect(calls(fetch)).toEqual(['POST /api/auth/login'])
  })

  it('reads Retry-After on 429', async () => {
    mockFetch({
      'POST /api/auth/login': () =>
        Response.json(
          { error: { code: 'too_many_attempts', message: 'slow down' } },
          { status: 429, headers: { 'Retry-After': '30' } },
        ),
    })
    await expect(api.post('/api/auth/login', {}, { noRefresh: true })).rejects.toMatchObject({
      retryAfter: 30,
    })
  })
})
