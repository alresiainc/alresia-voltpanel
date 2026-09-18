import React, { useState } from 'react'
import { api, FileEntry } from '../lib/api'

export default function Files() {
  const [path, setPath] = useState<string>('.')
  const [entries, setEntries] = useState<FileEntry[]>([])
  const [filePath, setFilePath] = useState('')
  const [content, setContent] = useState('')
  const [newDir, setNewDir] = useState('')
  const list = () => api.listFiles(path).then(setEntries).catch(() => {})
  const write = () => api.writeFile(filePath, content)
  const del = (p: string) => api.deleteFile(p).then(list)
  const mkdir = () => api.mkdir(newDir).then(() => { setNewDir(''); list() })
  return (
    <div className="grid gap-4 grid-cols-2">
      <div className="space-y-2">
        <div className="flex gap-2">
          <input value={path} onChange={e=>setPath(e.target.value)} className="border px-2 w-full" />
          <button onClick={list} className="border px-2">List</button>
        </div>
        <ul className="text-sm border p-2 max-h-96 overflow-auto">
          {entries.map((e,i)=>(<li key={i} className="flex justify-between border-b">
            <span>{e.isDir ? '📁 ' : ''}{e.name}</span>
            <button onClick={()=>del(e.path)} className="text-red-600">Delete</button>
          </li>))}
        </ul>
        <div className="flex gap-2">
          <input placeholder="new folder path" value={newDir} onChange={e=>setNewDir(e.target.value)} className="border px-2 w-full" />
          <button onClick={mkdir} className="border px-2">New folder</button>
        </div>
      </div>
      <div className="space-y-2">
        <input placeholder="/path/to/file" value={filePath} onChange={e=>setFilePath(e.target.value)} className="border px-2 w-full" />
        <textarea placeholder="content" value={content} onChange={e=>setContent(e.target.value)} className="border px-2 w-full h-64" />
        <button onClick={write} className="border px-3">Write</button>
      </div>
    </div>
  )
}
