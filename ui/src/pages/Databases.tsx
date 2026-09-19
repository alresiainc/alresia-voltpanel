import React, { useEffect, useState } from 'react'
import { ChevronLeft, ChevronRight, Database, Play, Plug, Plus, RefreshCw, Table as TableIcon, Trash2 } from 'lucide-react'
import { api, ApiError, DBConnection, DBQueryResult } from '../lib/api'
import { Badge, Button, Card, EmptyState, ErrorNote, Input, Label, PageHeader, Select, Table, Td, Textarea, Th } from '../components/ui'

function ResultGrid({ result }: { result: DBQueryResult }) {
  if (!result.columns || result.columns.length === 0) {
    return (
      <div className="text-sm text-slate-500">
        OK — {result.rowsAffected} row{result.rowsAffected === 1 ? '' : 's'} affected ({result.durationMs} ms).
      </div>
    )
  }
  return (
    <div>
      <div className="mb-2 text-xs text-slate-400">
        {result.rows.length} row{result.rows.length === 1 ? '' : 's'} · {result.durationMs} ms
        {result.truncated && <span className="ml-1 text-amber-600">(truncated at server limit)</span>}
      </div>
      <div className="max-h-[28rem] overflow-auto">
        <Table>
          <thead><tr>{result.columns.map(c => <Th key={c}>{c}</Th>)}</tr></thead>
          <tbody>
            {result.rows.map((row, i) => (
              <tr key={i}>
                {row.map((v, j) => (
                  <Td key={j} className="whitespace-nowrap font-mono text-xs">
                    {v === null ? <span className="italic text-slate-300">NULL</span> : typeof v === 'object' ? JSON.stringify(v) : String(v)}
                  </Td>
                ))}
              </tr>
            ))}
          </tbody>
        </Table>
      </div>
    </div>
  )
}

export default function Databases() {
  const [connections, setConnections] = useState<DBConnection[] | null>(null)
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState({ name: '', kind: 'mysql' as 'mysql' | 'postgres', host: '127.0.0.1', port: 3306, username: '', password: '', databaseName: '' })
  const [error, setError] = useState('')

  const [activeId, setActiveId] = useState('')
  const [databases, setDatabases] = useState<string[] | null>(null)
  const [database, setDatabase] = useState('')
  const [tables, setTables] = useState<string[] | null>(null)
  const [table, setTable] = useState('')
  const [offset, setOffset] = useState(0)
  const [browseResult, setBrowseResult] = useState<DBQueryResult | null>(null)
  const [sql, setSql] = useState('')
  const [queryResult, setQueryResult] = useState<DBQueryResult | null>(null)
  const [busy, setBusy] = useState(false)

  const load = () => api.listDBConnections().then(setConnections).catch(() => setConnections([]))
  useEffect(() => { load() }, [])

  const create = () => {
    setError('')
    api.createDBConnection({
      name: form.name, kind: form.kind, host: form.host, port: form.port, username: form.username,
      password: form.password || undefined, databaseName: form.databaseName || undefined,
    })
      .then(() => { setShowForm(false); setForm({ ...form, name: '', password: '' }); load() })
      .catch(e => setError(e instanceof ApiError ? e.message : 'failed to add connection'))
  }

  const remove = (id: string) => {
    if (!window.confirm('Remove this connection? Its stored password (if any) is deleted too.')) return
    api.deleteDBConnection(id).then(() => { if (activeId === id) selectConnection(''); load() }).catch(e => setError(e instanceof ApiError ? e.message : 'failed to remove connection'))
  }

  const test = (id: string) => {
    setError('')
    api.testDBConnection(id).then(() => alert('Connection OK')).catch(e => setError(e instanceof ApiError ? e.message : 'connection failed'))
  }

  const selectConnection = (id: string) => {
    setActiveId(id)
    setDatabases(null); setDatabase(''); setTables(null); setTable(''); setBrowseResult(null); setQueryResult(null); setSql('')
    if (!id) return
    api.listDatabases(id).then(setDatabases).catch(e => setError(e instanceof ApiError ? e.message : 'failed to list databases'))
  }

  const selectDatabase = (db: string) => {
    setDatabase(db)
    setTables(null); setTable(''); setBrowseResult(null)
    if (!db) return
    api.listTables(activeId, db).then(setTables).catch(e => setError(e instanceof ApiError ? e.message : 'failed to list tables'))
  }

  const browse = (t: string, o = 0) => {
    setTable(t); setOffset(o); setBusy(true); setError('')
    api.browseTable(activeId, database, t, o).then(setBrowseResult).catch(e => setError(e instanceof ApiError ? e.message : 'failed to browse table')).finally(() => setBusy(false))
  }

  const runSQL = () => {
    if (!sql.trim()) return
    setBusy(true); setError('')
    api.runQuery(activeId, database, sql).then(setQueryResult).catch(e => setError(e instanceof ApiError ? e.message : 'query failed')).finally(() => setBusy(false))
  }

  const activeConn = connections?.find(c => c.id === activeId)

  return (
    <div>
      <PageHeader
        title="Databases"
        description="Connect to a real MySQL or PostgreSQL server, browse tables, and run SQL."
        actions={<Button variant="primary" onClick={() => setShowForm(v => !v)}><Plus size={14} className="mr-1" /> Add Connection</Button>}
      />

      {error && <div className="mb-4"><ErrorNote>{error}</ErrorNote></div>}

      {showForm && (
        <Card title="Add a connection" className="mb-4">
          <div className="flex flex-wrap items-end gap-3">
            <div><Label>Name</Label><Input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} /></div>
            <div>
              <Label>Kind</Label>
              <Select value={form.kind} onChange={e => setForm({ ...form, kind: e.target.value as 'mysql' | 'postgres', port: e.target.value === 'postgres' ? 5432 : 3306 })}>
                <option value="mysql">MySQL</option>
                <option value="postgres">PostgreSQL</option>
              </Select>
            </div>
            <div><Label>Host</Label><Input value={form.host} onChange={e => setForm({ ...form, host: e.target.value })} /></div>
            <div><Label>Port</Label><Input type="number" className="w-24" value={form.port} onChange={e => setForm({ ...form, port: Number(e.target.value) })} /></div>
            <div><Label>Username</Label><Input value={form.username} onChange={e => setForm({ ...form, username: e.target.value })} /></div>
            <div><Label>Password</Label><Input type="password" value={form.password} onChange={e => setForm({ ...form, password: e.target.value })} /></div>
            <div><Label>Default database (optional)</Label><Input value={form.databaseName} onChange={e => setForm({ ...form, databaseName: e.target.value })} /></div>
            <Button variant="primary" onClick={create}>Save connection</Button>
          </div>
          <p className="mt-2 text-xs text-slate-400">The password is stored via your OS keychain (or an encrypted local fallback) — never as plaintext.</p>
        </Card>
      )}

      {connections?.length === 0 ? (
        <EmptyState icon={<Database size={28} />} title="No connections yet" description="Add a MySQL or PostgreSQL connection above to start browsing." />
      ) : (
        <div className="mb-4 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {connections?.map(c => (
            <Card key={c.id} className={activeId === c.id ? 'ring-2 ring-volt-400' : undefined}>
              <div className="flex items-start justify-between gap-2">
                <button className="text-left" onClick={() => selectConnection(c.id)}>
                  <div className="font-medium text-slate-900">{c.name}</div>
                  <div className="text-xs text-slate-500">{c.username}@{c.host}:{c.port}</div>
                </button>
                <Badge tone="info">{c.kind}</Badge>
              </div>
              <div className="mt-3 flex gap-2 border-t border-slate-100 pt-3">
                <Button size="sm" onClick={() => test(c.id)}><Plug size={12} className="mr-1" /> Test</Button>
                <Button size="sm" onClick={() => selectConnection(c.id)}><TableIcon size={12} className="mr-1" /> Browse</Button>
                <Button size="sm" variant="danger" className="ml-auto" onClick={() => remove(c.id)}><Trash2 size={13} /></Button>
              </div>
            </Card>
          ))}
        </div>
      )}

      {activeConn && (
        <Card title={`Browsing: ${activeConn.name}`} className="mb-4">
          <div className="flex flex-wrap items-end gap-3">
            <div>
              <Label>Database</Label>
              <Select value={database} onChange={e => selectDatabase(e.target.value)}>
                <option value="">Select…</option>
                {databases?.map(db => <option key={db} value={db}>{db}</option>)}
              </Select>
            </div>
            {database && (
              <div>
                <Label>Table</Label>
                <Select value={table} onChange={e => browse(e.target.value)}>
                  <option value="">Select…</option>
                  {tables?.map(t => <option key={t} value={t}>{t}</option>)}
                </Select>
              </div>
            )}
            {table && (
              <div className="flex gap-1">
                <Button size="sm" disabled={offset === 0} onClick={() => browse(table, Math.max(0, offset - 200))}><ChevronLeft size={13} /></Button>
                <Button size="sm" onClick={() => browse(table, offset)}><RefreshCw size={13} /></Button>
                <Button size="sm" disabled={!browseResult || browseResult.rows.length < 200} onClick={() => browse(table, offset + 200)}><ChevronRight size={13} /></Button>
              </div>
            )}
          </div>

          {database && !table && <div className="mt-3 text-sm text-slate-400">Pick a table to browse its rows.</div>}
          {busy && <div className="mt-3 text-sm text-slate-400">Loading…</div>}
          {browseResult && !busy && <div className="mt-3"><ResultGrid result={browseResult} /></div>}
        </Card>
      )}

      {activeConn && database && (
        <Card title="Run SQL" description={`Executes against ${activeConn.name} / ${database}`}>
          <Textarea value={sql} onChange={e => setSql(e.target.value)} placeholder="SELECT * FROM users LIMIT 10;" className="h-32 w-full font-mono text-sm" />
          <Button className="mt-2" variant="primary" onClick={runSQL} disabled={busy}><Play size={14} className="mr-1.5" /> Run</Button>
          {queryResult && <div className="mt-4"><ResultGrid result={queryResult} /></div>}
        </Card>
      )}
    </div>
  )
}
