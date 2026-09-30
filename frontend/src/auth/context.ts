import { createContext, useContext } from 'react'
import type { Credentials } from '../api/auth.ts'
import type { Role, User } from '../api/types.ts'

export interface AuthState {
  /** undefined while the session is being checked, null when logged out. */
  user: User | null | undefined
  login: (credentials: Credentials) => Promise<User>
  logout: () => Promise<void>
}

export const AuthContext = createContext<AuthState | null>(null)

export const meQueryKey = ['auth', 'me'] as const

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used inside <AuthProvider>')
  return ctx
}

export function hasRole(user: User | null | undefined, roles?: readonly Role[]): boolean {
  if (!user) return false
  return !roles || roles.includes(user.role)
}
