import React, { useEffect, useState } from 'react'
import { Box, Container, Play, RotateCw, ScrollText, Square } from 'lucide-react'
import { api, ApiError, ComposeProject, DockerContainer } from '../lib/api'
import { Badge, Button, Card, EmptyState, PageHeader, Table, Td, Th, Tone } from '../components/ui'

// Docker is unavailable until proven otherwise (matches the daemon's own
// default assumption -- see internal/api/v1/docker.go): most dev machines
// this daemon runs on won't have Docker running, so the empty/error state
// is the common case, not an edge case.
type LoadState = 'loading' | 'unavailable' | 'error' | 'ready'

const stateTone: Record<string, Tone> = {
  running: 'success',
  restarting: 'warning',
  paused: 'warning',
  exited: 'neutral',
  dead: 'error',
  created: 'info',
}

export default function Docker() {
  const [state, setState] = useState<LoadState>('loading')
  const [errorMessage, setErrorMessage] = useState('')
  const [groups, setGroups] = useState<ComposeProject[]>([])
  const [selected, setSelected] = useState<string>('')
  const [logs, setLogs] = useState('')
  const [busyId, setBusyId] = useState<string>('')

  const load = () => {
    setState('loading')
    api.listDockerContainersGrouped()
      .then((g) => { setGroups(g); setState('ready') })
      .catch((err: unknown) => {
        if (err instanceof ApiError && err.status === 503) {
          setState('unavailable')
          setErrorMessage(err.message)
        } else {
          setState('error')
          setErrorMessage(err instanceof Error ? err.message : String(err))
        }
      })
  }
  useEffect(() => { load() }, [])

  const runAction = (id: string, action: 'start' | 'stop' | 'restart') => {
    setBusyId(id)
    const call = action === 'start' ? api.startDockerContainer : action === 'stop' ? api.stopDockerContainer : api.restartDockerContainer
    call(id).then(load).catch((err: unknown) => setErrorMessage(err instanceof Error ? err.message : String(err))).finally(() => setBusyId(''))
  }

  const showLogs = (id: string) => {
    setSelected(id)
    api.dockerContainerLogs(id, 200).then(setLogs).catch((err: unknown) => setLogs(err instanceof Error ? `(failed to load logs: ${err.message})` : String(err)))
  }

  return (
    <div>
      <PageHeader title="Docker" description="Containers, grouped by Compose project, running on this machine's Docker daemon." />

      {state === 'loading' && <div className="text-sm text-slate-400">Loading containers…</div>}

      {state === 'unavailable' && (
        <EmptyState
          icon={<Container size={28} />}
          title="Docker is not available"
          description="VoltPanel couldn't reach a local Docker daemon. Install/start Docker (or Docker Desktop, Colima, etc.) and retry."
          action={<Button onClick={load}>Retry</Button>}
        />
      )}

      {state === 'error' && (
        <EmptyState
          icon={<Container size={28} />}
          title="Failed to load Docker data"
          description={errorMessage}
          action={<Button onClick={load}>Retry</Button>}
        />
      )}

      {state === 'ready' && (
        <div className="space-y-4">
          {groups.every(g => g.containers.length === 0) && (
            <EmptyState icon={<Box size={28} />} title="No containers found" description="Nothing running or stopped locally yet." />
          )}

          {groups.filter(g => g.containers.length > 0).map((g) => (
            <Card key={g.name || '(standalone)'} title={g.name ? `Compose project: ${g.name}` : 'Standalone containers'} bodyClassName="p-0">
              <Table>
                <thead><tr><Th>Name</Th><Th>Image</Th><Th>State</Th><Th>Status</Th><Th>Ports</Th><Th /></tr></thead>
                <tbody>
                  {g.containers.map((c: DockerContainer) => (
                    <tr key={c.id}>
                      <Td className="font-medium text-slate-900">{c.names[0] || c.id.slice(0, 12)}</Td>
                      <Td className="text-xs text-slate-500">{c.image}</Td>
                      <Td><Badge tone={stateTone[c.state] ?? 'neutral'}>{c.state}</Badge></Td>
                      <Td className="text-xs text-slate-500">{c.status}</Td>
                      <Td className="font-mono text-xs text-slate-500">
                        {(c.ports || []).filter(p => p.publicPort).map(p => `${p.publicPort}->${p.privatePort}/${p.type}`).join(', ') || '—'}
                      </Td>
                      <Td className="text-right space-x-1 whitespace-nowrap">
                        <Button size="sm" disabled={busyId === c.id} onClick={() => runAction(c.id, 'start')}><Play size={12} /></Button>
                        <Button size="sm" disabled={busyId === c.id} onClick={() => runAction(c.id, 'stop')}><Square size={12} /></Button>
                        <Button size="sm" disabled={busyId === c.id} onClick={() => runAction(c.id, 'restart')}><RotateCw size={12} /></Button>
                        <Button size="sm" onClick={() => showLogs(c.id)}><ScrollText size={12} /></Button>
                      </Td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            </Card>
          ))}

          {selected && (
            <Card title={`Logs — ${selected.slice(0, 12)}`}>
              <pre className="volt-scroll max-h-[40vh] overflow-auto whitespace-pre-wrap rounded-md bg-slate-950 p-3 text-xs text-slate-200">{logs}</pre>
            </Card>
          )}
        </div>
      )}
    </div>
  )
}
