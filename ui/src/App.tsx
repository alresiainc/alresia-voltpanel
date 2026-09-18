import React, { useEffect, useState } from 'react'
import Dashboard from './pages/Dashboard'
import Processes from './pages/Processes'
import Files from './pages/Files'
import Logs from './pages/Logs'
import Settings from './pages/Settings'
import { setToken as setApiToken, api } from './lib/api'
import { wsClient } from './lib/ws'

function useToken() {
  const [token, setToken] = useState<string>('')
  useEffect(() => {
    const t = localStorage.getItem('voltToken') || ''
    setToken(t)
  }, [])
  const update = (t: string) => {
    setToken(t)
    localStorage.setItem('voltToken', t)
    setApiToken(t)
    api.verifyToken(t).catch(() => {}) // establishes the session cookie; failures surface per-request
    wsClient.connect(t)
  }
  useEffect(() => { if (token) update(token) }, []) // eslint-disable-line react-hooks/exhaustive-deps
  return { token, setToken: update }
}

export default function App() {
  const [tab, setTab] = useState<'dash'|'proc'|'files'|'logs'|'settings'>('dash')
  const { token, setToken } = useToken()

  return (
    <div className="min-h-screen">
      <nav className="flex gap-3 p-3 border-b">
        <button onClick={() => setTab('dash')}>Dashboard</button>
        <button onClick={() => setTab('proc')}>Processes</button>
        <button onClick={() => setTab('files')}>Files</button>
        <button onClick={() => setTab('logs')}>Logs</button>
        <button onClick={() => setTab('settings')}>Settings</button>
        <div className="ml-auto flex items-center gap-2">
          <input placeholder="Token" value={token} onChange={e=>setToken(e.target.value)} className="border px-2 py-1 text-sm" />
        </div>
      </nav>
      <main className="p-4">
        {tab==='dash' && <Dashboard />}
        {tab==='proc' && <Processes />}
        {tab==='files' && <Files />}
        {tab==='logs' && <Logs />}
        {tab==='settings' && <Settings />}
      </main>
    </div>
  )
}
