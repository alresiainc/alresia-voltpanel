import React, { useEffect, useState } from 'react'
import { Download, GitBranch, GitFork, Trash2 } from 'lucide-react'
import { api, ApiError, GitRepo, Integration } from '../lib/api'
import { Button, Card, EmptyState, ErrorNote, Input, Label, PageHeader } from '../components/ui'

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
    <div>
      <PageHeader title="Git" description="Connect a GitHub account to browse and clone repositories." />

      <Card title="Connect an account" className="mb-4">
        <div className="flex flex-wrap items-end gap-3">
          <div>
            <Label>GitHub personal access token</Label>
            <Input type="password" className="w-80" value={token} onChange={e => setToken(e.target.value)} />
          </div>
          <Button variant="primary" onClick={connect}><GitFork size={14} className="mr-1.5" /> Connect GitHub</Button>
        </div>
        {error && <div className="mt-2"><ErrorNote>{error}</ErrorNote></div>}
      </Card>

      {integrations.length === 0 ? (
        <EmptyState icon={<GitFork size={28} />} title="No accounts connected" description="Connect a GitHub account above to browse and clone repositories." />
      ) : (
        <Card title="Connected accounts">
          <ul className="divide-y divide-slate-50 text-sm">
            {integrations.map(i => (
              <li key={i.id} className="flex items-center justify-between py-2">
                <span className="text-slate-700">{i.accountRef} <span className="text-slate-400">({i.kind})</span></span>
                <span className="flex gap-2">
                  <Button size="sm" onClick={() => browse(i.id)}><GitBranch size={12} className="mr-1" /> Browse repos</Button>
                  <Button size="sm" variant="danger" onClick={() => disconnect(i.id)}><Trash2 size={12} /></Button>
                </span>
              </li>
            ))}
          </ul>
        </Card>
      )}

      {activeId && (
        <Card title="Repositories" className="mt-4">
          <Input placeholder="destination path" value={dest} onChange={e => setDest(e.target.value)} className="mb-3 w-96" />
          {repos.length === 0 ? (
            <div className="text-sm text-slate-400">No repositories found.</div>
          ) : (
            <ul className="divide-y divide-slate-50 text-sm">
              {repos.map(r => (
                <li key={r.id} className="flex items-center justify-between py-1.5">
                  <span className="text-slate-700">{r.name}</span>
                  <Button size="sm" onClick={() => clone(r)}><Download size={12} className="mr-1" /> Clone</Button>
                </li>
              ))}
            </ul>
          )}
        </Card>
      )}
    </div>
  )
}
