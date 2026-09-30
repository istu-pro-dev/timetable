import { Suspense, useState } from 'react'
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router'
import { hasRole, useAuth } from '../auth/context.ts'
import { Button } from '../components/ui/Button.tsx'
import { cx } from '../components/ui/cx.ts'
import { FullPageSpinner } from '../components/ui/Spinner.tsx'
import { t } from '../i18n/index.ts'
import { useTheme, type ThemePreference } from '../theme/theme.ts'
import { navSections } from './nav.ts'

function Sidebar({ onNavigate }: { onNavigate: () => void }) {
  const { user } = useAuth()
  return (
    <nav aria-label={t('nav.main')} className="flex flex-col gap-5 p-3">
      {navSections
        .filter((section) => !section.roles || hasRole(user, section.roles))
        .map((section, i) => (
          <div key={section.title ?? i} className="flex flex-col gap-0.5">
            {section.title && (
              <h2 className="px-2 pb-1 text-xs font-semibold tracking-wide text-muted uppercase">
                {t(section.title)}
              </h2>
            )}
            <ul className="flex flex-col gap-0.5">
              {section.items.map((item) => (
                <li key={item.to}>
                  <NavLink
                    to={item.to}
                    end={item.to === '/'}
                    onClick={onNavigate}
                    className={({ isActive }) =>
                      cx(
                        'block rounded-md px-2 py-1.5 text-sm transition-colors',
                        isActive
                          ? 'bg-primary/10 font-medium text-primary'
                          : 'text-fg hover:bg-surface-2',
                      )
                    }
                  >
                    {t(item.label)}
                  </NavLink>
                </li>
              ))}
            </ul>
          </div>
        ))}
    </nav>
  )
}

function ThemeSelect() {
  const [theme, setTheme] = useTheme()
  return (
    <select
      aria-label={t('theme.label')}
      value={theme}
      onChange={(e) => setTheme(e.target.value as ThemePreference)}
      className="h-8 rounded-md border border-border bg-surface px-2 text-sm"
    >
      <option value="system">{t('theme.system')}</option>
      <option value="light">{t('theme.light')}</option>
      <option value="dark">{t('theme.dark')}</option>
    </select>
  )
}

/** The authenticated shell: header, role-aware sidebar and the routed page. */
export function AppLayout() {
  const { user, logout } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  // The mobile menu stays open only on the page where it was opened.
  const [menuOpenAt, setMenuOpenAt] = useState<string | null>(null)
  const menuOpen = menuOpenAt === location.pathname
  const setMenuOpen = (open: boolean) => setMenuOpenAt(open ? location.pathname : null)

  const onLogout = async () => {
    await logout()
    await navigate('/login', { replace: true })
  }

  return (
    <div className="flex min-h-screen flex-col">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-50 focus:rounded focus:bg-surface focus:px-3 focus:py-2"
      >
        {t('app.skipToContent')}
      </a>
      <header className="sticky top-0 z-30 flex h-14 items-center gap-3 border-b border-border bg-surface px-4">
        <Button
          variant="ghost"
          size="sm"
          className="md:hidden"
          aria-expanded={menuOpen}
          aria-controls="sidebar"
          aria-label={menuOpen ? t('nav.closeMenu') : t('nav.openMenu')}
          onClick={() => setMenuOpen(!menuOpen)}
        >
          <span aria-hidden>☰</span>
        </Button>
        <span className="font-semibold">{t('app.title')}</span>
        <div className="ml-auto flex items-center gap-3">
          <ThemeSelect />
          {user && (
            <span className="hidden text-sm sm:inline">
              <span className="font-medium">{user.display_name || user.login}</span>
              <span className="text-muted"> · {t(`roles.${user.role}`)}</span>
            </span>
          )}
          <Button size="sm" onClick={() => void onLogout()}>
            {t('auth.logout')}
          </Button>
        </div>
      </header>
      <div className="flex flex-1">
        <aside
          id="sidebar"
          className={cx(
            'w-60 shrink-0 border-r border-border bg-surface',
            menuOpen ? 'fixed inset-y-14 left-0 z-20 block overflow-y-auto' : 'hidden',
            'md:static md:block',
          )}
        >
          <Sidebar onNavigate={() => setMenuOpen(false)} />
        </aside>
        <main id="main" className="min-w-0 flex-1 p-4 md:p-6">
          <Suspense fallback={<FullPageSpinner />}>
            <Outlet />
          </Suspense>
        </main>
      </div>
    </div>
  )
}
