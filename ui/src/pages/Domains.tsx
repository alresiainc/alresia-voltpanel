import React, { useEffect, useState } from 'react'
import { ExternalLink, Globe, Lock, ShieldCheck, Trash2 } from 'lucide-react'
import { api, ApiError, CAInfo, Domain, Project, Service } from '../lib/api'
import { Badge, Button, Card, EmptyState, ErrorNote, Input, Label, PageHeader, Select } from '../components/ui'

const projectServiceId = (projectId: string) => `project:${projectId}`

export default function Domains() {
  const [domains, setDomains] = useState<Domain[] | null>(null)
  const [projects, setProjects] = useState<Project[]>([])
  const [services, setServices] = useState<Service[]>([])
  const [proxyPort, setProxyPort] = useState(0)
  const [hostname, setHostname] = useState('')
  const [port, setPort] = useState('')
  const [projectId, setProjectId] = useState('')
  const [ca, setCa] = useState<CAInfo | null>(null)
  const [error, setError] = useState('')

  const load = () => {
    api.listDomains().then(setDomains).catch(() => setDomains([]))
    api.listServices().then(setServices).catch(() => {})
  }
  useEffect(() => {
    load()
    api.listProjects().then(setProjects).catch(() => {})
    api.proxyStatus().then(s => setProxyPort(s.port)).catch(() => {})
  }, [])

  const add = () => {
    setError('')
    api.createDomain(hostname, projectId ? undefined : (port ? Number(port) : undefined), projectId || undefined)
      .then(() => { setHostname(''); setPort(''); setProjectId(''); load() })
      .catch(e => setError(e instanceof ApiError ? e.message : 'failed to add domain'))
  }

  const remove = (id: string) => {
    if (!window.confirm('Remove this domain? This also removes its hosts-file entry.')) return
    api.deleteDomain(id).then(load).catch(e => setError(e instanceof ApiError ? e.message : 'failed to delete domain'))
  }

  const ensureCA = () => {
    api.ensureCA().then(setCa).catch(e => setError(e instanceof ApiError ? e.message : 'failed to create local CA'))
  }

  const issueCert = (id: string) => {
    api.issueCertificate(id).then(load).catch(e => setError(e instanceof ApiError ? e.message : 'failed to issue certificate'))
  }

  const trustCA = () => {
    if (!window.confirm(
      'This will add VoltPanel\'s local CA to your OS/browser trust store, so browsers stop warning about certificates it issues. ' +
      'This changes system trust settings on your machine. Continue?'
    )) return
    api.trustCA(true).then(() => setCa(c => c ? { ...c, trusted: true } : c)).catch(e => setError(e instanceof ApiError ? e.message : 'failed to trust CA'))
  }

  const projectFor = (id?: string) => projects.find(p => p.id === id)
  const isProjectRunning = (id?: string) => !!id && services.some(s => s.id === projectServiceId(id) && s.status === 'running')

  return (
    <div>
      <PageHeader
        title="Domains & SSL"
        description="Map a hostname to 127.0.0.1 and route it, through VoltPanel's built-in reverse proxy, to a running project."
      />

      <Card title="Add a domain" className="mb-4">
        <div className="flex flex-wrap items-end gap-3">
          <div>
            <Label>Hostname</Label>
            <Input placeholder="myapp.test" value={hostname} onChange={e => setHostname(e.target.value)} />
          </div>
          <div>
            <Label>Bind to project</Label>
            <Select value={projectId} onChange={e => setProjectId(e.target.value)}>
              <option value="">None (manual port)</option>
              {projects.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
            </Select>
          </div>
          {!projectId && (
            <div>
              <Label>Port (optional)</Label>
              <Input type="number" className="w-24" placeholder="3000" value={port} onChange={e => setPort(e.target.value)} />
            </div>
          )}
          <Button variant="primary" onClick={add}>Add domain</Button>
        </div>
        <p className="mt-2 text-xs text-slate-400">
          {proxyPort
            ? <>Bound domains are reachable at <code>http://&lt;hostname&gt;:{proxyPort}</code> via VoltPanel's built-in reverse proxy — binding the bare port 80/443 needs admin privileges, which isn't wired up yet.</>
            : 'Once bound to a running project, VoltPanel proxies requests for this hostname to that project\'s port.'}
        </p>
        {error && <div className="mt-2"><ErrorNote>{error}</ErrorNote></div>}
      </Card>

      <Card title="Local HTTPS" description="A local certificate authority, trusted per-machine, for issuing dev certs." className="mb-4">
        {!ca ? (
          <Button onClick={ensureCA}><ShieldCheck size={14} className="mr-1.5" /> Ensure local CA</Button>
        ) : (
          <div className="flex flex-wrap items-center gap-4 text-sm">
            <div className="text-slate-600">{ca.commonName} <span className="text-slate-400">· expires {ca.notAfter}</span></div>
            <Badge tone={ca.trusted ? 'success' : 'warning'}>{ca.trusted ? 'Trusted by this OS' : 'Not trusted yet'}</Badge>
            {!ca.trusted && (
              <Button size="sm" onClick={trustCA}>Trust CA (modifies OS trust store)</Button>
            )}
          </div>
        )}
      </Card>

      {domains?.length === 0 ? (
        <EmptyState
          icon={<Globe size={28} />}
          title="No domains yet"
          description="Add a hostname above to map it to 127.0.0.1 through your hosts file."
        />
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          {domains?.map(d => {
            const project = projectFor(d.projectId)
            const running = isProjectRunning(d.projectId)
            const reachable = running && proxyPort ? `http://${d.hostname}:${proxyPort}` : null
            return (
              <Card key={d.id}>
                <div className="flex items-start justify-between gap-2">
                  <div>
                    <div className="font-medium text-slate-900">{d.hostname}</div>
                    {project && <div className="text-xs text-slate-400">project: {project.name}</div>}
                    {!project && d.port ? <div className="text-xs text-slate-400">target port {d.port}</div> : null}
                  </div>
                  <Badge tone={d.enabled ? 'success' : 'neutral'}>{d.enabled ? 'Active' : 'Disabled'}</Badge>
                </div>
                <div className="mt-3 flex items-center gap-2">
                  <Lock size={13} className={d.sslEnabled ? 'text-emerald-500' : 'text-slate-300'} />
                  <span className="text-xs text-slate-500">SSL {d.sslEnabled ? 'enabled' : 'disabled'}</span>
                </div>
                {reachable ? (
                  <a href={reachable} target="_blank" rel="noreferrer" className="mt-2 inline-flex items-center gap-1 text-xs text-emerald-600 hover:underline">
                    <ExternalLink size={11} /> {reachable}
                  </a>
                ) : project ? (
                  <div className="mt-2 text-xs text-slate-400">Project isn't running — start it from Projects to make this reachable.</div>
                ) : null}
                <div className="mt-3 flex gap-2 border-t border-slate-100 pt-3">
                  {!d.sslEnabled && <Button size="sm" onClick={() => issueCert(d.id)}>Issue cert</Button>}
                  <Button size="sm" variant="danger" className="ml-auto" onClick={() => remove(d.id)}><Trash2 size={13} /></Button>
                </div>
              </Card>
            )
          })}
        </div>
      )}
    </div>
  )
}
