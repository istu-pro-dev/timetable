import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router'
import { fetchHealth } from '../api/health.ts'

export function HomePage() {
  const health = useQuery({
    queryKey: ['health'],
    queryFn: ({ signal }) => fetchHealth(signal),
  })

  let status: string
  if (health.isPending) status = 'проверка…'
  else if (health.isError) status = 'недоступен'
  else status = health.data.status === 'ok' ? 'работает' : health.data.status

  return (
    <section>
      <h1>Система составления расписания</h1>
      <p>
        API: <span data-testid="api-status">{status}</span>
      </p>
      <p>
        <Link to="/review">Проверить расписание</Link> — загрузить файл из «Тандема» и посмотреть
        накладки, окна и переходы между корпусами.
      </p>
    </section>
  )
}
