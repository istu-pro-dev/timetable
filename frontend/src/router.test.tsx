import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { User } from './api/types.ts'
import { routes } from './router.tsx'
import { adminUser, apiError, calls, mockFetch, renderRoutes, studentUser } from './test/utils.tsx'

function backend(initial: User | null) {
  let user = initial
  return mockFetch({
    'GET /api/auth/me': () => user ?? apiError(401, 'unauthorized'),
    'POST /api/auth/refresh': () => apiError(401, 'unauthorized'),
    'POST /api/auth/login': ({ body }) => {
      const { login, password } = body as { login: string; password: string }
      if (login !== 'admin' || password !== 'secret') return apiError(401, 'unauthorized')
      user = adminUser
      return { user, access_token: 'jwt', expires_at: '2026-10-01T00:00:00Z' }
    },
    'POST /api/auth/logout': () => {
      user = null
      return undefined
    },
    'GET /api/healthz': () => ({ status: 'ok' }),
  })
}

describe('routing and session', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('redirects to /login without a session (after one refresh attempt)', async () => {
    const fetch = backend(null)
    const { router } = renderRoutes(routes, '/buildings')
    expect(await screen.findByRole('heading', { name: 'Вход в систему' })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/login')
    expect(calls(fetch)).toEqual(['GET /api/auth/me', 'POST /api/auth/refresh'])
  })

  it('logs in and returns to the requested page', async () => {
    backend(null)
    const user = userEvent.setup()
    const { router } = renderRoutes(routes, '/buildings')
    await user.type(await screen.findByLabelText('Логин'), 'admin')
    await user.type(screen.getByLabelText('Пароль'), 'secret')
    await user.click(screen.getByRole('button', { name: 'Войти' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/buildings'))
    expect(await screen.findByRole('navigation', { name: 'Навигация' })).toBeInTheDocument()
  })

  it('shows an error for wrong credentials', async () => {
    backend(null)
    const user = userEvent.setup()
    renderRoutes(routes, '/login')
    await user.type(await screen.findByLabelText('Логин'), 'admin')
    await user.type(screen.getByLabelText('Пароль'), 'wrong')
    await user.click(screen.getByRole('button', { name: 'Войти' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Неверный логин или пароль')
  })

  it('validates empty fields without calling the API', async () => {
    const fetch = backend(null)
    const user = userEvent.setup()
    renderRoutes(routes, '/login')
    await user.click(await screen.findByRole('button', { name: 'Войти' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Введите логин и пароль')
    expect(calls(fetch)).not.toContain('POST /api/auth/login')
  })

  it('shows management sections to admins', async () => {
    backend(adminUser)
    renderRoutes(routes, '/')
    const nav = await screen.findByRole('navigation', { name: 'Навигация' })
    expect(within(nav).getByRole('link', { name: 'Корпуса' })).toBeInTheDocument()
    expect(within(nav).getByText('Справочники')).toBeInTheDocument()
  })

  it('hides management sections from other roles and forbids their routes', async () => {
    backend(studentUser)
    renderRoutes(routes, '/buildings')
    expect(await screen.findByText('Недостаточно прав для этого раздела')).toBeInTheDocument()
    const nav = screen.getByRole('navigation', { name: 'Навигация' })
    expect(within(nav).queryByRole('link', { name: 'Корпуса' })).not.toBeInTheDocument()
    expect(within(nav).getByRole('link', { name: 'Главная' })).toBeInTheDocument()
  })

  it('logs out and goes to /login', async () => {
    const fetch = backend(adminUser)
    const user = userEvent.setup()
    const { router } = renderRoutes(routes, '/')
    await user.click(await screen.findByRole('button', { name: 'Выйти' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
    expect(calls(fetch)).toContain('POST /api/auth/logout')
  })

  it('redirects a logged-in user away from /login', async () => {
    backend(adminUser)
    const { router } = renderRoutes(routes, '/login')
    await waitFor(() => expect(router.state.location.pathname).toBe('/'))
  })

  it('shows not found for unknown routes', async () => {
    backend(adminUser)
    renderRoutes(routes, '/nope')
    expect(await screen.findByText('Страница не найдена')).toBeInTheDocument()
  })
})
