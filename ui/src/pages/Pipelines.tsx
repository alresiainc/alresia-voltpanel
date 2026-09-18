import React, { useEffect, useState } from 'react'
import { ArrowRight, History, Play, Plus, Trash2, Workflow } from 'lucide-react'
import { api, ApiError, Pipeline, PipelineRun, Project } from '../lib/api'
import { Badge, Button, Card, EmptyState, ErrorNote, Input, PageHeader, Select, Textarea, Tone } from '../components/ui'

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

const stepTone: Record<string, Tone> = { success: 'success', failed: 'error' }

function StepFlow({ steps }: { steps: PipelineRun['steps'] }) {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {steps.map((s, i) => (
        <React.Fragment key={s.name}>
          <Badge tone={stepTone[s.status] ?? 'neutral'}>{s.name}</Badge>
          {i < steps.length - 1 && <ArrowRight size={12} className="text-slate-300" />}
        </React.Fragment>
      ))}
    </div>
  )
}

export default function Pipelines() {
  const [projects, setProjects] = useState<Project[]>([])
  const [projectId, setProjectId] = useState('')
  const [pipelines, setPipelines] = useState<Pipeline[]>([])
  const [name, setName] = useState('')
  const [yamlDef, setYamlDef] = useState(DEFAULT_YAML)
  const [error, setError] = useState('')
  const [showForm, setShowForm] = useState(false)
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
      .then(p => { setWebhookInfo({ path: p.webhookPath, secret: p.webhookSecret }); setName(''); setShowForm(false); loadForProject(projectId) })
      .catch(e => setError(e instanceof ApiError ? e.message : 'failed to create pipeline'))
  }

  const remove = (id: string) => {
    if (!window.confirm('Delete this pipeline?')) return
    api.deletePipeline(id).then(() => loadForProject(projectId)).catch(e => setError(e instanceof ApiError ? e.message : 'failed to delete'))
  }

  const run = (id: string) => {
    setError('')
    api.runPipeline(id).then(() => showRuns(id)).catch(e => setError(e instanceof ApiError ? e.message : 'pipeline run failed'))
  }

  const showRuns = (id: string) => {
    api.listPipelineRuns(id).then(list => setRuns(r => ({ ...r, [id]: list }))).catch(() => {})
  }

  return (
    <div>
      <PageHeader
        title="Pipelines"
        description="A small YAML step runner, triggered manually or by a signed webhook."
        actions={
          <Select value={projectId} onChange={e => loadForProject(e.target.value)}>
            <option value="">Select a project…</option>
            {projects.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
          </Select>
        }
      />

      {!projectId && (
        <EmptyState icon={<Workflow size={28} />} title="Pick a project" description="Select a project above to see its pipelines." />
      )}

      {projectId && (
        <div className="space-y-4">
          <Card title="New pipeline" actions={<Button size="sm" onClick={() => setShowForm(v => !v)}><Plus size={13} className="mr-1" /> New pipeline</Button>}>
            {showForm && (
              <div className="space-y-2">
                <Input placeholder="pipeline name" value={name} onChange={e => setName(e.target.value)} />
                <Textarea value={yamlDef} onChange={e => setYamlDef(e.target.value)} className="h-40 w-full font-mono text-xs" />
                <Button variant="primary" onClick={create}>Create pipeline</Button>
              </div>
            )}
            {error && <div className="mt-2"><ErrorNote>{error}</ErrorNote></div>}
            {webhookInfo && (
              <div className="mt-2 space-y-1 rounded-md bg-slate-50 p-2.5 text-xs">
                <div>Webhook URL: <code className="text-slate-700">{webhookInfo.path}</code></div>
                <div>Secret (shown once — configure as an HMAC-SHA256 webhook secret on your git host): <code className="text-slate-700">{webhookInfo.secret}</code></div>
              </div>
            )}
          </Card>

          {pipelines.length === 0 ? (
            <EmptyState icon={<Workflow size={28} />} title="No pipelines yet" description="Create one above to define steps for this project." />
          ) : pipelines.map(p => (
            <Card
              key={p.id}
              title={p.name}
              actions={
                <>
                  <Button size="sm" onClick={() => run(p.id)}><Play size={12} className="mr-1" /> Run</Button>
                  <Button size="sm" onClick={() => showRuns(p.id)}><History size={12} className="mr-1" /> History</Button>
                  <Button size="sm" variant="danger" onClick={() => remove(p.id)}><Trash2 size={12} /></Button>
                </>
              }
            >
              {runs[p.id] && (
                <ul className="space-y-2 text-sm">
                  {runs[p.id].map(r => (
                    <li key={r.id} className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-50 pb-2">
                      <div className="flex items-center gap-2 text-xs text-slate-500">
                        <span>{r.startedAt}</span>
                        <Badge tone="neutral">{r.triggerKind}</Badge>
                        <Badge tone={r.status === 'success' ? 'success' : r.status === 'failed' ? 'error' : 'info'}>{r.status}</Badge>
                      </div>
                      {r.steps?.length > 0 && <StepFlow steps={r.steps} />}
                    </li>
                  ))}
                </ul>
              )}
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}
