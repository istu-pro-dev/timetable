import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Report } from '../api/review.ts'
import { ReviewPage } from './ReviewPage.tsx'

const lesson = (id: number, discipline: string, group: string) => ({
  id,
  refs: [{ sheet: '32', row: 3 + id, col: 'C' }],
  audience: [{ group, subgroup: null }],
  day: 'mon',
  period: 1,
  parity: 'odd' as const,
  lesson_type: 'lab',
  discipline,
  teachers: ['Соболева Н.В.'],
  room: '3-605',
})

const report: Report = {
  source: { id: 's', adapter: 'tandem-xls-t1', file: 't1.xls' },
  periods: [{ n: 1, start: '08:30', end: '10:00' }],
  stats: { lessons: 2, groups: 2, teachers: 1, rooms: 1, disciplines: 2, hard: 1 },
  lessons: [lesson(1, 'Базы данных', 'Б24-782-1'), lesson(2, 'Компьютерные сети', 'Б24-782-2')],
  rooms: [{ key: '3-605', building: '3', type: 'computer', title: '3-605', capacity: null }],
  violations: [
    {
      rule: 'H1',
      title: 'Накладка преподавателя',
      message: 'Преподаватель Соболева Н.В. ведёт 2 занятия одновременно: понедельник, 1-я пара',
      lesson_ids: [1, 2],
      resource: { kind: 'teacher', name: 'Соболева Н.В.' },
      day: 'mon',
      period: 1,
    },
  ],
  soft: [],
  soft_total: 0,
  groups: [
    { name: 'Б24-782-1', lessons: 1, gaps: 0, transitions: 0, max_per_day: 1, conflicts: 0 },
    { name: 'Б24-782-2', lessons: 1, gaps: 0, transitions: 0, max_per_day: 1, conflicts: 0 },
  ],
  teachers: [
    { name: 'Соболева Н.В.', lessons: 2, gaps: 0, transitions: 0, max_per_day: 1, conflicts: 1 },
  ],
  issues: [],
  notes: [],
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  const { container } = render(
    <QueryClientProvider client={client}>
      <ReviewPage />
    </QueryClientProvider>,
  )
  return container.querySelector('input[type=file]') as HTMLInputElement
}

describe('ReviewPage', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('uploads the file and shows conflicts in the grid', async () => {
    const fetchMock = vi.fn(() => Promise.resolve(Response.json(report)))
    vi.stubGlobal('fetch', fetchMock)
    const input = renderPage()
    fireEvent.change(input, { target: { files: [new File(['x'], 't1.xls')] } })

    expect(await screen.findByText(/ведёт 2 занятия одновременно/)).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/review',
      expect.objectContaining({ method: 'POST' }),
    )
    // The first group's grid shows its lesson, marked as conflicting.
    const card = screen.getByText('Базы данных').closest('.lesson')
    expect(card).toHaveClass('conflict')

    fireEvent.click(screen.getByText('показать в сетке'))
    expect(screen.getByText('Компьютерные сети')).toBeInTheDocument()
  })

  it('shows the server error', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          Response.json(
            { error: { code: 'unsupported_file', message: 'unsupported file' } },
            { status: 422 },
          ),
        ),
      ),
    )
    const input = renderPage()
    fireEvent.change(input, { target: { files: [new File(['x'], 'a.txt')] } })
    expect(await screen.findByText('Ошибка: unsupported file')).toBeInTheDocument()
  })
})
