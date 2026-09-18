import React, { useEffect, useState } from 'react'
import { api, ApiError, ComposeProject, DockerContainer } from '../lib/api'

// Docker is unavailable until proven otherwise (matches the daemon's own
// default assumption -- see internal/api/v1/docker.go): most dev machines
// this daemon runs on won't have Docker running, so the empty/error state
// is the common case, not an edge case.
type LoadState = 'loading' | 'unavailable' | 'error' | 'ready'

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
      .then((g) => {
        setGroups(g)
        setState('ready')
      })
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
    call(id).then(load).catch((err: unknown) => {
      setErrorMessage(err instanceof Error ? err.message : String(err))
    }).finally(() => setBusyId(''))
  }

  const showLogs = (id: string) => {
    setSelected(id)
    api.dockerContainerLogs(id, 200).then(setLogs).catch((err: unknown) => {
      setLogs(err instanceof Error ? `(failed to load logs: ${err.message})` : String(err))
    })
  }

  if (state === 'loading') {
    return <div className="text-sm text-gray-500">Loading containers...</div>
  }

  if (state === 'unavailable') {
    return (
      <div className="border p-4 space-y-2">
        <div className="font-medium">Docker is not available</div>
        <div className="text-sm text-gray-500">
          VoltPanel couldn't reach a local Docker daemon. Install/start Docker (or Docker Desktop, Colima, etc.)
          and refresh this page.
        </div>
        <button onClick={load} className="border px-3 py-1 text-sm">Retry</button>
      </div>
    )
  }

  if (state === 'error') {
    return (
      <div className="border p-4 space-y-2">
        <div className="font-medium text-red-600">Failed to load Docker data</div>
        <div className="text-sm text-gray-500">{errorMessage}</div>
        <button onClick={load} className="border px-3 py-1 text-sm">Retry</button>
      </div>
    )
  }

  const totalContainers = groups.reduce((n, g) => n + g.containers.length, 0)

  return (
    <div className="space-y-4">
      {totalContainers === 0 && (
        <div className="text-sm text-gray-500">No containers found. Nothing running or stopped locally yet.</div>
      )}

      {groups.map((g) => (
        <div key={g.name || '(standalone)'} className="border">
          <div className="bg-gray-50 px-3 py-2 text-sm font-medium border-b">
            {g.name ? `Compose project: ${g.name}` : 'Standalone containers'}
          </div>
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b">
                <th className="text-left px-2 py-1">Name</th>
                <th className="text-left px-2 py-1">Image</th>
                <th className="text-left px-2 py-1">State</th>
                <th className="text-left px-2 py-1">Status</th>
                <th className="text-left px-2 py-1">Ports</th>
                <th className="px-2 py-1"></th>
              </tr>
            </thead>
            <tbody>
              {g.containers.map((c: DockerContainer) => (
                <tr key={c.id} className="border-b">
                  <td className="px-2 py-1">{c.names[0] || c.id.slice(0, 12)}</td>
                  <td className="px-2 py-1">{c.image}</td>
                  <td className="px-2 py-1">{c.state}</td>
                  <td className="px-2 py-1">{c.status}</td>
                  <td className="px-2 py-1">
                    {(c.ports || []).filter(p => p.publicPort).map(p => `${p.publicPort}->${p.privatePort}/${p.type}`).join(', ')}
                  </td>
                  <td className="px-2 py-1 text-right space-x-2 whitespace-nowrap">
                    <button disabled={busyId === c.id} onClick={() => runAction(c.id, 'start')} className="border px-2">Start</button>
                    <button disabled={busyId === c.id} onClick={() => runAction(c.id, 'stop')} className="border px-2">Stop</button>
                    <button disabled={busyId === c.id} onClick={() => runAction(c.id, 'restart')} className="border px-2">Restart</button>
                    <button onClick={() => showLogs(c.id)} className="border px-2">Logs</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ))}

      {selected && (
        <div className="space-y-1">
          <div className="text-sm font-medium">Logs: {selected}</div>
          <pre className="border p-2 max-h-[40vh] overflow-auto whitespace-pre-wrap text-xs">{logs}</pre>
        </div>
      )}
    </div>
  )
}
