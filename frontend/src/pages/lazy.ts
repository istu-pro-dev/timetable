// Route-level code splitting: pages loaded on demand (each becomes a separate chunk).
import { lazy } from 'react'

export const LoginPage = lazy(() =>
  import('./LoginPage.tsx').then((m) => ({ default: m.LoginPage })),
)
