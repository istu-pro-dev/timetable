// Minimal typed i18n: nested dictionaries, dotted keys, `{param}` interpolation.
// RU is the default locale; a new locale is one more `Messages` object in `dictionaries`.
import { ru, type Messages } from './ru.ts'

export type Locale = 'ru'

const dictionaries: Record<Locale, Messages> = { ru }

let locale: Locale = 'ru'

type Leaves<T, P extends string = ''> = {
  [K in keyof T & string]: T[K] extends string ? `${P}${K}` : Leaves<T[K], `${P}${K}.`>
}[keyof T & string]

/** Every translation key, e.g. `'nav.home'`. */
export type MessageKey = Leaves<Messages>

export function setLocale(next: Locale): void {
  locale = next
  document.documentElement.lang = next
}

export function getLocale(): Locale {
  return locale
}

function lookup(key: string): string | undefined {
  let node: unknown = dictionaries[locale]
  for (const part of key.split('.')) {
    if (typeof node !== 'object' || node === null) return undefined
    node = (node as Record<string, unknown>)[part]
  }
  return typeof node === 'string' ? node : undefined
}

/** Translates a key; unknown keys (impossible when typed) fall back to the key itself. */
export function t(key: MessageKey, params?: Record<string, string | number>): string {
  const text = lookup(key) ?? key
  if (!params) return text
  return text.replace(/\{(\w+)\}/g, (match, name: string) =>
    name in params ? String(params[name]) : match,
  )
}
