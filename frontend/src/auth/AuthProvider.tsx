import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, type ReactNode } from 'react'
import { authApi, type Credentials } from '../api/auth.ts'
import { setUnauthorizedHandler } from '../api/client.ts'
import type { User } from '../api/types.ts'
import { AuthContext, meQueryKey, type AuthState } from './context.ts'

/**
 * Loads the current user (GET /api/auth/me, refreshing the session once on 401) and exposes
 * login/logout. When any request stays unauthorized, the user becomes null and the protected
 * routes redirect to /login.
 */
export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()

  const me = useQuery({
    queryKey: meQueryKey,
    queryFn: ({ signal }) => authApi.me(signal),
    staleTime: Infinity,
    retry: false,
  })

  const clearSession = useCallback(() => {
    queryClient.removeQueries({ predicate: (q) => q.queryKey[0] !== meQueryKey[0] })
    queryClient.setQueryData<User | null>(meQueryKey, null)
  }, [queryClient])

  useEffect(() => {
    setUnauthorizedHandler(clearSession)
    return () => setUnauthorizedHandler(undefined)
  }, [clearSession])

  const login = useCallback(
    async (credentials: Credentials) => {
      const session = await authApi.login(credentials)
      queryClient.setQueryData<User | null>(meQueryKey, session.user)
      return session.user
    },
    [queryClient],
  )

  const logout = useCallback(async () => {
    try {
      await authApi.logout()
    } finally {
      clearSession()
    }
  }, [clearSession])

  // A failed /me (server down) is treated as logged out; the login page shows the error.
  const user = me.isPending ? undefined : (me.data ?? null)
  const value = useMemo<AuthState>(() => ({ user, login, logout }), [user, login, logout])

  return <AuthContext value={value}>{children}</AuthContext>
}
