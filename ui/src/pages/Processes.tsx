import React, { useEffect, useState } from 'react'
import { Activity, Play, RotateCw, Square } from 'lucide-react'
import { api, Service } from '../lib/api'
import { Badge, Button, Card, EmptyState, Input, Label, PageHeader, Table, Td, Th, Tone } from '../components/ui'

const statusTone: Record<string, Tone> = {
  running: 'success',
  stopped: 'neutral',
  failed: 'error',
}

export default function Processes() {
  const [procs, setProcs] = useState<Service[] | null>(null)
  const [form, setForm] = useState({ id: '', name: '', command: '', args: '', cwd: '' })
  const load = () => api.listServices().then(setProcs).catch(() => setProcs([]))
  useEffect(() => { load() }, [])

  return (
    <div>
      <PageHeader title="Processes" description="Start, stop, and monitor any process VoltPanel manages on this machine." />

      <Card title="Start a process" className="mb-4">
        <div className="flex flex-wrap items-end gap-3">
          <div><Label>ID</Label><Input value={form.id} onChange={e => setForm({ ...form, id: e.target.value })} /></div>
          <div><Label>Name</Label><Input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} /></div>
          <div><Label>Command</Label><Input value={form.command} onChange={e => setForm({ ...form, command: e.target.value })} /></div>
          <div><Label>Args (space separated)</Label><Input value={form.args} onChange={e => setForm({ ...form, args: e.target.value })} /></div>
          <div className="min-w-[16rem]"><Label>Working directory</Label><Input className="w-full" value={form.cwd} onChange={e => setForm({ ...form, cwd: e.target.value })} /></div>
          <Button variant="primary" onClick={() => {
            api.startService(form.id, { name: form.name, command: form.command, args: form.args ? form.args.split(' ') : [], cwd: form.cwd }).then(load)
          }}><Play size={14} className="mr-1" /> Start</Button>
        </div>
      </Card>

      {procs?.length === 0 ? (
        <EmptyState icon={<Activity size={28} />} title="No processes running" description="Start a process above to see it here." />
      ) : (
        <Card bodyClassName="p-0">
          <Table>
            <thead><tr><Th>ID</Th><Th>Name</Th><Th>PID</Th><Th>Status</Th><Th /></tr></thead>
            <tbody>
              {procs?.map(p => (
                <tr key={p.id}>
                  <Td className="font-mono text-xs">{p.id}</Td>
                  <Td className="font-medium text-slate-900">{p.name}</Td>
                  <Td className="font-mono text-xs text-slate-500">{p.pid || '—'}</Td>
                  <Td><Badge tone={statusTone[p.status] ?? 'neutral'}>{p.status}</Badge></Td>
                  <Td className="text-right space-x-2 whitespace-nowrap">
                    <Button size="sm" onClick={() => api.restartService(p.id).then(load)}><RotateCw size={13} className="mr-1" /> Restart</Button>
                    <Button size="sm" variant="danger" onClick={() => api.stopService(p.id).then(load)}><Square size={13} className="mr-1" /> Stop</Button>
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        </Card>
      )}
    </div>
  )
}
