import React, { useEffect, useState } from 'react'

export default function Processes({ headers }: { headers: Record<string,string>}) {
  const [procs, setProcs] = useState<any[]>([])
  const [form, setForm] = useState({ id:'', name:'', command:'', args:'', cwd:'' })
  const load = () => fetch('/processes', { headers }).then(r=>r.json()).then(setProcs)
  useEffect(() => { load() }, [])
  return (
    <div className="space-y-4">
      <div className="border p-3 space-y-2">
        <div className="flex gap-2">
          <input placeholder="id" value={form.id} onChange={e=>setForm({...form, id:e.target.value})} className="border px-2" />
          <input placeholder="name" value={form.name} onChange={e=>setForm({...form, name:e.target.value})} className="border px-2" />
          <input placeholder="command" value={form.command} onChange={e=>setForm({...form, command:e.target.value})} className="border px-2" />
          <input placeholder="args (space sep)" value={form.args} onChange={e=>setForm({...form, args:e.target.value})} className="border px-2" />
          <input placeholder="cwd" value={form.cwd} onChange={e=>setForm({...form, cwd:e.target.value})} className="border px-2 w-72" />
          <button onClick={()=>{
            fetch('/services/start', { method:'POST', headers: { ...headers, 'Content-Type':'application/json' }, body: JSON.stringify({ id: form.id, name: form.name, command: form.command, args: form.args?form.args.split(' '):[], cwd: form.cwd }) }).then(load)
          }} className="border px-3">Start</button>
        </div>
      </div>
      <table className="w-full text-sm">
        <thead><tr><th className="text-left">ID</th><th>Name</th><th>PID</th><th>Status</th><th></th></tr></thead>
        <tbody>
          {procs.map(p=> (
            <tr key={p.id} className="border-b">
              <td>{p.id}</td>
              <td>{p.name}</td>
              <td>{p.pid}</td>
              <td>{p.status}</td>
              <td className="text-right">
                <button onClick={()=>fetch('/services/stop', { method:'POST', headers:{...headers, 'Content-Type':'application/json'}, body: JSON.stringify({ id: p.id }) }).then(load)} className="border px-2">Stop</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
