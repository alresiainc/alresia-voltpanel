import React, { useState } from 'react'
import { Menu, KeyRound } from 'lucide-react'
import { StatusDot } from './ui'

export default function Topbar({
  pageLabel,
  onMenuClick,
  token,
  onTokenChange,
  connected,
}: {
  pageLabel: string
  onMenuClick: () => void
  token: string
  onTokenChange: (t: string) => void
  connected: boolean | null
}) {
  const [showToken, setShowToken] = useState(false)

  return (
    <header className="sticky top-0 z-30 flex items-center gap-3 border-b border-slate-200 bg-white/80 px-4 py-3 backdrop-blur">
      <button
        onClick={onMenuClick}
        className="rounded-md p-1.5 text-slate-500 hover:bg-slate-100 md:hidden"
        aria-label="Open navigation"
      >
        <Menu size={20} />
      </button>

      <div className="flex min-w-0 flex-1 items-center gap-2 text-sm text-slate-500">
        <span className="hidden text-slate-400 sm:inline">Volt</span>
        <span className="hidden text-slate-300 sm:inline">/</span>
        <span className="truncate font-medium text-slate-800">{pageLabel}</span>
      </div>

      <div className="flex items-center gap-3">
        <StatusDot
          tone={connected === null ? 'neutral' : connected ? 'success' : 'error'}
          label={
            <span className="hidden sm:inline">
              {connected === null ? 'Checking daemon…' : connected ? 'Local daemon connected' : 'Daemon unreachable'}
            </span>
          }
        />

        <div className="relative">
          <button
            onClick={() => setShowToken((v) => !v)}
            title="Session token"
            className="flex items-center gap-1.5 rounded-md border border-slate-200 px-2.5 py-1.5 text-xs font-medium text-slate-600 hover:bg-slate-50"
          >
            <KeyRound size={14} />
            <span className="hidden sm:inline">{token ? 'Token set' : 'Set token'}</span>
          </button>
          {showToken && (
            <div className="absolute right-0 z-40 mt-2 w-72 rounded-lg border border-slate-200 bg-white p-3 shadow-lg">
              <label className="mb-1 block text-xs font-medium text-slate-500">Daemon session token</label>
              <input
                autoFocus
                value={token}
                onChange={(e) => onTokenChange(e.target.value)}
                placeholder="paste token from ~/.volt/config.json"
                className="w-full rounded-md border border-slate-200 px-2 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-volt-500/40"
              />
              <p className="mt-1.5 text-[11px] text-slate-400">
                Printed on first run, stored in <code>~/.volt/config.json</code>.
              </p>
            </div>
          )}
        </div>
      </div>
    </header>
  )
}
