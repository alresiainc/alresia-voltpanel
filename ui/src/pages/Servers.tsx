import React, { useEffect, useState } from 'react'
import { api, ApiError, RemoteMetrics, Server } from '../lib/api'

export default function Servers() {
  const [servers, setServers] = useState<Server[]>([])
  const [form, setForm] = useState({ name: '', hostname: '', port: 22, username: '', authMethod: 'agent' as 'agent' | 'key', key: '' })
  const [error, setError] = useState('')
  const [metrics, setMetrics] = useState<Record<string, RemoteMetrics | string>>({})
  const [command, setCommand] = useState('')
  const [output, setOutput] = useState('')

  const load = () => api.listServers().then(setServers).catch(() => {})
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
    <div className="space-y-4">
      <div className="border p-3 space-y-2">
        <div className="flex gap-2 flex-wrap">
          <input placeholder="name" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} className="border px-2" />
          <input placeholder="hostname" value={form.hostname} onChange={e => setForm({ ...form, hostname: e.target.value })} className="border px-2" />
          <input placeholder="port" type="number" value={form.port} onChange={e => setForm({ ...form, port: Number(e.target.value) })} className="border px-2 w-20" />
          <input placeholder="username" value={form.username} onChange={e => setForm({ ...form, username: e.target.value })} className="border px-2" />
          <select value={form.authMethod} onChange={e => setForm({ ...form, authMethod: e.target.value as 'agent' | 'key' })} className="border px-2">
            <option value="agent">ssh-agent</option>
            <option value="key">private key</option>
          </select>
          {form.authMethod === 'key' && (
            <textarea placeholder="private key (PEM)" value={form.key} onChange={e => setForm({ ...form, key: e.target.value })} className="border px-2 w-96 h-16" />
          )}
          <button onClick={add} className="border px-3">Add server</button>
        </div>
        {error && <div className="text-red-600 text-sm">{error}</div>}
      </div>

      <table className="w-full text-sm">
        <thead><tr><th className="text-left">Name</th><th>Host</th><th>Auth</th><th></th></tr></thead>
        <tbody>
          {servers.map(s => (
            <tr key={s.id} className="border-b align-top">
              <td>{s.name}</td>
              <td>{s.hostname}:{s.port} ({s.username})</td>
              <td className="text-center">{s.authMethod}</td>
              <td className="text-right space-x-2">
                <button onClick={() => test(s.id)} className="border px-2">Test</button>
                <button onClick={() => showMetrics(s.id)} className="border px-2">Metrics</button>
                <button onClick={() => remove(s.id)} className="border px-2">Remove</button>
                {metrics[s.id] && (
                  <div className="text-xs text-gray-600 mt-1">
                    {typeof metrics[s.id] === 'string' ? metrics[s.id] as string : JSON.stringify(metrics[s.id])}
                  </div>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <div className="border p-3 space-y-2">
        <div className="font-medium">Exec (pick a server by id from the list above)</div>
        <div className="flex gap-2">
          <input placeholder="command" value={command} onChange={e => setCommand(e.target.value)} className="border px-2 w-96" />
          {servers[0] && <button onClick={() => exec(servers[0].id)} className="border px-3">Run on {servers[0].name}</button>}
        </div>
        <pre className="border p-2 max-h-64 overflow-auto whitespace-pre-wrap text-xs">{output}</pre>
      </div>
    </div>
  )
}
