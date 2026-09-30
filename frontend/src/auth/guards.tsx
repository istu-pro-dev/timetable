import type { ReactNode } from 'react'
import { Navigate, Outlet, useLocation } from 'react-router'
import type { Role } from '../api/types.ts'
import { PageMessage } from '../components/ui/PageMessage.tsx'
import { FullPageSpinner } from '../components/ui/Spinner.tsx'
import { t } from '../i18n/index.ts'
import { hasRole, useAuth } from './context.ts'

/** Renders the child routes for a logged-in user, otherwise redirects to /login. */
export function RequireAuth({ children }: { children?: ReactNode }) {
  const { user } = useAuth()
  const location = useLocation()
  if (user === undefined) return <FullPageSpinner />
  if (user === null) {
    return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />
  }
  return children ?? <Outlet />
}

/** Renders the child routes only for the given roles (inside RequireAuth). */
export function RequireRole({ roles, children }: { roles: readonly Role[]; children?: ReactNode }) {
  const { user } = useAuth()
  if (!hasRole(user, roles)) return <PageMessage title={t('common.forbidden')} />
  return children ?? <Outlet />
}
