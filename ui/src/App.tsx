import React, { useEffect, useMemo, useState } from 'react'
import Dashboard from './pages/Dashboard'
import Processes from './pages/Processes'
import Files from './pages/Files'
import Logs from './pages/Logs'
import Settings from './pages/Settings'

function useToken() {
  const [token, setToken] = useState<string>('')
  useEffect(() => {
    // In dev, allow empty; otherwise ask user to paste token or store in localStorage
    const t = localStorage.getItem('voltToken') || ''
    setToken(t)
  }, [])
  return { token, setToken }
}

export default function App() {
  const [tab, setTab] = useState<'dash'|'proc'|'files'|'logs'|'settings'>('dash')
  const { token, setToken } = useToken()
  const headers = useMemo(() => token ? { 'X-Volt-Token': token } : {}, [token])

  return (
    <div className="min-h-screen">
      <nav className="flex gap-3 p-3 border-b">
        <button onClick={() => setTab('dash')}>Dashboard</button>
        <button onClick={() => setTab('proc')}>Processes</button>
        <button onClick={() => setTab('files')}>Files</button>
        <button onClick={() => setTab('logs')}>Logs</button>
        <button onClick={() => setTab('settings')}>Settings</button>
        <div className="ml-auto flex items-center gap-2">
          <input placeholder="Token" value={token} onChange={e=>{setToken(e.target.value); localStorage.setItem('voltToken', e.target.value)}} className="border px-2 py-1 text-sm" />
        </div>
      </nav>
      <main className="p-4">
        {tab==='dash' && <Dashboard headers={headers} />}
        {tab==='proc' && <Processes headers={headers} />}
        {tab==='files' && <Files headers={headers} />}
        {tab==='logs' && <Logs headers={headers} />}
        {tab==='settings' && <Settings headers={headers} />}
      </main>
    </div>
  )
}
