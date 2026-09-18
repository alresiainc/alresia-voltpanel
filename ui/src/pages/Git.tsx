import React, { useEffect, useState } from 'react'
import { api, ApiError, GitRepo, Integration } from '../lib/api'

export default function Git() {
  const [integrations, setIntegrations] = useState<Integration[]>([])
  const [token, setToken] = useState('')
  const [error, setError] = useState('')
  const [activeId, setActiveId] = useState<string | null>(null)
  const [repos, setRepos] = useState<GitRepo[]>([])
  const [dest, setDest] = useState('')

  const load = () => api.listIntegrations().then(setIntegrations).catch(() => {})
  useEffect(() => { load() }, [])

  const connect = () => {
    setError('')
    api.createIntegration('github', token)
      .then(() => { setToken(''); load() })
      .catch(e => setError(e instanceof ApiError ? e.message : 'failed to connect account (check the token)'))
  }

  const disconnect = (id: string) => {
    api.deleteIntegration(id).then(load).catch(e => setError(e instanceof ApiError ? e.message : 'failed to disconnect'))
  }

  const browse = (id: string) => {
    setActiveId(id)
    api.listGitRepos(id).then(setRepos).catch(e => setError(e instanceof ApiError ? e.message : 'failed to list repos'))
  }

  const clone = (repo: GitRepo) => {
    if (!activeId || !dest) return
    api.cloneGitRepo(activeId, repo, dest)
      .then(() => alert(`Cloned ${repo.name} to ${dest}`))
      .catch(e => setError(e instanceof ApiError ? e.message : 'clone failed'))
  }

  return (
    <div className="space-y-4">
      <div className="border p-3 space-y-2">
        <div className="flex gap-2">
          <input placeholder="GitHub personal access token" type="password" value={token} onChange={e => setToken(e.target.value)} className="border px-2 w-96" />
          <button onClick={connect} className="border px-3">Connect GitHub</button>
        </div>
        {error && <div className="text-red-600 text-sm">{error}</div>}
      </div>

      <table className="w-full text-sm">
        <thead><tr><th className="text-left">Account</th><th></th></tr></thead>
        <tbody>
          {integrations.map(i => (
            <tr key={i.id} className="border-b">
              <td>{i.accountRef} ({i.kind})</td>
              <td className="text-right space-x-2">
                <button onClick={() => browse(i.id)} className="border px-2">Browse repos</button>
                <button onClick={() => disconnect(i.id)} className="border px-2">Disconnect</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {activeId && (
        <div className="border p-3 space-y-2">
          <div className="flex gap-2">
            <input placeholder="destination path" value={dest} onChange={e => setDest(e.target.value)} className="border px-2 w-96" />
          </div>
          <ul className="text-sm">
            {repos.map(r => (
              <li key={r.id} className="flex justify-between border-b py-1">
                <span>{r.name}</span>
                <button onClick={() => clone(r)} className="border px-2">Clone</button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}
