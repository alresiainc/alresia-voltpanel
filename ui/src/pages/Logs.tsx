import React, { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { wsClient } from '../lib/ws'

export default function Logs() {
  const [id, setId] = useState('')
  const [text, setText] = useState('')

  useEffect(() => {
    return wsClient.subscribe((data) => {
      if (data.type === 'log' && (!id || data.id === id)) setText((t) => t + data.data)
    })
  }, [id])

  const load = () => api.serviceLogs(id, true).then(setText).catch(() => {})

  return (
    <div className="space-y-2">
      <div className="flex gap-2">
        <input placeholder="service id" value={id} onChange={e=>setId(e.target.value)} className="border px-2" />
        <button onClick={load} className="border px-2">Tail</button>
      </div>
      <pre className="border p-2 max-h-[60vh] overflow-auto whitespace-pre-wrap">{text}</pre>
    </div>
  )
}
