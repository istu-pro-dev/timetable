import { useCallback, useEffect, useState } from 'react'

export type ThemePreference = 'light' | 'dark' | 'system'

const STORAGE_KEY = 'tt-theme'

export function readThemePreference(): ThemePreference {
  try {
    const value = localStorage.getItem(STORAGE_KEY)
    if (value === 'light' || value === 'dark') return value
  } catch {
    // Storage unavailable (private mode) — follow the system.
  }
  return 'system'
}

function systemPrefersDark(): boolean {
  return typeof matchMedia === 'function' && matchMedia('(prefers-color-scheme: dark)').matches
}

/** Sets the `dark` class on <html> (Tailwind's dark variant keys off it). */
export function applyTheme(pref: ThemePreference): void {
  const dark = pref === 'dark' || (pref === 'system' && systemPrefersDark())
  document.documentElement.classList.toggle('dark', dark)
  document.documentElement.style.colorScheme = dark ? 'dark' : 'light'
}

export function useTheme(): [ThemePreference, (pref: ThemePreference) => void] {
  const [pref, setPref] = useState<ThemePreference>(readThemePreference)

  useEffect(() => {
    applyTheme(pref)
    if (pref !== 'system' || typeof matchMedia !== 'function') return
    const mq = matchMedia('(prefers-color-scheme: dark)')
    const onChange = () => applyTheme('system')
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [pref])

  const update = useCallback((next: ThemePreference) => {
    try {
      if (next === 'system') localStorage.removeItem(STORAGE_KEY)
      else localStorage.setItem(STORAGE_KEY, next)
    } catch {
      // ignore
    }
    setPref(next)
  }, [])

  return [pref, update]
}
