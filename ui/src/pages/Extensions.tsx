import React, { useEffect, useState } from 'react'
import { api, ApiError, Extension } from '../lib/api'

export default function Extensions() {
  const [extensions, setExtensions] = useState<Extension[]>([])
  const [path, setPath] = useState('')
  const [error, setError] = useState('')

  const load = () => api.listExtensions().then(setExtensions).catch(() => {})
  useEffect(() => { load() }, [])

  const install = () => {
    setError('')
    api.installExtension(path)
      .then(() => { setPath(''); load() })
      .catch(e => setError(e instanceof ApiError ? e.message : 'failed to install extension'))
  }

  const toggle = (ext: Extension) => {
    const action = ext.enabled ? api.disableExtension(ext.id) : api.enableExtension(ext.id)
    action.then(load).catch(e => setError(e instanceof ApiError ? e.message : 'failed to toggle extension'))
  }

  const remove = (id: string) => {
    if (!window.confirm('Remove this extension? It will be disabled first if currently enabled.')) return
    api.removeExtension(id).then(load).catch(e => setError(e instanceof ApiError ? e.message : 'failed to remove extension'))
  }

  return (
    <div className="space-y-4">
      <div className="border p-3 space-y-2">
        <div className="flex gap-2">
          <input placeholder="local path to extension directory" value={path} onChange={e => setPath(e.target.value)} className="border px-2 w-96" />
          <button onClick={install} className="border px-3">Install from path</button>
        </div>
        {error && <div className="text-red-600 text-sm">{error}</div>}
      </div>
      <table className="w-full text-sm">
        <thead>
          <tr>
            <th className="text-left">Name</th>
            <th>Version</th>
            <th>Kind</th>
            <th>Enabled</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {extensions.map(e => (
            <tr key={e.id} className="border-b">
              <td>{e.name}</td>
              <td className="text-center">{e.version}</td>
              <td className="text-center">{e.kind}</td>
              <td className="text-center">{e.enabled ? 'yes' : 'no'}</td>
              <td className="text-right space-x-2">
                <button onClick={() => toggle(e)} className="border px-2">{e.enabled ? 'Disable' : 'Enable'}</button>
                <button onClick={() => remove(e.id)} className="border px-2">Remove</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
