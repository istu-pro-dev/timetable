import { api, isApiError } from './client.ts'
import type { Session, User } from './types.ts'

export interface Credentials {
  login: string
  password: string
}

export const authApi = {
  login: (credentials: Credentials) =>
    api.post<Session>('/api/auth/login', credentials, { noRefresh: true }),

  logout: () => api.post<void>('/api/auth/logout', undefined, { noRefresh: true }),

  /** The current user, or null without a valid session (after one refresh attempt). */
  me: async (signal?: AbortSignal): Promise<User | null> => {
    try {
      return await api.get<User>('/api/auth/me', { signal })
    } catch (err) {
      if (isApiError(err, 401)) return null
      throw err
    }
  },
}
