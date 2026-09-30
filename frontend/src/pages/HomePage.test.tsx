import { screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { adminUser, mockFetch, renderPage } from '../test/utils.tsx'
import { HomePage } from './HomePage.tsx'

describe('HomePage', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('greets the user and shows API as running when healthz is ok', async () => {
    mockFetch({
      'GET /api/auth/me': () => adminUser,
      'GET /api/healthz': () => ({ status: 'ok' }),
    })
    renderPage(<HomePage />)
    expect(await screen.findByText('работает')).toBeInTheDocument()
    expect(await screen.findByText('Здравствуйте, Администратор!')).toBeInTheDocument()
  })

  it('shows API as unavailable on error', async () => {
    mockFetch({
      'GET /api/auth/me': () => adminUser,
      'GET /api/healthz': () => new Response(null, { status: 502 }),
    })
    renderPage(<HomePage />)
    expect(await screen.findByText('недоступен')).toBeInTheDocument()
  })
})
