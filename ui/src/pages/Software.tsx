import React, { useEffect, useRef, useState } from 'react'
import { Boxes, Clock, Download, History, Loader2, Play, RotateCw, Search, Square, Trash2, ArrowUpCircle, XCircle } from 'lucide-react'
import { api, ApiError, InstalledPackage, Job, PackageInfo } from '../lib/api'
import { wsClient } from '../lib/ws'
import { Badge, Button, Card, EmptyState, ErrorNote, Input, Modal, PageHeader, Table, Td, Th, Tone } from '../components/ui'

interface Catalog {
  label: string
  family: string
  versions?: string[]
}

// Node.js is deliberately not in this catalog: it has a real native
// installer (internal/providers/runtime/node downloads nodejs.org's own
// binaries directly, no Homebrew) wired into the Runtimes page instead.
// Listing it here too would mean two different "install Node" paths --
// one native, one still shelling to brew -- which is exactly the kind of
// confusing duplication this split is meant to avoid.
const CATALOG: Catalog[] = [
  { label: 'PHP', family: 'php', versions: ['8.1', '8.2', '8.3', '8.4'] },
  { label: 'MySQL', family: 'mysql' },
  { label: 'PostgreSQL', family: 'postgresql', versions: ['14', '15', '16', '17'] },
  { label: 'Redis', family: 'redis' },
  { label: 'Go', family: 'go' },
  { label: 'Nginx', family: 'nginx' },
  { label: 'Apache', family: 'httpd' },
]

const jobTone: Record<string, Tone> = { running: 'info', success: 'success', failed: 'error', canceled: 'neutral' }

function elapsed(startedAt: string): string {
  const ms = Date.now() - new Date(startedAt).getTime()
  const mins = Math.floor(ms / 60000)
  const secs = Math.floor((ms % 60000) / 1000)
  return mins > 0 ? `${mins}m ${secs}s` : `${secs}s`
}

// One version's inline status -- this replaces having to scroll to a
// separate "Jobs" list to find out whether an Install click did anything.
function VersionStatus({
  formula, installed, job, onInstall, onOpenJob,
}: {
  formula: string
  installed: InstalledPackage | undefined
  job: Job | undefined // most recent job for this exact formula, of any status
  onInstall: () => void
  onOpenJob: (id: string) => void
}) {
  if (job && job.status === 'running') {
    return (
      <button onClick={() => onOpenJob(job.id)} className="flex items-center gap-1.5 text-xs font-medium text-blue-600 hover:underline">
        <Loader2 size={13} className="animate-spin" /> {job.kind === 'install' ? 'Installing' : job.kind === 'upgrade' ? 'Upgrading' : 'Working'}… {elapsed(job.startedAt)}
      </button>
    )
  }
  if (installed) {
    return <Badge tone="success">{installed.versions[0] || 'installed'}</Badge>
  }
  if (job && job.status === 'failed') {
    return (
      <button onClick={() => onOpenJob(job.id)} className="flex items-center gap-1 text-xs font-medium text-red-600 hover:underline">
        <XCircle size={13} /> Failed
      </button>
    )
  }
  return <Button size="sm" onClick={onInstall}><Download size={12} className="mr-1" /> Install</Button>
}

function JobModal({ jobId, onClose, onChanged }: { jobId: string; onClose: () => void; onChanged: () => void }) {
  const [job, setJob] = useState<Job | null>(null)
  const [log, setLog] = useState('')
  const [error, setError] = useState('')

  const refresh = () => {
    api.getJob(jobId).then(setJob).catch(() => {})
    api.getJobLog(jobId).then(setLog).catch(() => {})
  }
  useEffect(() => { refresh() }, [jobId]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    return wsClient.subscribe(data => {
      if (data.type === 'log' && data.id === jobId) setLog(prev => prev + data.data)
    })
  }, [jobId])

  useEffect(() => {
    if (!job || job.status !== 'running') return
    const t = setInterval(refresh, 2000)
    return () => clearInterval(t)
  }, [job]) // eslint-disable-line react-hooks/exhaustive-deps

  const cancel = () => {
    setError('')
    api.cancelJob(jobId).then(() => { refresh(); onChanged() }).catch(e => setError(e instanceof ApiError ? e.message : 'failed to cancel'))
  }

  return (
    <Modal
      title={job ? `${job.kind} ${job.target}` : 'Job'}
      onClose={() => { onClose(); onChanged() }}
      actions={job?.status === 'running' ? <Button variant="danger" onClick={cancel}><XCircle size={14} className="mr-1.5" /> Cancel</Button> : undefined}
    >
      {job && (
        <div className="mb-3 flex items-center gap-3 text-sm">
          <Badge tone={jobTone[job.status] ?? 'neutral'}>{job.status}</Badge>
          <span className="flex items-center gap-1 text-xs text-slate-400"><Clock size={12} /> started {new Date(job.startedAt).toLocaleString()}</span>
        </div>
      )}
      {job?.error && <div className="mb-3"><ErrorNote>{job.error}</ErrorNote></div>}
      {error && <div className="mb-3"><ErrorNote>{error}</ErrorNote></div>}
      <pre className="volt-scroll max-h-96 overflow-auto whitespace-pre-wrap rounded-md bg-slate-950 p-3 text-xs text-slate-200">{log || '(no output yet)'}</pre>
    </Modal>
  )
}

function HistoryModal({ jobs, onClose, onSelect }: { jobs: Job[]; onClose: () => void; onSelect: (id: string) => void }) {
  return (
    <Modal title="Job history" onClose={onClose}>
      {jobs.length === 0 ? (
        <div className="text-sm text-slate-400">No installs/upgrades/uninstalls run yet.</div>
      ) : (
        <ul className="max-h-96 divide-y divide-slate-50 overflow-auto text-sm">
          {jobs.map(j => (
            <li key={j.id}>
              <button onClick={() => onSelect(j.id)} className="flex w-full items-center justify-between py-2 text-left hover:bg-slate-50">
                <span className="font-mono text-slate-700">{j.kind} {j.target}</span>
                <span className="flex items-center gap-2">
                  <Badge tone={jobTone[j.status] ?? 'neutral'}>{j.status}</Badge>
                  <span className="text-xs text-slate-400">{new Date(j.startedAt).toLocaleTimeString()}</span>
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </Modal>
  )
}

export default function Software() {
  const [installed, setInstalled] = useState<InstalledPackage[] | null>(null)
  const [unavailable, setUnavailable] = useState(false)
  const [jobs, setJobs] = useState<Job[]>([])
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<PackageInfo[] | null>(null)
  const [error, setError] = useState('')
  const [openJobId, setOpenJobId] = useState('')
  const [historyOpen, setHistoryOpen] = useState(false)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const load = () => {
    api.listInstalledPackages()
      .then(list => { setInstalled(list); setUnavailable(false) })
      .catch(e => { setInstalled([]); setUnavailable(e instanceof ApiError && e.status === 503) })
    api.listJobs().then(setJobs).catch(() => {})
  }
  useEffect(() => { load() }, [])

  // Poll while anything is running so tiles flip from "Installing…" to
  // their final state without needing a manual refresh.
  useEffect(() => {
    const hasRunning = jobs.some(j => j.status === 'running')
    if (pollRef.current) { clearInterval(pollRef.current); pollRef.current = null }
    if (hasRunning) pollRef.current = setInterval(() => api.listJobs().then(setJobs).catch(() => {}), 3000)
    return () => { if (pollRef.current) clearInterval(pollRef.current) }
  }, [jobs])

  const installedByName = (name: string) => installed?.find(p => p.name === name)
  const latestJobFor = (target: string) => jobs.find(j => j.target === target) // jobs is already most-recent-first

  const runInstall = (name: string) => {
    setError('')
    api.installPackage(name).then(j => { setOpenJobId(j.id); load() }).catch(e => setError(e instanceof ApiError ? e.message : `failed to install ${name}`))
  }
  const runUpgrade = (name: string) => {
    setError('')
    api.upgradePackage(name).then(j => { setOpenJobId(j.id); load() }).catch(e => setError(e instanceof ApiError ? e.message : `failed to upgrade ${name}`))
  }
  const runUninstall = (name: string) => {
    if (!window.confirm(`Uninstall ${name}? This removes it from your system via Homebrew.`)) return
    setError('')
    api.uninstallPackage(name).then(j => { setOpenJobId(j.id); load() }).catch(e => setError(e instanceof ApiError ? e.message : `failed to uninstall ${name}`))
  }
  const runService = (name: string, action: 'start' | 'stop' | 'restart') => {
    setError('')
    api.packageServiceAction(name, action).then(load).catch(e => setError(e instanceof ApiError ? e.message : `failed to ${action} ${name}`))
  }

  const [settingDefault, setSettingDefault] = useState('')
  const setDefault = (target: string) => {
    setError('')
    setSettingDefault(target)
    api.setDefaultPackageVersion(target)
      .then(() => load())
      .catch(e => setError(e instanceof ApiError ? e.message : `failed to switch to ${target}`))
      .finally(() => setSettingDefault(''))
  }

  const search = () => {
    if (!query) return
    setError('')
    api.searchPackages(query).then(setResults).catch(e => setError(e instanceof ApiError ? e.message : 'search failed'))
  }

  if (unavailable) {
    return (
      <div>
        <PageHeader title="Software" description="Install and manage PHP, MySQL, PostgreSQL, Redis, Go, and more. (Node.js has its own native installer on the Runtimes page.)" />
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
        description="Install fresh databases and services, switch versions, and manage them -- backed by Homebrew. Node.js has its own native installer on the Runtimes page instead."
        actions={<Button variant="secondary" onClick={() => setHistoryOpen(true)}><History size={14} className="mr-1.5" /> Job history</Button>}
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
                <VersionStatus formula={r.name} installed={installedByName(r.name)} job={latestJobFor(r.name)} onInstall={() => runInstall(r.name)} onOpenJob={setOpenJobId} />
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
                  const job = latestJobFor(formula)
                  return (
                    <div key={formula} className="flex items-center justify-between text-sm">
                      <span className="font-mono text-slate-600">{v || 'latest'}</span>
                      {pkg && entry.versions ? (
                        <span className="flex items-center gap-1.5">
                          <Badge tone="success">{pkg.versions[0] || 'installed'}</Badge>
                          <Button size="sm" disabled={settingDefault === formula} onClick={() => setDefault(formula)}>
                            {settingDefault === formula ? <Loader2 size={12} className="animate-spin" /> : 'Use'}
                          </Button>
                        </span>
                      ) : (
                        <VersionStatus formula={formula} installed={pkg} job={job} onInstall={() => runInstall(formula)} onOpenJob={setOpenJobId} />
                      )}
                    </div>
                  )
                })}
              </div>
            </Card>
          )
        })}
      </div>

      <Card title="Installed via Homebrew" bodyClassName="p-0">
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

      {openJobId && <JobModal jobId={openJobId} onClose={() => setOpenJobId('')} onChanged={load} />}
      {historyOpen && !openJobId && (
        <HistoryModal jobs={jobs} onClose={() => setHistoryOpen(false)} onSelect={(id) => { setHistoryOpen(false); setOpenJobId(id) }} />
      )}
    </div>
  )
}
