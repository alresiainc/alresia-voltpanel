import React, { useEffect, useState } from 'react'
import { Cpu, MemoryStick, HardDrive, Activity, FolderPlus, Globe, Server as ServerIcon, Rocket, Network } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { api, Metrics, Service } from '../lib/api'
import { Card, PageHeader, Button, Meter, Skeleton, EmptyState, Tone } from '../components/ui'
import type { Tab } from '../App'

function bytesToGB(b: number) {
  return (b / 1024 / 1024 / 1024).toFixed(1)
}

function usageTone(percent: number): Tone {
  if (percent >= 90) return 'error'
  if (percent >= 70) return 'warning'
  return 'success'
}

export default function Dashboard({ onNavigate }: { onNavigate: (t: Tab) => void }) {
  const [metrics, setMetrics] = useState<Metrics | null>(null)
  const [services, setServices] = useState<Service[] | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    api.systemMetrics().then(setMetrics).catch((e) => setError(e.message || 'failed to load metrics'))
    api.listServices().then(setServices).catch(() => setServices([]))
  }, [])

  const runningCount = services?.filter((s) => s.status === 'running').length ?? null

  const quickActions: { label: string; icon: LucideIcon; onClick: () => void }[] = [
    { label: 'New Project', icon: FolderPlus, onClick: () => onNavigate('projects') },
    { label: 'Add Domain', icon: Globe, onClick: () => onNavigate('domains') },
    { label: 'Connect Server', icon: ServerIcon, onClick: () => onNavigate('servers') },
    { label: 'Deploy Project', icon: Rocket, onClick: () => onNavigate('deployments') },
  ]

  return (
    <div>
      <PageHeader
        eyebrow="Local development environment"
        title="Good to see you back."
        description="Manage your projects, runtimes, services and deployments from one control plane."
        actions={
          <>
            <Button variant="secondary" onClick={() => onNavigate('projects')}>New Project</Button>
            <Button variant="primary" onClick={() => onNavigate('runtimes')}>Quick Action</Button>
          </>
        }
      />

      {error && <div className="mb-4 text-sm text-red-600">{error}</div>}

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Card>
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium uppercase tracking-wide text-slate-400">CPU</span>
            <Cpu size={16} className="text-slate-400" />
          </div>
          {metrics ? (
            <>
              <div className="mt-2 text-2xl font-semibold text-slate-900">{metrics.cpuPercent?.toFixed?.(0) ?? '—'}%</div>
              <div className="mt-3"><Meter percent={metrics.cpuPercent ?? 0} tone={usageTone(metrics.cpuPercent ?? 0)} /></div>
            </>
          ) : (
            <Skeleton className="mt-3 h-8 w-20" />
          )}
        </Card>

        <Card>
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium uppercase tracking-wide text-slate-400">Memory</span>
            <MemoryStick size={16} className="text-slate-400" />
          </div>
          {metrics ? (
            <>
              <div className="mt-2 text-2xl font-semibold text-slate-900">{bytesToGB(metrics.memUsed)} GB</div>
              <div className="mt-1 text-xs text-slate-400">of {bytesToGB(metrics.memTotal)} GB</div>
              <div className="mt-2">
                <Meter percent={(metrics.memUsed / metrics.memTotal) * 100} tone={usageTone((metrics.memUsed / metrics.memTotal) * 100)} />
              </div>
            </>
          ) : (
            <Skeleton className="mt-3 h-8 w-20" />
          )}
        </Card>

        <Card>
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium uppercase tracking-wide text-slate-400">Storage</span>
            <HardDrive size={16} className="text-slate-400" />
          </div>
          {metrics ? (
            <>
              <div className="mt-2 text-2xl font-semibold text-slate-900">{bytesToGB(metrics.diskUsed)} GB</div>
              <div className="mt-1 text-xs text-slate-400">of {bytesToGB(metrics.diskTotal)} GB</div>
              <div className="mt-2">
                <Meter percent={(metrics.diskUsed / metrics.diskTotal) * 100} tone={usageTone((metrics.diskUsed / metrics.diskTotal) * 100)} />
              </div>
            </>
          ) : (
            <Skeleton className="mt-3 h-8 w-20" />
          )}
        </Card>

        <Card>
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium uppercase tracking-wide text-slate-400">Services</span>
            <Activity size={16} className="text-slate-400" />
          </div>
          {services ? (
            <>
              <div className="mt-2 text-2xl font-semibold text-slate-900">{runningCount}</div>
              <div className="mt-1 text-xs text-slate-400">running of {services.length} total</div>
            </>
          ) : (
            <Skeleton className="mt-3 h-8 w-20" />
          )}
        </Card>
      </div>

      <div className="mt-6 grid grid-cols-1 gap-4 lg:grid-cols-3">
        <Card title="Quick Actions" className="lg:col-span-1">
          <div className="grid grid-cols-2 gap-2">
            {quickActions.map((a) => (
              <button
                key={a.label}
                onClick={a.onClick}
                className="flex flex-col items-start gap-2 rounded-lg border border-slate-100 p-3 text-left transition-colors hover:border-slate-200 hover:bg-slate-50"
              >
                <a.icon size={16} />
                <span className="text-xs font-medium text-slate-700">{a.label}</span>
              </button>
            ))}
          </div>
        </Card>

        <Card
          title="Machine Overview"
          description="Live signals VoltPanel's API exposes today"
          className="lg:col-span-1"
        >
          <div className="space-y-3 text-sm">
            <div className="flex items-center justify-between">
              <span className="flex items-center gap-2 text-slate-500"><Network size={14} /> Open local ports</span>
              <span className="font-mono text-xs text-slate-700">
                {metrics ? (metrics.openLocalPorts?.length ? metrics.openLocalPorts.join(', ') : '—') : <Skeleton className="h-4 w-16" />}
              </span>
            </div>
            <div className="rounded-md bg-slate-50 px-3 py-2 text-xs text-slate-400">
              Hostname, OS, architecture, uptime and Volt version aren't exposed by <code>/api/v1/system/metrics</code> yet
              — only resource usage and open ports are.
            </div>
          </div>
        </Card>

        <Card title="Recent Activity" className="lg:col-span-1">
          <EmptyState
            title="No activity feed yet"
            description="VoltPanel records actions internally, but doesn't expose an activity API for the dashboard to read from yet."
          />
        </Card>
      </div>
    </div>
  )
}
