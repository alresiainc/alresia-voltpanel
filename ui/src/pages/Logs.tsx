import React, { useEffect, useRef, useState } from 'react'

export default function Logs({ headers, token }: { headers: Record<string,string>, token: string }) {
  const [id, setId] = useState('')
  const [text, setText] = useState('')
  const wsRef = useRef<WebSocket | null>(null)

  const openWs = () => {
    wsRef.current?.close()
    const url = (location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/ws/events'
    const ws = new WebSocket(url)
    // Browsers can't set custom headers on a WS handshake, so auth goes
    // over the socket itself as the first message (see internal/ws).
    ws.onopen = () => { ws.send(JSON.stringify({ type: 'auth', token })) }
    ws.onmessage = (e) => {
      try { const data = JSON.parse(e.data); if (data.type === 'log' && (!id || data.id === id)) setText((t)=>t + data.data) } catch {}
    }
    wsRef.current = ws
  }

  useEffect(() => { openWs(); return () => wsRef.current?.close() }, [token])

  const load = () => fetch(`/logs/${encodeURIComponent(id)}?tail=true`, { headers }).then(r=>r.text()).then(setText)

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
