import React from 'react'
import {
  Zap,
  LayoutDashboard,
  Boxes,
  FolderKanban,
  FolderOpen,
  Layers,
  Activity,
  Container,
  Database,
  Globe,
  GitBranch,
  Server as ServerIcon,
  Rocket,
  Workflow,
  ScrollText,
  Puzzle,
  Settings as SettingsIcon,
  X,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { cn } from './ui'
import type { Tab } from '../App'

interface NavItem {
  key: Tab
  label: string
  icon: LucideIcon
}

interface NavGroup {
  label: string
  items: NavItem[]
}

const GROUPS: NavGroup[] = [
  {
    label: 'Workspace',
    items: [
      { key: 'dash', label: 'Overview', icon: LayoutDashboard },
      { key: 'projects', label: 'Projects', icon: FolderKanban },
      { key: 'files', label: 'File Manager', icon: FolderOpen },
    ],
  },
  {
    label: 'Environment',
    items: [
      { key: 'software', label: 'Software', icon: Boxes },
      { key: 'runtimes', label: 'Runtimes', icon: Layers },
      { key: 'proc', label: 'Processes', icon: Activity },
      { key: 'docker', label: 'Docker', icon: Container },
      { key: 'databases', label: 'Databases', icon: Database },
      { key: 'domains', label: 'Domains & SSL', icon: Globe },
    ],
  },
  {
    label: 'Delivery',
    items: [
      { key: 'git', label: 'Git', icon: GitBranch },
      { key: 'deployments', label: 'Deployments', icon: Rocket },
      { key: 'pipelines', label: 'Pipelines', icon: Workflow },
      { key: 'servers', label: 'Servers', icon: ServerIcon },
    ],
  },
  {
    label: 'System',
    items: [
      { key: 'logs', label: 'Logs', icon: ScrollText },
      { key: 'extensions', label: 'Extensions', icon: Puzzle },
      { key: 'settings', label: 'Settings', icon: SettingsIcon },
    ],
  },
]

export function navLabel(tab: Tab): string {
  for (const g of GROUPS) {
    const found = g.items.find((i) => i.key === tab)
    if (found) return found.label
  }
  return ''
}

export default function Sidebar({
  tab,
  onSelect,
  open,
  onClose,
}: {
  tab: Tab
  onSelect: (t: Tab) => void
  open: boolean
  onClose: () => void
}) {
  return (
    <>
      {/* Mobile backdrop */}
      {open && (
        <div className="fixed inset-0 z-40 bg-slate-950/40 md:hidden" onClick={onClose} aria-hidden="true" />
      )}

      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-50 flex w-64 flex-col bg-slate-950 text-slate-300 transition-transform duration-200 md:sticky md:top-0 md:h-screen md:translate-x-0',
          open ? 'translate-x-0' : '-translate-x-full',
        )}
      >
        <div className="flex items-center justify-between gap-2 px-5 py-5">
          <div className="flex items-center gap-2">
            <span className="flex h-7 w-7 items-center justify-center rounded-md bg-volt-500 text-slate-950">
              <Zap size={16} strokeWidth={2.5} fill="currentColor" />
            </span>
            <span className="text-sm font-semibold tracking-wide text-white">VOLT</span>
          </div>
          <button
            onClick={onClose}
            className="rounded-md p-1 text-slate-400 hover:bg-slate-800 hover:text-white md:hidden"
            aria-label="Close navigation"
          >
            <X size={18} />
          </button>
        </div>

        <nav className="volt-scroll flex-1 overflow-y-auto px-3 pb-4">
          {GROUPS.map((group) => (
            <div key={group.label} className="mb-5">
              <div className="mb-1.5 px-2 text-[11px] font-semibold uppercase tracking-wider text-slate-500">
                {group.label}
              </div>
              <div className="space-y-0.5">
                {group.items.map((item) => {
                  const active = item.key === tab
                  const Icon = item.icon
                  return (
                    <button
                      key={item.key}
                      onClick={() => {
                        onSelect(item.key)
                        onClose()
                      }}
                      className={cn(
                        'flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-left text-sm font-medium transition-colors',
                        active
                          ? 'bg-slate-800/80 text-white'
                          : 'text-slate-400 hover:bg-slate-900 hover:text-slate-100',
                      )}
                    >
                      <span className={cn('h-4 w-0.5 rounded-full', active ? 'bg-volt-400' : 'bg-transparent')} />
                      <Icon size={16} className={active ? 'text-volt-400' : 'text-slate-500'} />
                      {item.label}
                    </button>
                  )
                })}
              </div>
            </div>
          ))}
        </nav>

        <div className="border-t border-slate-800 px-4 py-3 text-[11px] text-slate-500">
          Local daemon &middot; 127.0.0.1:7788
        </div>
      </aside>
    </>
  )
}
