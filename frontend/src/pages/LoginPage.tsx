import { useState, type FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router'
import { isApiError } from '../api/client.ts'
import { errorMessage } from '../api/errorMessage.ts'
import { useAuth } from '../auth/context.ts'
import { Alert } from '../components/ui/Alert.tsx'
import { Button } from '../components/ui/Button.tsx'
import { Field, Input } from '../components/ui/Field.tsx'
import { FullPageSpinner } from '../components/ui/Spinner.tsx'
import { t } from '../i18n/index.ts'

function loginError(err: unknown): string {
  if (isApiError(err, 401)) return t('auth.invalidCredentials')
  if (isApiError(err, 403)) return t('auth.accountDisabled')
  if (isApiError(err, 429)) {
    return err.retryAfter
      ? t('auth.tooManyAttempts', { seconds: err.retryAfter })
      : t('auth.tooManyAttemptsNoDelay')
  }
  return errorMessage(err)
}

/** Where to go after login: the page that redirected here, if it is a local path. */
function redirectTarget(state: unknown): string {
  const from = (state as { from?: unknown } | null)?.from
  return typeof from === 'string' && from.startsWith('/') && !from.startsWith('//') ? from : '/'
}

export function LoginPage() {
  const { user, login } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [form, setForm] = useState({ login: '', password: '' })
  const [error, setError] = useState<string>()
  const [submitting, setSubmitting] = useState(false)

  if (user === undefined) return <FullPageSpinner />
  if (user && !submitting) return <Navigate to={redirectTarget(location.state)} replace />

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!form.login.trim() || !form.password) {
      setError(t('auth.required'))
      return
    }
    setError(undefined)
    setSubmitting(true)
    try {
      await login({ login: form.login.trim(), password: form.password })
      await navigate(redirectTarget(location.state), { replace: true })
    } catch (err) {
      setError(loginError(err))
      setSubmitting(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <form
        onSubmit={(e) => void onSubmit(e)}
        noValidate
        aria-labelledby="login-title"
        className="flex w-full max-w-sm flex-col gap-4 rounded-xl border border-border bg-surface p-6 shadow-sm"
      >
        <div>
          <h1 id="login-title" className="text-xl font-semibold">
            {t('auth.loginTitle')}
          </h1>
          <p className="text-sm text-muted">{t('app.subtitle')}</p>
        </div>
        {error && <Alert>{error}</Alert>}
        <Field label={t('auth.login')}>
          <Input
            name="login"
            autoComplete="username"
            autoFocus
            value={form.login}
            onChange={(e) => setForm({ ...form, login: e.target.value })}
          />
        </Field>
        <Field label={t('auth.password')}>
          <Input
            name="password"
            type="password"
            autoComplete="current-password"
            value={form.password}
            onChange={(e) => setForm({ ...form, password: e.target.value })}
          />
        </Field>
        <Button type="submit" variant="primary" disabled={submitting}>
          {submitting ? t('auth.submitting') : t('auth.submit')}
        </Button>
      </form>
    </div>
  )
}
