import { t } from '../../i18n/index.ts'
import { cx } from './cx.ts'

export function Spinner({ className }: { className?: string }) {
  return (
    <span
      role="status"
      aria-label={t('app.loading')}
      className={cx(
        'inline-block size-5 animate-spin rounded-full border-2 border-current border-r-transparent text-muted',
        className,
      )}
    />
  )
}

export function FullPageSpinner() {
  return (
    <div className="flex min-h-[50vh] items-center justify-center">
      <Spinner className="size-8" />
    </div>
  )
}
