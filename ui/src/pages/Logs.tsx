import React, { useEffect, useState } from 'react'
import { Copy, ScrollText, Trash2 } from 'lucide-react'
import { api } from '../lib/api'
import { wsClient } from '../lib/ws'
import { Button, Card, Input, Label, PageHeader } from '../components/ui'

export default function Logs() {
  const [id, setId] = useState('')
  const [text, setText] = useState('')
  const [live, setLive] = useState(false)

  useEffect(() => {
    return wsClient.subscribe((data) => {
      if (data.type === 'log' && (!id || data.id === id)) {
        setText((t) => t + data.data)
        setLive(true)
      }
    })
  }, [id])

  const load = () => api.serviceLogs(id, true).then(setText).catch(() => {})

  return (
    <div>
      <PageHeader
        title="Logs"
        description="Tail a service's log file, and see live output as it streams over the daemon's WebSocket."
      />

      <Card
        title={
          <span className="flex items-center gap-2">
            <ScrollText size={15} className="text-slate-400" /> Log viewer
            {live && (
              <span className="flex items-center gap-1 text-xs font-normal text-emerald-600">
                <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-emerald-500" /> live
              </span>
            )}
          </span>
        }
        actions={
          <>
            <Button size="sm" onClick={() => navigator.clipboard?.writeText(text)}><Copy size={12} className="mr-1" /> Copy</Button>
            <Button size="sm" onClick={() => setText('')}><Trash2 size={12} className="mr-1" /> Clear</Button>
          </>
        }
        bodyClassName="p-0"
      >
        <div className="flex gap-2 border-b border-slate-100 p-3">
          <div className="flex-1">
            <Label>Service ID</Label>
            <Input className="w-full" placeholder="service id" value={id} onChange={e => setId(e.target.value)} />
          </div>
          <Button className="self-end" onClick={load}>Tail</Button>
        </div>
        <pre className="volt-scroll max-h-[60vh] overflow-auto whitespace-pre-wrap bg-slate-950 p-3 text-xs text-slate-200">{text || ' '}</pre>
      </Card>
    </div>
  )
}
