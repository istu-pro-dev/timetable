import { useQuery } from '@tanstack/react-query'
import { fetchHealth } from '../api/health.ts'
import { useAuth } from '../auth/context.ts'
import { t } from '../i18n/index.ts'

export function HomePage() {
  const { user } = useAuth()
  const health = useQuery({
    queryKey: ['health'],
    queryFn: ({ signal }) => fetchHealth(signal),
  })

  let status: string
  if (health.isPending) status = t('home.apiChecking')
  else if (health.isError) status = t('home.apiDown')
  else status = health.data.status === 'ok' ? t('home.apiOk') : health.data.status

  return (
    <section className="flex flex-col gap-2">
      <h1 className="text-2xl font-semibold">{t('app.subtitle')}</h1>
      {user && <p>{t('home.welcome', { name: user.display_name || user.login })}</p>}
      <p className="text-sm text-muted">
        {t('home.apiStatus')}: <span data-testid="api-status">{status}</span>
      </p>
    </section>
  )
}
