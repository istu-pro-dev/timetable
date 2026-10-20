import { useMutation } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import {
  uploadReview,
  type EntityScore,
  type Lesson,
  type Report,
  type SourceRef,
} from '../api/review.ts'

const DAYS = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat'] as const
const DAY_NAMES: Record<string, string> = {
  mon: 'Пн',
  tue: 'Вт',
  wed: 'Ср',
  thu: 'Чт',
  fri: 'Пт',
  sat: 'Сб',
  sun: 'Вс',
}
const TYPE_NAMES: Record<string, string> = {
  lecture: 'лек',
  practice: 'пр',
  lab: 'лаб',
  curator: 'кур',
  internship: 'практика',
  credit: 'зачёт',
  graded_credit: 'диф. зачёт',
  exam: 'экзамен',
  consultation: 'конс',
  other: '?',
}
const PLACE_NAMES: Record<string, string> = { online: 'онлайн', none: 'без аудитории' }

type EntityKind = 'group' | 'teacher' | 'room'
type Week = 'odd' | 'even'

function refText(r?: SourceRef): string {
  return r ? `лист ${r.sheet}, ${r.col}${r.row}` : ''
}

function involves(l: Lesson, kind: EntityKind, name: string): boolean {
  if (kind === 'group') return l.audience.some((m) => m.group === name)
  if (kind === 'teacher') return l.teachers.includes(name)
  return l.room === name
}

export function ReviewPage() {
  const review = useMutation({ mutationFn: uploadReview })

  return (
    <section className="review">
      <h1>Проверка расписания</h1>
      <p className="muted">
        Загрузите расписание из «Тандема» (.xls / .xlsx) или файл в формате ttx (JSON Lines). Файл
        не сохраняется: система разбирает его и проверяет накладки и качество.
      </p>
      <label className="upload">
        <input
          type="file"
          accept=".xls,.xlsx,.jsonl,.ttx,.json"
          onChange={(e) => {
            const f = e.target.files?.[0]
            if (f) review.mutate(f)
          }}
        />
      </label>
      {review.isPending && <p>Разбор и проверка…</p>}
      {review.isError && <p className="error">Ошибка: {review.error.message}</p>}
      {review.data && <ReportView report={review.data} />}
    </section>
  )
}

function ReportView({ report }: { report: Report }) {
  const [kind, setKind] = useState<EntityKind>('group')
  const [name, setName] = useState(report.groups[0]?.name ?? '')
  const [week, setWeek] = useState<Week>('odd')
  const [selected, setSelected] = useState<number | null>(null)

  const byId = useMemo(() => new Map(report.lessons.map((l) => [l.id, l])), [report])
  const conflicted = useMemo(() => {
    const m = new Map<number, string[]>()
    for (const v of report.violations)
      for (const id of v.lesson_ids) m.set(id, [...(m.get(id) ?? []), v.message])
    return m
  }, [report])
  const names = useMemo(() => {
    if (kind === 'group') return report.groups.map((g) => g.name)
    if (kind === 'teacher') return report.teachers.map((t) => t.name)
    return report.rooms.map((r) => r.key)
  }, [report, kind])

  const show = (k: EntityKind, n: string, w?: Week, lessonId?: number) => {
    setKind(k)
    setName(n)
    if (w) setWeek(w)
    setSelected(lessonId ?? null)
  }

  const s = report.stats
  return (
    <>
      <div className="stats">
        <Stat label="Занятий" value={s.lessons} />
        <Stat label="Групп" value={s.groups} />
        <Stat label="Преподавателей" value={s.teachers} />
        <Stat label="Аудиторий" value={s.rooms} />
        <Stat label="Накладок" value={s.hard} bad={s.hard > 0} />
        <Stat label="Штраф мягких критериев" value={Math.round(report.soft_total)} />
      </div>

      <h2>Сетка</h2>
      <div className="controls">
        <select
          value={kind}
          onChange={(e) => {
            const k = e.target.value as EntityKind
            const first =
              k === 'group'
                ? report.groups[0]?.name
                : k === 'teacher'
                  ? report.teachers[0]?.name
                  : report.rooms[0]?.key
            show(k, first ?? '')
          }}
        >
          <option value="group">Группа</option>
          <option value="teacher">Преподаватель</option>
          <option value="room">Аудитория</option>
        </select>
        <select value={name} onChange={(e) => show(kind, e.target.value)}>
          {names.map((n) => (
            <option key={n} value={n}>
              {n}
            </option>
          ))}
        </select>
        <div className="toggle">
          <button className={week === 'odd' ? 'on' : ''} onClick={() => setWeek('odd')}>
            Числитель (над чертой)
          </button>
          <button className={week === 'even' ? 'on' : ''} onClick={() => setWeek('even')}>
            Знаменатель (под чертой)
          </button>
        </div>
      </div>
      <Grid
        report={report}
        kind={kind}
        name={name}
        week={week}
        conflicted={conflicted}
        selected={selected}
      />

      <h2>Накладки ({report.violations.length})</h2>
      {report.violations.length === 0 ? (
        <p className="muted">Накладок не найдено.</p>
      ) : (
        <ul className="violations">
          {report.violations.map((v, i) => {
            const first = byId.get(v.lesson_ids[0] ?? 0)
            return (
              <li key={i}>
                <span className="rule">{v.rule}</span> {v.message}
                <div className="muted small">
                  {v.lesson_ids.map((id) => {
                    const l = byId.get(id)
                    if (!l) return null
                    return (
                      <div key={id}>
                        {l.discipline} ({TYPE_NAMES[l.lesson_type] ?? l.lesson_type}),{' '}
                        {l.audience.map((m) => m.group).join(', ')} — {refText(l.refs[0])}
                      </div>
                    )
                  })}
                </div>
                {first && v.resource && (
                  <button
                    className="link"
                    onClick={() =>
                      show(
                        v.resource!.kind,
                        v.resource!.name,
                        first.parity === 'even' ? 'even' : 'odd',
                        first.id,
                      )
                    }
                  >
                    показать в сетке
                  </button>
                )}
              </li>
            )
          })}
        </ul>
      )}

      <h2>Мягкие критерии</h2>
      <table className="table">
        <thead>
          <tr>
            <th>Критерий</th>
            <th>Значение</th>
            <th>Вес</th>
            <th>Штраф</th>
          </tr>
        </thead>
        <tbody>
          {report.soft.map((c) => (
            <tr key={c.key} className={c.available ? '' : 'muted'}>
              <td>{c.title}</td>
              <td>{c.available ? c.value : 'нет данных'}</td>
              <td>{c.weight}</td>
              <td>{c.available ? c.weighted : '—'}</td>
            </tr>
          ))}
        </tbody>
      </table>

      <div className="two-col">
        <Scores title="Группы" rows={report.groups} onPick={(n) => show('group', n)} />
        <Scores title="Преподаватели" rows={report.teachers} onPick={(n) => show('teacher', n)} />
      </div>

      {report.issues.length > 0 && (
        <>
          <h2>Качество данных ({report.issues.length})</h2>
          <ul>
            {report.issues.map((is, i) => (
              <li key={i}>
                <code>{is.code}</code> {is.message}
                {is.ref && <span className="muted"> — {refText(is.ref)}</span>}
              </li>
            ))}
          </ul>
        </>
      )}

      <h2>Ограничения проверки</h2>
      <ul className="muted">
        {report.notes.map((n, i) => (
          <li key={i}>{n}</li>
        ))}
      </ul>
    </>
  )
}

function Stat({ label, value, bad }: { label: string; value: number; bad?: boolean }) {
  return (
    <div className={bad ? 'stat bad' : 'stat'}>
      <div className="stat-value">{value}</div>
      <div className="stat-label">{label}</div>
    </div>
  )
}

function Grid(props: {
  report: Report
  kind: EntityKind
  name: string
  week: Week
  conflicted: Map<number, string[]>
  selected: number | null
}) {
  const { report, kind, name, week, conflicted, selected } = props
  const lessons = report.lessons.filter(
    (l) => involves(l, kind, name) && (l.parity === 'every' || l.parity === week),
  )
  const days = DAYS.filter((d) => d !== 'sat' || lessons.some((l) => l.day === 'sat'))
  const cell = (day: string, period: number) =>
    lessons.filter((l) => l.day === day && l.period === period)

  return (
    <div className="grid-wrap">
      <table className="grid">
        <thead>
          <tr>
            <th />
            {days.map((d) => (
              <th key={d}>{DAY_NAMES[d]}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {report.periods.map((p) => (
            <tr key={p.n}>
              <th>
                {p.n}
                <div className="small muted">
                  {p.start}–{p.end}
                </div>
              </th>
              {days.map((d) => (
                <td key={d}>
                  {cell(d, p.n).map((l) => (
                    <LessonCard
                      key={l.id}
                      lesson={l}
                      kind={kind}
                      problems={conflicted.get(l.id)}
                      selected={selected === l.id}
                    />
                  ))}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function LessonCard(props: {
  lesson: Lesson
  kind: EntityKind
  problems?: string[]
  selected: boolean
}) {
  const { lesson: l, kind, problems, selected } = props
  const cls = ['lesson', `t-${l.lesson_type}`]
  if (problems) cls.push('conflict')
  if (selected) cls.push('selected')
  const subgroups = l.audience.filter((m) => m.subgroup !== null).map((m) => m.subgroup)
  const where = l.room ?? PLACE_NAMES[l.place ?? ''] ?? l.place ?? ''
  const tooltip = [refText(l.refs[0]), ...(problems ?? []), l.note ?? ''].filter(Boolean).join('\n')
  return (
    <div className={cls.join(' ')} title={tooltip}>
      <div className="lesson-head">
        <span className="badge">{TYPE_NAMES[l.lesson_type] ?? l.lesson_type}</span>
        {subgroups.length > 0 && <span className="badge">п/г {subgroups.join(',')}</span>}
        {l.parity === 'every' && <span className="badge ghost">каждую неделю</span>}
      </div>
      <div className="lesson-title">{l.discipline}</div>
      {kind !== 'teacher' && <div className="small">{l.teachers.join(', ') || '—'}</div>}
      {kind !== 'group' && <div className="small">{l.audience.map((m) => m.group).join(', ')}</div>}
      {kind !== 'room' && <div className="small muted">{where}</div>}
    </div>
  )
}

function Scores(props: { title: string; rows: EntityScore[]; onPick: (name: string) => void }) {
  const [sort, setSort] = useState<keyof EntityScore>('transitions')
  const rows = [...props.rows].sort((a, b) =>
    sort === 'name' ? a.name.localeCompare(b.name) : (b[sort] as number) - (a[sort] as number),
  )
  const head = (key: keyof EntityScore, label: string) => (
    <th className="sortable" onClick={() => setSort(key)}>
      {label}
      {sort === key ? ' ▾' : ''}
    </th>
  )
  return (
    <div>
      <h2>{props.title}</h2>
      <div className="scroll">
        <table className="table">
          <thead>
            <tr>
              {head('name', 'Имя')}
              {head('lessons', 'Пар за 2 нед.')}
              {head('gaps', 'Окна')}
              {head('transitions', 'Переходы')}
              {head('max_per_day', 'Макс. в день')}
              {head('conflicts', 'Накладки')}
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.name}>
                <td>
                  <button className="link" onClick={() => props.onPick(r.name)}>
                    {r.name}
                  </button>
                </td>
                <td>{r.lessons}</td>
                <td>{r.gaps}</td>
                <td>{r.transitions}</td>
                <td>{r.max_per_day}</td>
                <td className={r.conflicts > 0 ? 'error' : ''}>{r.conflicts}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
