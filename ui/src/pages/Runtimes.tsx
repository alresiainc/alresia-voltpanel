import React, { useEffect, useState } from 'react'
import { api, Runtime } from '../lib/api'

export default function Runtimes() {
  const [runtimes, setRuntimes] = useState<Runtime[]>([])
  const [loading, setLoading] = useState(false)
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
    setLoading(true)
    api.detectRuntime(kind).then(load).catch(e => setError(e.message)).finally(() => setLoading(false))
  }

  const setDefault = (kind: string, version: string) => {
    api.setDefaultRuntimeVersion(kind, version).then(load).catch(e => setError(e.message))
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <button onClick={load} className="border px-3 py-1" disabled={loading}>
          {loading ? 'Refreshing…' : 'Refresh all'}
        </button>
        {error && <span className="text-red-600 text-sm">{error}</span>}
      </div>

      {runtimes.length === 0 && !loading && (
        <div className="text-sm text-gray-500">No runtimes detected yet. Click "Refresh all" to scan this machine.</div>
      )}

      {runtimes.map(rt => (
        <div key={rt.id} className="border p-3 space-y-2">
          <div className="flex items-center justify-between">
            <div className="font-semibold">{rt.name} <span className="text-gray-500 text-sm">({rt.kind})</span></div>
            <button onClick={() => detect(rt.kind)} className="border px-2 py-1 text-sm">Detect</button>
          </div>
          <table className="w-full text-sm">
            <thead>
              <tr>
                <th className="text-left">Version</th>
                <th className="text-left">Install path</th>
                <th className="text-left">Status</th>
                <th className="text-left">Default</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {rt.versions.map(v => (
                <tr key={v.id} className="border-b">
                  <td>{v.version}</td>
                  <td className="text-gray-600">{v.installPath || '—'}</td>
                  <td>{v.status}</td>
                  <td>{v.isDefault ? 'Yes' : ''}</td>
                  <td className="text-right">
                    {!v.isDefault && (
                      <button onClick={() => setDefault(rt.kind, v.version)} className="border px-2 py-1">
                        Set default
                      </button>
                    )}
                  </td>
                </tr>
              ))}
              {rt.versions.length === 0 && (
                <tr><td colSpan={5} className="text-gray-500">No versions detected.</td></tr>
              )}
            </tbody>
          </table>
        </div>
      ))}
    </div>
  )
}
