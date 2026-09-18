import React, { useEffect, useState } from 'react'
import Dashboard from './pages/Dashboard'
import Runtimes from './pages/Runtimes'
import Processes from './pages/Processes'
import Projects from './pages/Projects'
import Files from './pages/Files'
import Logs from './pages/Logs'
import Docker from './pages/Docker'
import Domains from './pages/Domains'
import Extensions from './pages/Extensions'
import Git from './pages/Git'
import Servers from './pages/Servers'
import Deployments from './pages/Deployments'
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
  const [tab, setTab] = useState<'dash'|'runtimes'|'proc'|'projects'|'files'|'logs'|'docker'|'domains'|'extensions'|'git'|'servers'|'deployments'|'settings'>('dash')
  const { token, setToken } = useToken()

  return (
    <div className="min-h-screen">
      <nav className="flex gap-3 p-3 border-b">
        <button onClick={() => setTab('dash')}>Dashboard</button>
        <button onClick={() => setTab('runtimes')}>Runtimes</button>
        <button onClick={() => setTab('proc')}>Processes</button>
        <button onClick={() => setTab('projects')}>Projects</button>
        <button onClick={() => setTab('files')}>Files</button>
        <button onClick={() => setTab('logs')}>Logs</button>
        <button onClick={() => setTab('docker')}>Docker</button>
        <button onClick={() => setTab('domains')}>Domains</button>
        <button onClick={() => setTab('extensions')}>Extensions</button>
        <button onClick={() => setTab('git')}>Git</button>
        <button onClick={() => setTab('servers')}>Servers</button>
        <button onClick={() => setTab('deployments')}>Deployments</button>
        <button onClick={() => setTab('settings')}>Settings</button>
        <div className="ml-auto flex items-center gap-2">
          <input placeholder="Token" value={token} onChange={e=>setToken(e.target.value)} className="border px-2 py-1 text-sm" />
        </div>
      </nav>
      <main className="p-4">
        {tab==='dash' && <Dashboard />}
        {tab==='runtimes' && <Runtimes />}
        {tab==='proc' && <Processes />}
        {tab==='projects' && <Projects />}
        {tab==='files' && <Files />}
        {tab==='logs' && <Logs />}
        {tab==='docker' && <Docker />}
        {tab==='domains' && <Domains />}
        {tab==='extensions' && <Extensions />}
        {tab==='git' && <Git />}
        {tab==='servers' && <Servers />}
        {tab==='deployments' && <Deployments />}
        {tab==='settings' && <Settings />}
      </main>
    </div>
  )
}
