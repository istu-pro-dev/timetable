import { createBrowserRouter } from 'react-router'
import { App } from './App.tsx'
import { HomePage } from './pages/HomePage.tsx'

export const router = createBrowserRouter([
  {
    path: '/',
    element: <App />,
    children: [{ index: true, element: <HomePage /> }],
  },
])
