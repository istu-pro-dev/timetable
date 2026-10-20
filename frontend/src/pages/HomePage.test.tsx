import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { HomePage } from './HomePage.tsx'

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <HomePage />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('HomePage', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('shows API as running when healthz is ok', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(Response.json({ status: 'ok' }))),
    )
    renderPage()
    expect(await screen.findByText('работает')).toBeInTheDocument()
  })

  it('shows API as unavailable on error', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response(null, { status: 502 }))),
    )
    renderPage()
    expect(await screen.findByText('недоступен')).toBeInTheDocument()
  })
})
