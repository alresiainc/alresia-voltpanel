import React, { useEffect, useState } from 'react'
import { GitCommit, Plus, Rocket, RotateCcw, ScrollText, Trash2 } from 'lucide-react'
import { api, ApiError, Deployment, DeploymentTarget, Project, Server } from '../lib/api'
import { Badge, Button, Card, EmptyState, ErrorNote, Input, PageHeader, Select, Table, Td, Th, Tone } from '../components/ui'

const statusTone: Record<string, Tone> = {
  success: 'success',
  failed: 'error',
  running: 'info',
}

export default function Deployments({ initialProjectId }: { initialProjectId?: string }) {
  const [projects, setProjects] = useState<Project[]>([])
  const [servers, setServers] = useState<Server[]>([])
  const [projectId, setProjectId] = useState('')
  const [targets, setTargets] = useState<DeploymentTarget[]>([])
  const [deployments, setDeployments] = useState<Deployment[]>([])
  const [error, setError] = useState('')
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState({ serverId: '', repoUrl: '', branch: 'main', deployPath: '', installCommand: '', restartCommand: '', healthCheckUrl: '' })
  const [log, setLog] = useState('')

  useEffect(() => {
    api.listProjects().then(setProjects).catch(() => {})
    api.listServers().then(setServers).catch(() => {})
  }, [])

  const loadForProject = (id: string) => {
    setProjectId(id)
    if (!id) return
    api.listDeploymentTargets(id).then(setTargets).catch(() => {})
    api.listDeployments(id).then(setDeployments).catch(() => {})
  }
  useEffect(() => { if (initialProjectId) loadForProject(initialProjectId) }, [initialProjectId]) // eslint-disable-line react-hooks/exhaustive-deps

  const addTarget = () => {
    if (!projectId) return
    setError('')
    api.createDeploymentTarget({ projectId, ...form })
      .then(() => { setShowForm(false); loadForProject(projectId) })
      .catch(e => setError(e instanceof ApiError ? e.message : 'failed to add deployment target'))
  }

  const removeTarget = (id: string) => {
    if (!window.confirm('Remove this deployment target?')) return
    api.deleteDeploymentTarget(id).then(() => loadForProject(projectId)).catch(e => setError(e instanceof ApiError ? e.message : 'failed to remove target'))
  }

  const runDeploy = (targetId: string) => {
    setError('')
    api.deploy(targetId).then(() => loadForProject(projectId)).catch(e => setError(e instanceof ApiError ? e.message : 'deploy failed'))
  }

  const rollback = (id: string) => {
    if (!window.confirm(
      'Roll back to the previous code checkout and restart? This does NOT undo any database migrations that ran since -- ' +
      'if this deployment included migrations, they will still be applied on top of the older code.'
    )) return
    api.rollbackDeployment(id).then(() => loadForProject(projectId)).catch(e => setError(e instanceof ApiError ? e.message : 'rollback failed'))
  }

  const showLog = (id: string) => {
    api.deploymentLog(id).then(setLog).catch(e => setLog(e instanceof ApiError ? e.message : 'failed to load log'))
  }

  return (
    <div>
      <PageHeader
        title="Deployments"
        description="Checkout → install → restart → health-check over SSH, with history and rollback."
        actions={
          <Select value={projectId} onChange={e => loadForProject(e.target.value)}>
            <option value="">Select a project…</option>
            {projects.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
          </Select>
        }
      />

      {!projectId && (
        <EmptyState icon={<Rocket size={28} />} title="Pick a project" description="Select a project above to see its deployment targets and history." />
      )}

      {projectId && (
        <div className="space-y-4">
          <Card
            title="Deployment targets"
            actions={<Button size="sm" onClick={() => setShowForm(v => !v)}><Plus size={13} className="mr-1" /> Add target</Button>}
          >
            {showForm && (
              <div className="mb-3 flex flex-wrap items-end gap-2 rounded-md bg-slate-50 p-3">
                <Select value={form.serverId} onChange={e => setForm({ ...form, serverId: e.target.value })}>
                  <option value="">server…</option>
                  {servers.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
                </Select>
                <Input placeholder="repo URL" value={form.repoUrl} onChange={e => setForm({ ...form, repoUrl: e.target.value })} className="w-56" />
                <Input placeholder="branch" value={form.branch} onChange={e => setForm({ ...form, branch: e.target.value })} className="w-24" />
                <Input placeholder="deploy path" value={form.deployPath} onChange={e => setForm({ ...form, deployPath: e.target.value })} className="w-44" />
                <Input placeholder="install command" value={form.installCommand} onChange={e => setForm({ ...form, installCommand: e.target.value })} className="w-44" />
                <Input placeholder="restart command" value={form.restartCommand} onChange={e => setForm({ ...form, restartCommand: e.target.value })} className="w-44" />
                <Input placeholder="health check URL (optional)" value={form.healthCheckUrl} onChange={e => setForm({ ...form, healthCheckUrl: e.target.value })} className="w-56" />
                <Button variant="primary" size="sm" onClick={addTarget}>Save target</Button>
              </div>
            )}
            {error && <div className="mb-2"><ErrorNote>{error}</ErrorNote></div>}
            {targets.length === 0 ? (
              <div className="text-sm text-slate-400">No deployment targets yet for this project.</div>
            ) : (
              <ul className="divide-y divide-slate-50 text-sm">
                {targets.map(t => (
                  <li key={t.id} className="flex items-center justify-between py-2">
                    <span className="text-slate-700">{t.repoUrl} <span className="text-slate-400">→</span> {t.deployPath} <Badge tone="neutral">{t.branch}</Badge></span>
                    <span className="flex gap-2">
                      <Button size="sm" variant="primary" onClick={() => runDeploy(t.id)}><Rocket size={12} className="mr-1" /> Deploy</Button>
                      <Button size="sm" variant="danger" onClick={() => removeTarget(t.id)}><Trash2 size={12} /></Button>
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </Card>

          <Card title="History">
            {deployments.length === 0 ? (
              <div className="text-sm text-slate-400">No deployments yet.</div>
            ) : (
              <Table>
                <thead><tr><Th>When</Th><Th>Status</Th><Th>Commit</Th><Th /></tr></thead>
                <tbody>
                  {deployments.map(d => (
                    <tr key={d.id}>
                      <Td className="text-xs text-slate-500">{d.startedAt}</Td>
                      <Td><Badge tone={statusTone[d.status] ?? 'neutral'}>{d.status}</Badge></Td>
                      <Td className="font-mono text-xs"><GitCommit size={12} className="mr-1 inline text-slate-400" />{d.commitSha?.slice(0, 8) || '—'}</Td>
                      <Td className="text-right space-x-2 whitespace-nowrap">
                        <Button size="sm" onClick={() => showLog(d.id)}><ScrollText size={12} className="mr-1" /> Log</Button>
                        {d.codeRollbackRef && <Button size="sm" onClick={() => rollback(d.id)}><RotateCcw size={12} className="mr-1" /> Rollback</Button>}
                      </Td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
            {!deployments.some(d => d.migrationRollbackSupported) && deployments.length > 0 && (
              <div className="mt-2 text-xs text-slate-400">Note: rollback here only reverts code, never database migrations.</div>
            )}
            {log && <pre className="volt-scroll mt-3 max-h-64 overflow-auto whitespace-pre-wrap rounded-md bg-slate-950 p-3 text-xs text-slate-200">{log}</pre>}
          </Card>
        </div>
      )}
    </div>
  )
}
