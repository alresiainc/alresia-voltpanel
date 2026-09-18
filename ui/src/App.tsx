import React, { useEffect, useState } from 'react'
import Dashboard from './pages/Dashboard'
import Software from './pages/Software'
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
import Pipelines from './pages/Pipelines'
import Settings from './pages/Settings'
import { setToken as setApiToken, api } from './lib/api'
import { wsClient } from './lib/ws'
import Sidebar, { navLabel } from './components/Sidebar'
import Topbar from './components/Topbar'

export type Tab =
  | 'dash' | 'software' | 'runtimes' | 'proc' | 'projects' | 'files' | 'logs' | 'docker'
  | 'domains' | 'extensions' | 'git' | 'servers' | 'deployments' | 'pipelines' | 'settings'

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

// Polls the public, unauthenticated /api/v1/health endpoint so the topbar
// can show a real connection indicator instead of a decorative one.
function useDaemonHealth() {
  const [connected, setConnected] = useState<boolean | null>(null)
  useEffect(() => {
    let cancelled = false
    const check = () => {
      fetch('/api/v1/health')
        .then((res) => { if (!cancelled) setConnected(res.ok) })
        .catch(() => { if (!cancelled) setConnected(false) })
    }
    check()
    const interval = setInterval(check, 20000)
    return () => { cancelled = true; clearInterval(interval) }
  }, [])
  return connected
}

export default function App() {
  const [tab, setTab] = useState<Tab>('dash')
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const { token, setToken } = useToken()
  const connected = useDaemonHealth()

  // Cross-page navigation intents: a page (e.g. a Project card) can jump to
  // another tab pre-filled with context, without the two pages needing a
  // real router or knowing about each other directly.
  const [filesInitialPath, setFilesInitialPath] = useState<string | undefined>(undefined)
  const [deployProjectId, setDeployProjectId] = useState<string | undefined>(undefined)

  const openProjectFiles = (path: string) => { setFilesInitialPath(path); setTab('files') }
  const openProjectDeployments = (projectId: string) => { setDeployProjectId(projectId); setTab('deployments') }

  return (
    <div className="flex min-h-screen bg-slate-50">
      <Sidebar tab={tab} onSelect={setTab} open={sidebarOpen} onClose={() => setSidebarOpen(false)} />

      <div className="flex min-h-screen flex-1 flex-col md:min-w-0">
        <Topbar
          pageLabel={navLabel(tab)}
          onMenuClick={() => setSidebarOpen(true)}
          token={token}
          onTokenChange={setToken}
          connected={connected}
        />

        <main className="flex-1 p-4 sm:p-6">
          {tab === 'dash' && <Dashboard onNavigate={setTab} />}
          {tab === 'software' && <Software />}
          {tab === 'runtimes' && <Runtimes />}
          {tab === 'proc' && <Processes />}
          {tab === 'projects' && <Projects onOpenFiles={openProjectFiles} onOpenDeployments={openProjectDeployments} />}
          {tab === 'files' && <Files initialPath={filesInitialPath} />}
          {tab === 'logs' && <Logs />}
          {tab === 'docker' && <Docker />}
          {tab === 'domains' && <Domains />}
          {tab === 'extensions' && <Extensions />}
          {tab === 'git' && <Git />}
          {tab === 'servers' && <Servers />}
          {tab === 'deployments' && <Deployments initialProjectId={deployProjectId} />}
          {tab === 'pipelines' && <Pipelines />}
          {tab === 'settings' && <Settings />}
        </main>
      </div>
    </div>
  )
}
