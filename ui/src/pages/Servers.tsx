import React, { useEffect, useState } from 'react'
import { Plug, Server as ServerIcon, Terminal, Trash2 } from 'lucide-react'
import { api, ApiError, RemoteMetrics, Server } from '../lib/api'
import { Badge, Button, Card, EmptyState, ErrorNote, Input, Label, PageHeader, Select, Textarea } from '../components/ui'

export default function Servers() {
  const [servers, setServers] = useState<Server[] | null>(null)
  const [form, setForm] = useState({ name: '', hostname: '', port: 22, username: '', authMethod: 'agent' as 'agent' | 'key', key: '' })
  const [error, setError] = useState('')
  const [metrics, setMetrics] = useState<Record<string, RemoteMetrics | string>>({})
  const [command, setCommand] = useState('')
  const [output, setOutput] = useState('')

  const load = () => api.listServers().then(setServers).catch(() => setServers([]))
  useEffect(() => { load() }, [])

  const add = () => {
    setError('')
    api.createServer({ name: form.name, hostname: form.hostname, port: form.port, username: form.username, authMethod: form.authMethod, key: form.authMethod === 'key' ? form.key : undefined })
      .then(() => { setForm({ name: '', hostname: '', port: 22, username: '', authMethod: 'agent', key: '' }); load() })
      .catch(e => setError(e instanceof ApiError ? e.message : 'failed to add server'))
  }

  const remove = (id: string) => {
    if (!window.confirm('Remove this server? Its stored credential (if any) will be deleted too.')) return
    api.deleteServer(id).then(load).catch(e => setError(e instanceof ApiError ? e.message : 'failed to remove server'))
  }

  const test = (id: string) => {
    api.testServerConnection(id)
      .then(() => setMetrics(m => ({ ...m, [id]: 'connection OK' })))
      .catch(e => setMetrics(m => ({ ...m, [id]: e instanceof ApiError ? e.message : 'connection failed' })))
  }

  const showMetrics = (id: string) => {
    api.serverMetrics(id).then(m => setMetrics(prev => ({ ...prev, [id]: m }))).catch(e => setMetrics(m => ({ ...m, [id]: e instanceof ApiError ? e.message : 'failed to fetch metrics' })))
  }

  const exec = (id: string) => {
    api.execServer(id, command).then(r => setOutput(r.stdout + (r.stderr ? '\n[stderr] ' + r.stderr : ''))).catch(e => setOutput(e instanceof ApiError ? e.message : 'exec failed'))
  }

  return (
    <div>
      <PageHeader title="Servers" description="Remote hosts reachable over SSH — files, metrics, and one-off commands." />

      <Card title="Connect a server" className="mb-4">
        <div className="flex flex-wrap items-end gap-3">
          <div><Label>Name</Label><Input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} /></div>
          <div><Label>Hostname</Label><Input value={form.hostname} onChange={e => setForm({ ...form, hostname: e.target.value })} /></div>
          <div><Label>Port</Label><Input type="number" className="w-20" value={form.port} onChange={e => setForm({ ...form, port: Number(e.target.value) })} /></div>
          <div><Label>Username</Label><Input value={form.username} onChange={e => setForm({ ...form, username: e.target.value })} /></div>
          <div>
            <Label>Auth method</Label>
            <Select value={form.authMethod} onChange={e => setForm({ ...form, authMethod: e.target.value as 'agent' | 'key' })}>
              <option value="agent">ssh-agent</option>
              <option value="key">private key</option>
            </Select>
          </div>
          <Button variant="primary" onClick={add}><Plug size={14} className="mr-1" /> Add server</Button>
        </div>
        {form.authMethod === 'key' && (
          <Textarea className="mt-3 h-20 w-full font-mono text-xs" placeholder="private key (PEM)" value={form.key} onChange={e => setForm({ ...form, key: e.target.value })} />
        )}
        {error && <div className="mt-2"><ErrorNote>{error}</ErrorNote></div>}
      </Card>

      {servers?.length === 0 ? (
        <EmptyState icon={<ServerIcon size={28} />} title="No servers connected" description="Add a remote host above to browse its files, run commands, and see basic metrics." />
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          {servers?.map(s => (
            <Card key={s.id}>
              <div className="flex items-start justify-between gap-2">
                <div>
                  <div className="font-medium text-slate-900">{s.name}</div>
                  <div className="text-xs text-slate-500">{s.username}@{s.hostname}:{s.port}</div>
                </div>
                <Badge tone="info">{s.authMethod}</Badge>
              </div>

              {metrics[s.id] && (
                <div className="mt-2 rounded-md bg-slate-50 px-2.5 py-1.5 text-xs text-slate-600">
                  {typeof metrics[s.id] === 'string' ? metrics[s.id] as string : JSON.stringify(metrics[s.id])}
                </div>
              )}

              <div className="mt-3 flex flex-wrap gap-2 border-t border-slate-100 pt-3">
                <Button size="sm" onClick={() => test(s.id)}>Test</Button>
                <Button size="sm" onClick={() => showMetrics(s.id)}>Metrics</Button>
                <Button size="sm" variant="danger" className="ml-auto" onClick={() => remove(s.id)}><Trash2 size={13} /></Button>
              </div>
            </Card>
          ))}
        </div>
      )}

      {servers && servers.length > 0 && (
        <Card title="Run a command" className="mt-4" description={`Runs on ${servers[0].name}`}>
          <div className="flex gap-2">
            <Input className="flex-1" placeholder="command" value={command} onChange={e => setCommand(e.target.value)} />
            <Button variant="primary" onClick={() => exec(servers[0].id)}><Terminal size={14} className="mr-1" /> Run</Button>
          </div>
          {output && <pre className="volt-scroll mt-3 max-h-64 overflow-auto whitespace-pre-wrap rounded-md bg-slate-950 p-3 text-xs text-slate-200">{output}</pre>}
        </Card>
      )}
    </div>
  )
}
