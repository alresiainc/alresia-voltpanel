import React, { useEffect, useRef, useState } from 'react'
import { Copy, Download, File, FileCode, Folder, FolderPlus, Pencil, RefreshCw, Save, Search, Trash2, Upload } from 'lucide-react'
import { api, FileEntry } from '../lib/api'
import { Button, Card, EmptyState, Input, PageHeader, Table, Td, Textarea, Th } from '../components/ui'

const CODE_EXT = new Set(['ts', 'tsx', 'js', 'jsx', 'go', 'php', 'py', 'json', 'yml', 'yaml', 'css', 'html', 'sh', 'md', 'env', 'toml'])

function FileIcon({ entry }: { entry: FileEntry }) {
  if (entry.isDir) return <Folder size={15} className="text-amber-500" />
  const ext = entry.name.split('.').pop()?.toLowerCase() || ''
  if (CODE_EXT.has(ext)) return <FileCode size={15} className="text-slate-400" />
  return <File size={15} className="text-slate-400" />
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++ }
  return `${v.toFixed(1)} ${units[i]}`
}

function formatDate(iso: string): string {
  if (!iso || iso.startsWith('0001-01-01')) return '—'
  return new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}

function joinPath(dir: string, name: string): string {
  return dir === '.' || dir === '' ? name : `${dir.replace(/\/$/, '')}/${name}`
}

function Breadcrumbs({ path, onNavigate }: { path: string; onNavigate: (p: string) => void }) {
  const clean = path === '.' || path === '' ? [] : path.split('/').filter(Boolean)
  return (
    <div className="flex items-center gap-1 text-sm text-slate-500">
      <button onClick={() => onNavigate('.')} className="rounded px-1.5 py-0.5 hover:bg-slate-100 hover:text-slate-800">root</button>
      {clean.map((seg, i) => (
        <React.Fragment key={i}>
          <span className="text-slate-300">/</span>
          <button
            onClick={() => onNavigate((path.startsWith('/') ? '/' : '') + clean.slice(0, i + 1).join('/'))}
            className="rounded px-1.5 py-0.5 hover:bg-slate-100 hover:text-slate-800"
          >
            {seg}
          </button>
        </React.Fragment>
      ))}
    </div>
  )
}

export default function Files({ initialPath }: { initialPath?: string }) {
  const [path, setPath] = useState<string>(initialPath || '.')
  const [entries, setEntries] = useState<FileEntry[] | null>(null)
  const [filter, setFilter] = useState('')
  const [editingPath, setEditingPath] = useState('')
  const [content, setContent] = useState('')
  const [error, setError] = useState('')
  const [uploading, setUploading] = useState(false)
  const fileInput = useRef<HTMLInputElement>(null)

  const list = (p = path) => {
    setError('')
    api.listFiles(p).then(setEntries).catch(e => { setError(e.message || 'failed to list directory'); setEntries([]) })
  }
  useEffect(() => { list(path) }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const navigate = (p: string) => { setPath(p); setEditingPath(''); list(p) }

  const openFile = (p: string) => {
    setEditingPath(p)
    setContent('')
    api.readFile(p).then(setContent).catch(e => setError(e.message || 'failed to read file'))
  }

  const save = () => {
    api.writeFile(editingPath, content).then(() => list()).catch(e => setError(e.message || 'failed to write file'))
  }

  const del = (p: string, isDir: boolean) => {
    if (!window.confirm(`Delete ${isDir ? 'folder' : 'file'} "${p}"?`)) return
    api.deleteFile(p).then(() => { if (editingPath === p) setEditingPath(''); list() }).catch(e => setError(e.message || 'failed to delete'))
  }

  const rename = (entry: FileEntry) => {
    const name = window.prompt('New name', entry.name)
    if (!name || name === entry.name) return
    const dst = joinPath(path, name)
    api.moveFile(entry.path, dst).then(() => list()).catch(e => setError(e.message || 'failed to rename'))
  }

  const duplicate = (entry: FileEntry) => {
    if (entry.isDir) { setError('duplicating folders is not supported yet'); return }
    const name = window.prompt('Name for the copy', `${entry.name}.copy`)
    if (!name) return
    api.copyFile(entry.path, joinPath(path, name)).then(() => list()).catch(e => setError(e.message || 'failed to duplicate'))
  }

  const newFolder = () => {
    const name = window.prompt('Folder name')
    if (!name) return
    api.mkdir(joinPath(path, name)).then(() => list()).catch(e => setError(e.message || 'failed to create folder'))
  }

  const newFile = () => {
    const name = window.prompt('File name')
    if (!name) return
    const target = joinPath(path, name)
    api.writeFile(target, '').then(() => { list(); openFile(target) }).catch(e => setError(e.message || 'failed to create file'))
  }

  const handleUpload = (files: FileList | null) => {
    if (!files || files.length === 0) return
    setUploading(true)
    setError('')
    Promise.all(Array.from(files).map(f => api.uploadFile(joinPath(path, f.name), f)))
      .then(() => list())
      .catch(e => setError(e.message || 'upload failed'))
      .finally(() => setUploading(false))
  }

  const visible = entries?.filter(e => e.name.toLowerCase().includes(filter.toLowerCase())) ?? []

  return (
    <div>
      <PageHeader title="File Manager" description="Browse, edit, and manage files VoltPanel has access to on this machine." />

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card
          title={<Breadcrumbs path={path} onNavigate={navigate} />}
          bodyClassName="p-0"
          actions={
            <>
              <Button size="sm" onClick={newFile}><File size={13} className="mr-1" /> File</Button>
              <Button size="sm" onClick={newFolder}><FolderPlus size={13} className="mr-1" /> Folder</Button>
              <Button size="sm" disabled={uploading} onClick={() => fileInput.current?.click()}>
                <Upload size={13} className="mr-1" /> {uploading ? 'Uploading…' : 'Upload'}
              </Button>
              <input ref={fileInput} type="file" multiple className="hidden" onChange={e => handleUpload(e.target.files)} />
              <Button size="sm" onClick={() => list()}><RefreshCw size={13} /></Button>
            </>
          }
        >
          <div className="flex items-center gap-2 border-b border-slate-100 p-2.5">
            <Search size={14} className="text-slate-400" />
            <Input className="w-full border-none px-0 focus:ring-0" placeholder="Filter this folder…" value={filter} onChange={e => setFilter(e.target.value)} />
          </div>

          {entries === null ? (
            <div className="p-4 text-sm text-slate-400">Loading…</div>
          ) : visible.length === 0 ? (
            <div className="p-4"><EmptyState title={filter ? 'No matches' : 'Empty directory'} description={filter ? 'Nothing matches that filter.' : 'Nothing here yet.'} /></div>
          ) : (
            <div className="max-h-[32rem] overflow-auto">
              <Table>
                <thead><tr><Th>Name</Th><Th>Size</Th><Th>Modified</Th><Th>Permissions</Th><Th /></tr></thead>
                <tbody>
                  {visible.map((e, i) => (
                    <tr key={i} className="group">
                      <Td>
                        <button
                          onClick={() => e.isDir ? navigate(e.path) : openFile(e.path)}
                          className="flex min-w-0 items-center gap-2 text-left text-slate-700 hover:text-slate-950"
                        >
                          <FileIcon entry={e} />
                          <span className="truncate">{e.name}</span>
                        </button>
                      </Td>
                      <Td className="whitespace-nowrap text-xs text-slate-500">{e.isDir ? '—' : formatBytes(e.size)}</Td>
                      <Td className="whitespace-nowrap text-xs text-slate-500">{formatDate(e.modTime)}</Td>
                      <Td className="whitespace-nowrap font-mono text-xs text-slate-400">{e.mode || '—'}</Td>
                      <Td className="text-right whitespace-nowrap opacity-0 transition-opacity group-hover:opacity-100">
                        {!e.isDir && (
                          <a href={api.downloadUrl(e.path)} download className="mr-1 inline-flex rounded p-1.5 text-slate-400 hover:bg-slate-100 hover:text-slate-700">
                            <Download size={13} />
                          </a>
                        )}
                        <button onClick={() => rename(e)} className="mr-1 rounded p-1.5 text-slate-400 hover:bg-slate-100 hover:text-slate-700"><Pencil size={13} /></button>
                        {!e.isDir && <button onClick={() => duplicate(e)} className="mr-1 rounded p-1.5 text-slate-400 hover:bg-slate-100 hover:text-slate-700"><Copy size={13} /></button>}
                        <button onClick={() => del(e.path, e.isDir)} className="rounded p-1.5 text-slate-400 hover:bg-red-50 hover:text-red-500"><Trash2 size={13} /></button>
                      </Td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            </div>
          )}
        </Card>

        <Card
          title={editingPath ? <span className="font-mono text-sm">{editingPath}</span> : 'Editor'}
          description={!editingPath ? 'Click a file on the left to open it here.' : undefined}
          actions={editingPath ? <Button size="sm" variant="primary" onClick={save}><Save size={13} className="mr-1" /> Save</Button> : undefined}
        >
          {editingPath ? (
            <Textarea value={content} onChange={e => setContent(e.target.value)} className="h-96 w-full font-mono text-xs" />
          ) : (
            <div className="flex h-96 items-center justify-center text-sm text-slate-300">No file open</div>
          )}
        </Card>
      </div>

      {error && <div className="mt-4 text-sm text-red-600">{error}</div>}
    </div>
  )
}
