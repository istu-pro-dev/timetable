import type { ReactNode } from 'react'
import { Link } from 'react-router'
import { t } from '../../i18n/index.ts'

/** A centered message page (not found, forbidden, coming soon). */
export function PageMessage({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <section className="mx-auto flex max-w-md flex-col items-center gap-3 py-16 text-center">
      <h1 className="text-xl font-semibold">{title}</h1>
      {children}
      <Link to="/" className="text-sm text-primary underline-offset-4 hover:underline">
        {t('common.toHome')}
      </Link>
    </section>
  )
}
