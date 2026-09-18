import React, { useEffect, useState } from 'react'
import { ExternalLink, FolderKanban, FolderOpen, Play, Rocket, RefreshCw, Square, Trash2, Plus } from 'lucide-react'
import { api, ApiError, Domain, Project, Service } from '../lib/api'
import { Badge, Button, Card, EmptyState, ErrorNote, Input, Label, PageHeader, Skeleton } from '../components/ui'

const projectServiceId = (projectId: string) => `project:${projectId}`

export default function Projects({
  onOpenFiles,
  onOpenDeployments,
}: {
  onOpenFiles: (path: string) => void
  onOpenDeployments: (projectId: string) => void
}) {
  const [projects, setProjects] = useState<Project[] | null>(null)
  const [domains, setDomains] = useState<Domain[]>([])
  const [services, setServices] = useState<Service[]>([])
  const [proxyPort, setProxyPort] = useState<number>(0)
  const [form, setForm] = useState({ name: '', path: '' })
  const [error, setError] = useState('')
  const [showForm, setShowForm] = useState(false)
  const [busyId, setBusyId] = useState('')

  const loadProjects = () => api.listProjects().then(setProjects).catch(() => setProjects([]))
  const loadLive = () => {
    api.listDomains().then(setDomains).catch(() => {})
    api.listServices().then(setServices).catch(() => {})
  }
  useEffect(() => {
    loadProjects()
    loadLive()
    api.proxyStatus().then(s => setProxyPort(s.port)).catch(() => {})
  }, [])

  const create = () => {
    setError('')
    api.createProject(form.name, form.path)
      .then(() => { setForm({ name: '', path: '' }); setShowForm(false); loadProjects() })
      .catch(e => setError(e instanceof ApiError ? e.message : 'failed to add project'))
  }

  const remove = (id: string) => {
    if (!window.confirm('Delete this project? This only removes VoltPanel\'s record of it, nothing on disk.')) return
    api.deleteProject(id).then(loadProjects).catch(e => setError(e instanceof ApiError ? e.message : 'failed to delete project'))
  }

  const redetect = (id: string) => {
    api.detectProject(id).then(loadProjects).catch(e => setError(e instanceof ApiError ? e.message : 'failed to detect framework'))
  }

  const start = (id: string) => {
    setError('')
    setBusyId(id)
    api.startProject(id).then(loadLive).catch(e => setError(e instanceof ApiError ? e.message : 'failed to start project')).finally(() => setBusyId(''))
  }

  const stop = (id: string) => {
    setError('')
    setBusyId(id)
    api.stopProject(id).then(loadLive).catch(e => setError(e instanceof ApiError ? e.message : 'failed to stop project')).finally(() => setBusyId(''))
  }

  const domainFor = (projectId: string) => domains.find(d => d.projectId === projectId)
  const serviceFor = (projectId: string) => services.find(s => s.id === projectServiceId(projectId))

  return (
    <div>
      <PageHeader
        title="Projects"
        description="Point VoltPanel at a repo, detect its framework, then start it and bind a domain to it."
        actions={<Button variant="primary" onClick={() => setShowForm(v => !v)}><Plus size={14} className="mr-1" /> New Project</Button>}
      />

      {showForm && (
        <Card className="mb-4" title="Add a project">
          <div className="flex flex-wrap items-end gap-3">
            <div>
              <Label>Name</Label>
              <Input placeholder="talents-api" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} />
            </div>
            <div className="flex-1 min-w-[16rem]">
              <Label>Absolute path</Label>
              <Input className="w-full" placeholder="/Users/you/code/talents-api" value={form.path} onChange={e => setForm({ ...form, path: e.target.value })} />
            </div>
            <Button variant="primary" onClick={create}>Add project</Button>
          </div>
          {error && <div className="mt-2"><ErrorNote>{error}</ErrorNote></div>}
        </Card>
      )}

      {projects === null && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {[0, 1, 2].map(i => <Skeleton key={i} className="h-40" />)}
        </div>
      )}

      {projects?.length === 0 && (
        <EmptyState
          icon={<FolderKanban size={28} />}
          title="No projects yet"
          description="Create your first project and let Volt manage its runtime, domain, processes and services."
          action={<Button variant="primary" onClick={() => setShowForm(true)}>Create Project</Button>}
        />
      )}

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {projects?.map(p => {
          const domain = domainFor(p.id)
          const service = serviceFor(p.id)
          const running = service?.status === 'running'
          const reachable = running && domain && proxyPort ? `http://${domain.hostname}:${proxyPort}` : null
          return (
            <Card key={p.id} className="flex flex-col">
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <div className="truncate font-semibold text-slate-900">{p.name}</div>
                  <div className="truncate text-xs text-slate-500">{p.path}</div>
                </div>
                <div className="flex flex-col items-end gap-1">
                  {p.detectedKind && <Badge tone="info">{p.detectedKind}</Badge>}
                  <Badge tone={running ? 'success' : 'neutral'}>{running ? 'Running' : 'Stopped'}</Badge>
                </div>
              </div>

              <div className="mt-3 space-y-1 text-xs text-slate-500">
                <div>Run command: <span className="font-mono text-slate-700">{p.runCommand || 'unknown'}</span></div>
                {running && service?.env?.PORT && (
                  <div>Listening on <span className="font-mono text-slate-700">127.0.0.1:{service.env.PORT}</span></div>
                )}
                {domain && <div>Domain: <span className="font-mono text-slate-700">{domain.hostname}</span></div>}
                {reachable && (
                  <a href={reachable} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-emerald-600 hover:underline">
                    <ExternalLink size={11} /> {reachable}
                  </a>
                )}
                {domain && !running && <div className="text-slate-400">Start the project to make this domain reachable.</div>}
              </div>

              <div className="mt-4 flex flex-wrap gap-2 border-t border-slate-100 pt-3">
                {running ? (
                  <Button size="sm" variant="danger" disabled={busyId === p.id} onClick={() => stop(p.id)}><Square size={13} className="mr-1" /> Stop</Button>
                ) : (
                  <Button size="sm" variant="primary" disabled={busyId === p.id} onClick={() => start(p.id)}><Play size={13} className="mr-1" /> Start</Button>
                )}
                <Button size="sm" onClick={() => onOpenFiles(p.path)}><FolderOpen size={13} className="mr-1" /> Files</Button>
                <Button size="sm" onClick={() => onOpenDeployments(p.id)}><Rocket size={13} className="mr-1" /> Deploy</Button>
                <Button size="sm" onClick={() => redetect(p.id)}><RefreshCw size={13} className="mr-1" /> Re-detect</Button>
                <Button size="sm" variant="danger" className="ml-auto" onClick={() => remove(p.id)}><Trash2 size={13} /></Button>
              </div>
            </Card>
          )
        })}
      </div>

      {!showForm && error && <div className="mt-4"><ErrorNote>{error}</ErrorNote></div>}
    </div>
  )
}
