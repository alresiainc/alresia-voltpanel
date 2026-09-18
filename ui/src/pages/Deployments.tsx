import React, { useEffect, useState } from 'react'
import { api, ApiError, Deployment, DeploymentTarget, Project, Server } from '../lib/api'

export default function Deployments() {
  const [projects, setProjects] = useState<Project[]>([])
  const [servers, setServers] = useState<Server[]>([])
  const [projectId, setProjectId] = useState('')
  const [targets, setTargets] = useState<DeploymentTarget[]>([])
  const [deployments, setDeployments] = useState<Deployment[]>([])
  const [error, setError] = useState('')
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

  const addTarget = () => {
    if (!projectId) return
    setError('')
    api.createDeploymentTarget({ projectId, ...form })
      .then(() => loadForProject(projectId))
      .catch(e => setError(e instanceof ApiError ? e.message : 'failed to add deployment target'))
  }

  const removeTarget = (id: string) => {
    if (!window.confirm('Remove this deployment target?')) return
    api.deleteDeploymentTarget(id).then(() => loadForProject(projectId)).catch(e => setError(e instanceof ApiError ? e.message : 'failed to remove target'))
  }

  const runDeploy = (targetId: string) => {
    setError('')
    api.deploy(targetId)
      .then(() => loadForProject(projectId))
      .catch(e => setError(e instanceof ApiError ? e.message : 'deploy failed'))
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
    <div className="space-y-4">
      <div className="flex gap-2 items-center">
        <select value={projectId} onChange={e => loadForProject(e.target.value)} className="border px-2">
          <option value="">Select a project…</option>
          {projects.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
        </select>
      </div>

      {projectId && (
        <>
          <div className="border p-3 space-y-2">
            <div className="font-medium">Deployment targets</div>
            <div className="flex gap-2 flex-wrap">
              <select value={form.serverId} onChange={e => setForm({ ...form, serverId: e.target.value })} className="border px-2">
                <option value="">server…</option>
                {servers.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
              </select>
              <input placeholder="repo URL" value={form.repoUrl} onChange={e => setForm({ ...form, repoUrl: e.target.value })} className="border px-2 w-64" />
              <input placeholder="branch" value={form.branch} onChange={e => setForm({ ...form, branch: e.target.value })} className="border px-2 w-24" />
              <input placeholder="deploy path" value={form.deployPath} onChange={e => setForm({ ...form, deployPath: e.target.value })} className="border px-2 w-48" />
              <input placeholder="install command" value={form.installCommand} onChange={e => setForm({ ...form, installCommand: e.target.value })} className="border px-2 w-48" />
              <input placeholder="restart command" value={form.restartCommand} onChange={e => setForm({ ...form, restartCommand: e.target.value })} className="border px-2 w-48" />
              <input placeholder="health check URL (optional)" value={form.healthCheckUrl} onChange={e => setForm({ ...form, healthCheckUrl: e.target.value })} className="border px-2 w-64" />
              <button onClick={addTarget} className="border px-3">Add target</button>
            </div>
            {error && <div className="text-red-600 text-sm">{error}</div>}
            <ul className="text-sm">
              {targets.map(t => (
                <li key={t.id} className="flex justify-between items-center border-b py-1">
                  <span>{t.repoUrl} → {t.deployPath} ({t.branch})</span>
                  <span className="space-x-2">
                    <button onClick={() => runDeploy(t.id)} className="border px-2">Deploy</button>
                    <button onClick={() => removeTarget(t.id)} className="border px-2">Remove</button>
                  </span>
                </li>
              ))}
            </ul>
          </div>

          <div className="border p-3 space-y-2">
            <div className="font-medium">History</div>
            <table className="w-full text-sm">
              <thead><tr><th className="text-left">When</th><th>Status</th><th>Commit</th><th></th></tr></thead>
              <tbody>
                {deployments.map(d => (
                  <tr key={d.id} className="border-b">
                    <td>{d.startedAt}</td>
                    <td className="text-center">{d.status}</td>
                    <td className="text-center font-mono text-xs">{d.commitSha?.slice(0, 8)}</td>
                    <td className="text-right space-x-2">
                      <button onClick={() => showLog(d.id)} className="border px-2">Log</button>
                      {d.codeRollbackRef && <button onClick={() => rollback(d.id)} className="border px-2">Rollback</button>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {!deployments.some(d => d.migrationRollbackSupported) && deployments.length > 0 && (
              <div className="text-xs text-gray-500">Note: rollback here only reverts code, never database migrations.</div>
            )}
            {log && <pre className="border p-2 max-h-64 overflow-auto whitespace-pre-wrap text-xs">{log}</pre>}
          </div>
        </>
      )}
    </div>
  )
}
