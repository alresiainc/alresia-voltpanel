import React, { useEffect, useState } from 'react'
import { Boxes, Download, Play, RotateCw, Search, Square, Trash2, ArrowUpCircle } from 'lucide-react'
import { api, ApiError, InstalledPackage, Job, PackageInfo } from '../lib/api'
import { wsClient } from '../lib/ws'
import { Badge, Button, Card, EmptyState, ErrorNote, Input, PageHeader, Table, Td, Th, Tone } from '../components/ui'

interface Catalog {
  label: string
  family: string // brew formula, or the "@"-prefix family for versioned ones
  versions?: string[] // for versioned families (php, postgresql, node); omitted = single formula
}

const CATALOG: Catalog[] = [
  { label: 'PHP', family: 'php', versions: ['8.1', '8.2', '8.3', '8.4'] },
  { label: 'MySQL', family: 'mysql' },
  { label: 'PostgreSQL', family: 'postgresql', versions: ['14', '15', '16', '17'] },
  { label: 'Redis', family: 'redis' },
  { label: 'Node.js', family: 'node', versions: ['18', '20', '22'] },
  { label: 'Go', family: 'go' },
  { label: 'Nginx', family: 'nginx' },
  { label: 'Apache', family: 'httpd' },
]

const jobTone: Record<string, Tone> = { running: 'info', success: 'success', failed: 'error' }

export default function Software() {
  const [installed, setInstalled] = useState<InstalledPackage[] | null>(null)
  const [unavailable, setUnavailable] = useState(false)
  const [jobs, setJobs] = useState<Job[]>([])
  const [logs, setLogs] = useState<Record<string, string>>({})
  const [expandedJob, setExpandedJob] = useState('')
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<PackageInfo[] | null>(null)
  const [error, setError] = useState('')

  const load = () => {
    api.listInstalledPackages()
      .then(list => { setInstalled(list); setUnavailable(false) })
      .catch(e => { setInstalled([]); setUnavailable(e instanceof ApiError && e.status === 503) })
    api.listJobs().then(setJobs).catch(() => {})
  }
  useEffect(() => { load() }, [])

  // Poll job status while any job is still running -- WS only streams log
  // lines, not the status transition itself.
  useEffect(() => {
    if (!jobs.some(j => j.status === 'running')) return
    const t = setInterval(() => api.listJobs().then(setJobs).catch(() => {}), 2000)
    return () => clearInterval(t)
  }, [jobs])

  useEffect(() => {
    return wsClient.subscribe(data => {
      if (data.type === 'log' && jobs.some(j => j.id === data.id)) {
        setLogs(prev => ({ ...prev, [data.id]: (prev[data.id] || '') + data.data }))
      }
    })
  }, [jobs])

  const installedByName = (name: string) => installed?.find(p => p.name === name)

  const runInstall = (name: string) => {
    setError('')
    api.installPackage(name)
      .then(j => { setExpandedJob(j.id); load() })
      .catch(e => setError(e instanceof ApiError ? e.message : `failed to install ${name}`))
  }
  const runUpgrade = (name: string) => {
    setError('')
    api.upgradePackage(name).then(j => { setExpandedJob(j.id); load() }).catch(e => setError(e instanceof ApiError ? e.message : `failed to upgrade ${name}`))
  }
  const runUninstall = (name: string) => {
    if (!window.confirm(`Uninstall ${name}? This removes it from your system via Homebrew.`)) return
    setError('')
    api.uninstallPackage(name).then(j => { setExpandedJob(j.id); load() }).catch(e => setError(e instanceof ApiError ? e.message : `failed to uninstall ${name}`))
  }
  const runService = (name: string, action: 'start' | 'stop' | 'restart') => {
    setError('')
    api.packageServiceAction(name, action).then(load).catch(e => setError(e instanceof ApiError ? e.message : `failed to ${action} ${name}`))
  }
  const setDefault = (target: string) => {
    setError('')
    api.setDefaultPackageVersion(target).then(load).catch(e => setError(e instanceof ApiError ? e.message : `failed to switch to ${target}`))
  }

  const search = () => {
    if (!query) return
    setError('')
    api.searchPackages(query).then(setResults).catch(e => setError(e instanceof ApiError ? e.message : 'search failed'))
  }

  if (unavailable) {
    return (
      <div>
        <PageHeader title="Software" description="Install and manage PHP, MySQL, PostgreSQL, Redis, Node, Go, and more." />
        <EmptyState
          icon={<Boxes size={28} />}
          title="Homebrew isn't available"
          description="VoltPanel drives Homebrew to install and manage software on macOS. Install it from brew.sh, then reload this page."
        />
      </div>
    )
  }

  return (
    <div>
      <PageHeader
        title="Software"
        description="Install fresh runtimes and databases, switch versions, and manage services -- backed by Homebrew."
      />

      {error && <div className="mb-4"><ErrorNote>{error}</ErrorNote></div>}

      <Card title="Install anything" className="mb-4">
        <div className="flex gap-2">
          <Input className="flex-1" placeholder="search Homebrew formulae, e.g. sqlite, ffmpeg, imagemagick" value={query} onChange={e => setQuery(e.target.value)} onKeyDown={e => e.key === 'Enter' && search()} />
          <Button onClick={search}><Search size={14} className="mr-1.5" /> Search</Button>
        </div>
        {results && (
          <ul className="mt-3 max-h-56 divide-y divide-slate-50 overflow-auto text-sm">
            {results.length === 0 && <li className="py-2 text-slate-400">No matches.</li>}
            {results.map(r => (
              <li key={r.name} className="flex items-center justify-between py-1.5">
                <span className="font-mono text-slate-700">{r.name}</span>
                <Button size="sm" onClick={() => runInstall(r.name)}><Download size={12} className="mr-1" /> Install</Button>
              </li>
            ))}
          </ul>
        )}
      </Card>

      <div className="mb-4 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {CATALOG.map(entry => {
          const versions = entry.versions ?? ['']
          return (
            <Card key={entry.label} title={entry.label}>
              <div className="space-y-2">
                {versions.map(v => {
                  const formula = v ? `${entry.family}@${v}` : entry.family
                  const pkg = installedByName(formula) || (v === '' ? installedByName(entry.family) : undefined)
                  return (
                    <div key={formula} className="flex items-center justify-between text-sm">
                      <span className="font-mono text-slate-600">{v || 'latest'}</span>
                      {pkg ? (
                        <span className="flex items-center gap-1.5">
                          <Badge tone="success">{pkg.versions[0] || 'installed'}</Badge>
                          {entry.versions && <Button size="sm" onClick={() => setDefault(formula)}>Use</Button>}
                        </span>
                      ) : (
                        <Button size="sm" onClick={() => runInstall(formula)}><Download size={12} className="mr-1" /> Install</Button>
                      )}
                    </div>
                  )
                })}
              </div>
            </Card>
          )
        })}
      </div>

      <Card title="Installed via Homebrew" bodyClassName="p-0" className="mb-4">
        {installed === null ? (
          <div className="p-4 text-sm text-slate-400">Loading…</div>
        ) : installed.length === 0 ? (
          <div className="p-4"><EmptyState title="Nothing installed via Homebrew yet" description="Use the catalog above or search to install something." /></div>
        ) : (
          <Table>
            <thead><tr><Th>Name</Th><Th>Version(s)</Th><Th>Service</Th><Th /></tr></thead>
            <tbody>
              {installed.map(p => (
                <tr key={p.name}>
                  <Td className="font-medium text-slate-900">{p.name}</Td>
                  <Td className="font-mono text-xs text-slate-500">{p.versions.join(', ')}</Td>
                  <Td>{p.serviceStatus ? <Badge tone={p.serviceStatus === 'started' ? 'success' : 'neutral'}>{p.serviceStatus}</Badge> : <span className="text-slate-300">—</span>}</Td>
                  <Td className="text-right space-x-1 whitespace-nowrap">
                    {p.serviceStatus !== undefined && (
                      <>
                        <Button size="sm" onClick={() => runService(p.name, 'start')}><Play size={12} /></Button>
                        <Button size="sm" onClick={() => runService(p.name, 'stop')}><Square size={12} /></Button>
                        <Button size="sm" onClick={() => runService(p.name, 'restart')}><RotateCw size={12} /></Button>
                      </>
                    )}
                    <Button size="sm" onClick={() => runUpgrade(p.name)}><ArrowUpCircle size={12} className="mr-1" /> Upgrade</Button>
                    <Button size="sm" variant="danger" onClick={() => runUninstall(p.name)}><Trash2 size={12} /></Button>
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>

      <Card title="Jobs">
        {jobs.length === 0 ? (
          <div className="text-sm text-slate-400">No installs/upgrades/uninstalls run yet.</div>
        ) : (
          <ul className="divide-y divide-slate-50 text-sm">
            {jobs.map(j => (
              <li key={j.id} className="py-2">
                <button onClick={() => setExpandedJob(expandedJob === j.id ? '' : j.id)} className="flex w-full items-center justify-between text-left">
                  <span className="flex items-center gap-2">
                    <span className="font-mono text-slate-700">{j.kind} {j.target}</span>
                    <Badge tone={jobTone[j.status] ?? 'neutral'}>{j.status}</Badge>
                  </span>
                  <span className="text-xs text-slate-400">{new Date(j.startedAt).toLocaleTimeString()}</span>
                </button>
                {expandedJob === j.id && (
                  <pre className="volt-scroll mt-2 max-h-56 overflow-auto whitespace-pre-wrap rounded-md bg-slate-950 p-3 text-xs text-slate-200">
                    {logs[j.id] || '(no output yet)'}
                  </pre>
                )}
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  )
}
