import React from 'react'
import { KeyRound, Puzzle } from 'lucide-react'
import { Card, PageHeader } from '../components/ui'

export default function Settings() {
  return (
    <div>
      <PageHeader title="Settings" description="VoltPanel's daemon-level configuration." />

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card title={<span className="flex items-center gap-2"><KeyRound size={15} className="text-slate-400" /> Authentication</span>}>
          <div className="space-y-2 text-sm text-slate-600">
            <p>Set the session token from the topbar's token menu to authenticate this browser.</p>
            <p>
              To regenerate it, delete <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">~/.volt/config.json</code> and
              restart <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">voltpanel</code>.
            </p>
          </div>
        </Card>

        <Card title={<span className="flex items-center gap-2"><Puzzle size={15} className="text-slate-400" /> More settings</span>}>
          <p className="text-sm text-slate-400">
            Appearance, runtime defaults, storage, and network settings aren't configurable via the API yet.
            Extensions and Git integrations are managed from their own pages.
          </p>
        </Card>
      </div>
    </div>
  )
}
