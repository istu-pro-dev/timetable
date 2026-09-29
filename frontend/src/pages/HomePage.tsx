import { useQuery } from '@tanstack/react-query'
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
    </section>
  )
}
