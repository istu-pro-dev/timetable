import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode, Suspense } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider } from 'react-router'
import { isApiError } from './api/client.ts'
import { AuthProvider } from './auth/AuthProvider.tsx'
import { FullPageSpinner } from './components/ui/Spinner.tsx'
import { setLocale } from './i18n/index.ts'
import './index.css'
import { createAppRouter } from './router.tsx'
import { applyTheme, readThemePreference } from './theme/theme.ts'

applyTheme(readThemePreference())
setLocale('ru')

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      // Client errors (401/403/404/422…) will not fix themselves on retry.
      retry: (count, err) => count < 2 && !(isApiError(err) && err.status < 500),
    },
  },
})

const router = createAppRouter()

const root = document.getElementById('root')
if (!root) throw new Error('#root element not found')

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <Suspense fallback={<FullPageSpinner />}>
          <RouterProvider router={router} />
        </Suspense>
      </AuthProvider>
    </QueryClientProvider>
  </StrictMode>,
)
