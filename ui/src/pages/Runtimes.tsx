import React, { useEffect, useState } from 'react'
import { Layers, RefreshCw, Search } from 'lucide-react'
import { api, Runtime } from '../lib/api'
import { Badge, Button, Card, EmptyState, ErrorNote, PageHeader, Skeleton, Table, Td, Th, Tone } from '../components/ui'

const statusTone: Record<string, Tone> = {
  installed: 'success',
  installing: 'warning',
  missing: 'neutral',
}

export default function Runtimes() {
  const [runtimes, setRuntimes] = useState<Runtime[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [detecting, setDetecting] = useState('')
  const [error, setError] = useState('')

  const load = () => {
    setLoading(true)
    setError('')
    return api.listRuntimes()
      .then(setRuntimes)
      .catch(e => setError(e.message || 'failed to load runtimes'))
      .finally(() => setLoading(false))
  }
  useEffect(() => { load() }, [])

  const detect = (kind: string) => {
    setDetecting(kind)
    api.detectRuntime(kind).then(load).catch(e => setError(e.message)).finally(() => setDetecting(''))
  }

  const setDefault = (kind: string, version: string) => {
    api.setDefaultRuntimeVersion(kind, version).then(load).catch(e => setError(e.message))
  }

  return (
    <div>
      <PageHeader
        title="Runtimes"
        description="Detected language runtimes on this machine and their installed versions."
        actions={<Button variant="secondary" onClick={load} disabled={loading}><RefreshCw size={14} className={`mr-1.5 ${loading ? 'animate-spin' : ''}`} /> Refresh all</Button>}
      />

      {error && <div className="mb-4"><ErrorNote>{error}</ErrorNote></div>}

      {runtimes === null && (
        <div className="space-y-4">
          <Skeleton className="h-40" />
          <Skeleton className="h-40" />
        </div>
      )}

      {runtimes?.length === 0 && (
        <EmptyState
          icon={<Layers size={28} />}
          title="No runtimes detected yet"
          description="Click Refresh all to scan this machine for installed Node.js, PHP, and other runtimes."
          action={<Button variant="primary" onClick={load}>Refresh all</Button>}
        />
      )}

      <div className="space-y-4">
        {runtimes?.map(rt => (
          <Card
            key={rt.id}
            title={<span>{rt.name} <span className="font-normal text-slate-400">({rt.kind})</span></span>}
            actions={<Button size="sm" onClick={() => detect(rt.kind)} disabled={detecting === rt.kind}>
              <Search size={13} className="mr-1" /> {detecting === rt.kind ? 'Detecting…' : 'Detect'}
            </Button>}
          >
            {rt.versions.length === 0 ? (
              <div className="py-2 text-sm text-slate-400">No versions detected.</div>
            ) : (
              <Table>
                <thead><tr><Th>Version</Th><Th>Install path</Th><Th>Status</Th><Th>Default</Th><Th /></tr></thead>
                <tbody>
                  {rt.versions.map(v => (
                    <tr key={v.id}>
                      <Td className="font-medium text-slate-900">{v.version}</Td>
                      <Td className="font-mono text-xs text-slate-500">{v.installPath || '—'}</Td>
                      <Td><Badge tone={statusTone[v.status] ?? 'neutral'}>{v.status}</Badge></Td>
                      <Td>{v.isDefault && <Badge tone="info">Default</Badge>}</Td>
                      <Td className="text-right">
                        {!v.isDefault && (
                          <Button size="sm" onClick={() => setDefault(rt.kind, v.version)}>Set default</Button>
                        )}
                      </Td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
          </Card>
        ))}
      </div>
    </div>
  )
}
