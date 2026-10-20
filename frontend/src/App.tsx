import { NavLink, Outlet } from 'react-router'

export function App() {
  return (
    <div className="app">
      <header className="app-header">
        <span className="app-title">Расписание</span>
        <nav>
          <NavLink to="/" end>
            Главная
          </NavLink>
          <NavLink to="/review">Проверка расписания</NavLink>
        </nav>
      </header>
      <main className="app-main">
        <Outlet />
      </main>
    </div>
  )
}
