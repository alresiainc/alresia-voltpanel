import React, { useEffect, useState } from 'react'
import { api, ApiError, Project } from '../lib/api'

export default function Projects() {
  const [projects, setProjects] = useState<Project[]>([])
  const [form, setForm] = useState({ name: '', path: '' })
  const [error, setError] = useState('')

  const load = () => api.listProjects().then(setProjects).catch(() => {})
  useEffect(() => { load() }, [])

  const create = () => {
    setError('')
    api.createProject(form.name, form.path)
      .then(() => { setForm({ name: '', path: '' }); load() })
      .catch(e => setError(e instanceof ApiError ? e.message : 'failed to add project'))
  }

  const remove = (id: string) => {
    if (!window.confirm('Delete this project? This only removes VoltPanel\'s record of it, nothing on disk.')) return
    api.deleteProject(id).then(load).catch(e => setError(e instanceof ApiError ? e.message : 'failed to delete project'))
  }

  const redetect = (id: string) => {
    api.detectProject(id).then(load).catch(e => setError(e instanceof ApiError ? e.message : 'failed to detect framework'))
  }

  return (
    <div className="space-y-4">
      <div className="border p-3 space-y-2">
        <div className="flex gap-2">
          <input placeholder="name" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} className="border px-2" />
          <input placeholder="absolute path" value={form.path} onChange={e => setForm({ ...form, path: e.target.value })} className="border px-2 w-96" />
          <button onClick={create} className="border px-3">Add project</button>
        </div>
        {error && <div className="text-red-600 text-sm">{error}</div>}
      </div>
      <table className="w-full text-sm">
        <thead>
          <tr>
            <th className="text-left">Name</th>
            <th className="text-left">Path</th>
            <th>Framework</th>
            <th className="text-left">Run command</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {projects.map(p => (
            <tr key={p.id} className="border-b">
              <td>{p.name}</td>
              <td>{p.path}</td>
              <td className="text-center">{p.detectedKind}</td>
              <td>{p.runCommand || <span className="text-gray-400">unknown</span>}</td>
              <td className="text-right space-x-2">
                <button onClick={() => redetect(p.id)} className="border px-2">Re-detect</button>
                <button onClick={() => remove(p.id)} className="border px-2">Delete</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
