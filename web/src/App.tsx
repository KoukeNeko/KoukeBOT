import { BrowserRouter, Routes, Route, NavLink } from 'react-router-dom'
import PersonaList from './pages/PersonaList'
import PersonaDetail from './pages/PersonaDetail'
import Chat from './pages/Chat'
import './App.css'

export default function App() {
  return (
    <BrowserRouter>
      <div className="app">
        <nav className="navbar">
          <span className="brand">KoukeBOT</span>
          <NavLink to="/">Personas</NavLink>
          <NavLink to="/chat">Chat</NavLink>
        </nav>

        <main className="content">
          <Routes>
            <Route path="/" element={<PersonaList />} />
            <Route path="/personas/:id" element={<PersonaDetail />} />
            <Route path="/chat" element={<Chat />} />
          </Routes>
        </main>
      </div>
    </BrowserRouter>
  )
}
