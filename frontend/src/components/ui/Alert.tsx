import type { ReactNode } from 'react'
import { cx } from './cx.ts'

export function Alert({
  tone = 'danger',
  children,
  className,
}: {
  tone?: 'danger' | 'info'
  children: ReactNode
  className?: string
}) {
  return (
    <div
      role={tone === 'danger' ? 'alert' : 'status'}
      className={cx(
        'rounded-md border px-3 py-2 text-sm',
        tone === 'danger'
          ? 'border-danger/40 bg-danger/10 text-danger'
          : 'border-border bg-surface-2 text-fg',
        className,
      )}
    >
      {children}
    </div>
  )
}
