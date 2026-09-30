import { t, type MessageKey } from '../i18n/index.ts'
import { ru } from '../i18n/ru.ts'
import { ApiError } from './client.ts'

/** A localized, user-facing description of a failed request. */
export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    const title = err.code in ru.errors ? t(`errors.${err.code}` as MessageKey) : undefined
    if (title && err.message && err.message !== title) return `${title}: ${err.message}`
    return title ?? err.message
  }
  if (err instanceof TypeError) return t('common.networkError')
  if (err instanceof Error && err.message) return err.message
  return t('common.genericError')
}
