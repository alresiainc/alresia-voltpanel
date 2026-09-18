import React, { useEffect, useState } from 'react'
import { api, ApiError, Pipeline, PipelineRun, Project } from '../lib/api'

const DEFAULT_YAML = `steps:
  - name: test
    run: npm test
  - name: deploy
    deploy: <deployment-target-id>
  - name: verify
    healthcheck:
      url: https://myapp.test/health
      expectedStatus: 200
`

export default function Pipelines() {
  const [projects, setProjects] = useState<Project[]>([])
  const [projectId, setProjectId] = useState('')
  const [pipelines, setPipelines] = useState<Pipeline[]>([])
  const [name, setName] = useState('')
  const [yamlDef, setYamlDef] = useState(DEFAULT_YAML)
  const [error, setError] = useState('')
  const [webhookInfo, setWebhookInfo] = useState<{ path: string; secret: string } | null>(null)
  const [runs, setRuns] = useState<Record<string, PipelineRun[]>>({})

  useEffect(() => { api.listProjects().then(setProjects).catch(() => {}) }, [])

  const loadForProject = (id: string) => {
    setProjectId(id)
    if (!id) return
    api.listPipelines(id).then(setPipelines).catch(() => {})
  }

  const create = () => {
    if (!projectId || !name) return
    setError('')
    api.createPipeline(projectId, name, yamlDef)
      .then(p => {
        setWebhookInfo({ path: p.webhookPath, secret: p.webhookSecret })
        setName('')
        loadForProject(projectId)
      })
      .catch(e => setError(e instanceof ApiError ? e.message : 'failed to create pipeline'))
  }

  const remove = (id: string) => {
    if (!window.confirm('Delete this pipeline?')) return
    api.deletePipeline(id).then(() => loadForProject(projectId)).catch(e => setError(e instanceof ApiError ? e.message : 'failed to delete'))
  }

  const run = (id: string) => {
    setError('')
    api.runPipeline(id)
      .then(() => showRuns(id))
      .catch(e => setError(e instanceof ApiError ? e.message : 'pipeline run failed'))
  }

  const showRuns = (id: string) => {
    api.listPipelineRuns(id).then(list => setRuns(r => ({ ...r, [id]: list }))).catch(() => {})
  }

  return (
    <div className="space-y-4">
      <select value={projectId} onChange={e => loadForProject(e.target.value)} className="border px-2">
        <option value="">Select a project…</option>
        {projects.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
      </select>

      {projectId && (
        <>
          <div className="border p-3 space-y-2">
            <div className="flex gap-2">
              <input placeholder="pipeline name" value={name} onChange={e => setName(e.target.value)} className="border px-2" />
              <button onClick={create} className="border px-3">Create pipeline</button>
            </div>
            <textarea value={yamlDef} onChange={e => setYamlDef(e.target.value)} className="border px-2 w-full h-40 font-mono text-xs" />
            {error && <div className="text-red-600 text-sm">{error}</div>}
            {webhookInfo && (
              <div className="text-xs border p-2 bg-gray-50 space-y-1">
                <div>Webhook URL: <code>{webhookInfo.path}</code></div>
                <div>Secret (shown once — configure it on your git host as an HMAC-SHA256 webhook secret): <code>{webhookInfo.secret}</code></div>
              </div>
            )}
          </div>

          {pipelines.map(p => (
            <div key={p.id} className="border p-3 space-y-2">
              <div className="flex justify-between items-center">
                <span className="font-medium">{p.name}</span>
                <span className="space-x-2">
                  <button onClick={() => run(p.id)} className="border px-2">Run</button>
                  <button onClick={() => showRuns(p.id)} className="border px-2">History</button>
                  <button onClick={() => remove(p.id)} className="border px-2">Delete</button>
                </span>
              </div>
              {runs[p.id] && (
                <table className="w-full text-xs">
                  <thead><tr><th className="text-left">When</th><th>Trigger</th><th>Status</th><th></th></tr></thead>
                  <tbody>
                    {runs[p.id].map(r => (
                      <tr key={r.id} className="border-b">
                        <td>{r.startedAt}</td>
                        <td className="text-center">{r.triggerKind}</td>
                        <td className="text-center">{r.status}</td>
                        <td>{r.steps?.map(s => <div key={s.name}>{s.name}: {s.status}</div>)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          ))}
        </>
      )}
    </div>
  )
}
