import React, { useState } from 'react'

export default function Files({ headers }: { headers: Record<string,string>}) {
  const [path, setPath] = useState<string>('/')
  const [entries, setEntries] = useState<any[]>([])
  const [filePath, setFilePath] = useState('')
  const [content, setContent] = useState('')
  const list = () => fetch(`/files/list?path=${encodeURIComponent(path)}`, { headers }).then(r=>r.json()).then(setEntries)
  const write = () => fetch('/files/write', { method:'POST', headers: { ...headers, 'Content-Type':'application/json' }, body: JSON.stringify({ path: filePath, content }) })
  const del = (p: string) => fetch(`/files?path=${encodeURIComponent(p)}`, { method:'DELETE', headers })
  return (
    <div className="grid gap-4 grid-cols-2">
      <div className="space-y-2">
        <div className="flex gap-2">
          <input value={path} onChange={e=>setPath(e.target.value)} className="border px-2 w-full" />
          <button onClick={list} className="border px-2">List</button>
        </div>
        <ul className="text-sm border p-2 max-h-96 overflow-auto">
          {entries.map((e:any,i:number)=>(<li key={i} className="flex justify-between border-b">
            <span>{e.name}</span>
            <button onClick={()=>del(e.path)} className="text-red-600">Delete</button>
          </li>))}
        </ul>
      </div>
      <div className="space-y-2">
        <input placeholder="/path/to/file" value={filePath} onChange={e=>setFilePath(e.target.value)} className="border px-2 w-full" />
        <textarea placeholder="content" value={content} onChange={e=>setContent(e.target.value)} className="border px-2 w-full h-64" />
        <button onClick={write} className="border px-3">Write</button>
      </div>
    </div>
  )
}
