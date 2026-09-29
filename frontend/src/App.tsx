import { Outlet } from 'react-router'

export function App() {
  return (
    <div className="app">
      <header className="app-header">Расписание</header>
      <main className="app-main">
        <Outlet />
      </main>
    </div>
  )
}
