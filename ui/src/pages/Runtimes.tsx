import React, { useEffect, useState } from 'react'
import { Download, Layers, Loader2, RefreshCw, Search, Trash2 } from 'lucide-react'
import { api, Runtime } from '../lib/api'
import { wsClient } from '../lib/ws'
import { Badge, Button, Card, EmptyState, ErrorNote, Input, PageHeader, Skeleton, Table, Td, Th, Tone } from '../components/ui'

const statusTone: Record<string, Tone> = {
  installed: 'success',
  installing: 'warning',
  missing: 'neutral',
}

// Kinds with a real, native (no Homebrew/nvm/shell dependency) installer
// behind them -- see internal/providers/runtime/node. Everything else
// still only supports detection + set-default, so the install UI stays
// scoped to what's actually backed by something real.
const NATIVE_INSTALL_KINDS = new Set(['node'])

function InstallForm({ kind, onInstalled }: { kind: string; onInstalled: () => void }) {
  const [version, setVersion] = useState('')
  const [installing, setInstalling] = useState(false)
  const [progressLine, setProgressLine] = useState('')
  const [error, setError] = useState('')

  useEffect(() => {
    if (!installing) return
    return wsClient.subscribe(data => {
      if (data.type === 'log' && data.id === `runtime:${kind}:${version}`) {
        setProgressLine(data.data.trim())
      }
    })
  }, [installing, kind, version])

  const install = () => {
    if (!version.trim()) return
    setError('')
    setInstalling(true)
    setProgressLine('starting…')
    api.installRuntimeVersion(kind, version.trim())
      .then(() => { setVersion(''); onInstalled() })
      .catch(e => setError(e.message || 'install failed'))
      .finally(() => setInstalling(false))
  }

  return (
    <div className="mb-3 flex flex-wrap items-end gap-2 rounded-md bg-slate-50 p-3">
      <div className="flex-1 min-w-[12rem]">
        <Input
          className="w-full"
          placeholder="e.g. 20.11.0 -- downloaded directly from nodejs.org"
          value={version}
          onChange={e => setVersion(e.target.value)}
          disabled={installing}
        />
      </div>
      <Button size="sm" variant="primary" onClick={install} disabled={installing || !version.trim()}>
        {installing ? <Loader2 size={13} className="mr-1 animate-spin" /> : <Download size={13} className="mr-1" />}
        {installing ? 'Installing…' : 'Install'}
      </Button>
      {installing && progressLine && <span className="text-xs text-slate-500">{progressLine}</span>}
      {error && <div className="w-full"><ErrorNote>{error}</ErrorNote></div>}
    </div>
  )
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

  const remove = (kind: string, version: string) => {
    if (!window.confirm(`Remove ${kind}@${version}? This deletes the downloaded copy VoltPanel manages.`)) return
    api.removeRuntimeVersion(kind, version).then(load).catch(e => setError(e.message))
  }

  return (
    <div>
      <PageHeader
        title="Runtimes"
        description="Detected language runtimes on this machine, plus native version management where VoltPanel can download real binaries directly (no Homebrew)."
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
        {runtimes?.map(rt => {
          const native = NATIVE_INSTALL_KINDS.has(rt.kind)
          return (
            <Card
              key={rt.id}
              title={<span>{rt.name} <span className="font-normal text-slate-400">({rt.kind})</span>{native && <Badge tone="info">native</Badge>}</span>}
              actions={<Button size="sm" onClick={() => detect(rt.kind)} disabled={detecting === rt.kind}>
                <Search size={13} className="mr-1" /> {detecting === rt.kind ? 'Detecting…' : 'Detect'}
              </Button>}
            >
              {native && <InstallForm kind={rt.kind} onInstalled={load} />}

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
                        <Td className="text-right space-x-2 whitespace-nowrap">
                          {!v.isDefault && (
                            <Button size="sm" onClick={() => setDefault(rt.kind, v.version)}>Set default</Button>
                          )}
                          {native && <Button size="sm" variant="danger" onClick={() => remove(rt.kind, v.version)}><Trash2 size={12} /></Button>}
                        </Td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              )}
            </Card>
          )
        })}
      </div>
    </div>
  )
}
