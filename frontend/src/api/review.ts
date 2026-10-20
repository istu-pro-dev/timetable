export interface SourceRef {
  sheet: string
  row: number
  col: string
}

export interface Member {
  group: string
  subgroup: number | null
}

export type Parity = 'every' | 'odd' | 'even'

export interface Lesson {
  id: number
  refs: SourceRef[]
  audience: Member[]
  day: string
  period: number
  parity: Parity
  lesson_type: string
  discipline: string
  teachers: string[]
  room?: string
  place?: string
  note?: string
  flags?: string[]
}

export interface Violation {
  rule: string
  title: string
  message: string
  lesson_ids: number[]
  resource?: { kind: 'teacher' | 'group' | 'room'; name: string }
  day?: string
  period?: number
}

export interface Criterion {
  key: string
  title: string
  value: number
  weight: number
  weighted: number
  available: boolean
}

export interface EntityScore {
  name: string
  lessons: number
  gaps: number
  transitions: number
  max_per_day: number
  conflicts: number
}

export interface Issue {
  code: string
  ref?: SourceRef
  message: string
}

export interface Report {
  source: { id: string; adapter: string; file?: string; title?: string }
  periods: { n: number; start: string; end: string }[]
  stats: {
    lessons: number
    groups: number
    teachers: number
    rooms: number
    disciplines: number
    hard: number
  }
  lessons: Lesson[]
  rooms: { key: string; building: string; type: string; title: string; capacity: number | null }[]
  violations: Violation[]
  soft: Criterion[]
  soft_total: number
  groups: EntityScore[]
  teachers: EntityScore[]
  issues: Issue[]
  notes: string[]
}

export async function uploadReview(file: File): Promise<Report> {
  const body = new FormData()
  body.append('file', file)
  const res = await fetch('/api/review', { method: 'POST', body })
  if (!res.ok) {
    let message = `HTTP ${res.status}`
    try {
      const err = (await res.json()) as { error?: { message?: string } }
      if (err.error?.message) message = err.error.message
    } catch {
      // not JSON: keep the status
    }
    throw new Error(message)
  }
  return (await res.json()) as Report
}
