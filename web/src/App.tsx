import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import Personas from './pages/Personas'
import PersonaDetail from './pages/PersonaDetail'
import Chat from './pages/Chat'
import Settings from './pages/Settings'
import Schedules from './pages/Schedules'
import HowItWorks from './pages/HowItWorks'
import Layout from './components/Layout'
import './App.css'

export default function App() {
  return (
    <BrowserRouter>
      <Layout>
        <Routes>
          <Route path="/" element={<Navigate to="/personas" replace />} />
          <Route path="/personas" element={<Personas />}>
            <Route index element={<div className="empty-state">Select a persona to edit</div>} />
            <Route path=":id" element={<PersonaDetail />} />
          </Route>
          <Route path="/chat" element={<Chat />}>
            <Route path=":id" element={<Chat />} />
          </Route>
          <Route path="/schedules" element={<Schedules />} />
          <Route path="/settings" element={<Settings />} />
          <Route path="/how-it-works" element={<HowItWorks />} />
        </Routes>
      </Layout>
    </BrowserRouter>
  )
}
