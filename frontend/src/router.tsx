import { createBrowserRouter, type RouteObject } from 'react-router'
import { RequireAuth, RequireRole } from './auth/guards.tsx'
import { PageMessage } from './components/ui/PageMessage.tsx'
import { t } from './i18n/index.ts'
import { AppLayout } from './layout/AppLayout.tsx'
import { HomePage } from './pages/HomePage.tsx'
import { LoginPage } from './pages/lazy.ts'

const comingSoon = <PageMessage title={t('common.comingSoon')} />

const managementPaths = [
  'buildings',
  'room-types',
  'rooms',
  'groups',
  'teachers',
  'disciplines',
  'time-grid',
  'curriculum',
]

export const routes: RouteObject[] = [
  { path: '/login', element: <LoginPage /> },
  {
    element: <RequireAuth />,
    children: [
      {
        element: <AppLayout />,
        children: [
          { index: true, element: <HomePage /> },
          {
            element: <RequireRole roles={['admin']} />,
            children: managementPaths.map((path) => ({ path, element: comingSoon })),
          },
          { path: '*', element: <PageMessage title={t('common.notFound')} /> },
        ],
      },
    ],
  },
]

export function createAppRouter() {
  return createBrowserRouter(routes)
}
