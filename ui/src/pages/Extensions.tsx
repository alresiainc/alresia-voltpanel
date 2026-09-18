import React, { useEffect, useState } from 'react'
import { Puzzle, Trash2 } from 'lucide-react'
import { api, ApiError, Extension } from '../lib/api'
import { Badge, Button, Card, EmptyState, ErrorNote, Input, PageHeader, Table, Td, Th } from '../components/ui'

export default function Extensions() {
  const [extensions, setExtensions] = useState<Extension[] | null>(null)
  const [path, setPath] = useState('')
  const [error, setError] = useState('')

  const load = () => api.listExtensions().then(setExtensions).catch(() => setExtensions([]))
  useEffect(() => { load() }, [])

  const install = () => {
    setError('')
    api.installExtension(path)
      .then(() => { setPath(''); load() })
      .catch(e => setError(e instanceof ApiError ? e.message : 'failed to install extension'))
  }

  const toggle = (ext: Extension) => {
    const action = ext.enabled ? api.disableExtension(ext.id) : api.enableExtension(ext.id)
    action.then(load).catch(e => setError(e instanceof ApiError ? e.message : 'failed to toggle extension'))
  }

  const remove = (id: string) => {
    if (!window.confirm('Remove this extension? It will be disabled first if currently enabled.')) return
    api.removeExtension(id).then(load).catch(e => setError(e instanceof ApiError ? e.message : 'failed to remove extension'))
  }

  return (
    <div>
      <PageHeader title="Extensions" description="External providers run as a separate process speaking a small, versioned protocol." />

      <Card title="Install from a local path" className="mb-4">
        <div className="flex gap-2">
          <Input className="w-96" placeholder="local path to extension directory" value={path} onChange={e => setPath(e.target.value)} />
          <Button variant="primary" onClick={install}>Install</Button>
        </div>
        {error && <div className="mt-2"><ErrorNote>{error}</ErrorNote></div>}
      </Card>

      {extensions?.length === 0 ? (
        <EmptyState icon={<Puzzle size={28} />} title="No extensions installed" description="Install one from a local path above." />
      ) : (
        <Card bodyClassName="p-0">
          <Table>
            <thead><tr><Th>Name</Th><Th>Version</Th><Th>Kind</Th><Th>Status</Th><Th /></tr></thead>
            <tbody>
              {extensions?.map(e => (
                <tr key={e.id}>
                  <Td className="font-medium text-slate-900">{e.name}</Td>
                  <Td className="text-xs text-slate-500">{e.version}</Td>
                  <Td className="text-xs text-slate-500">{e.kind}</Td>
                  <Td><Badge tone={e.enabled ? 'success' : 'neutral'}>{e.enabled ? 'Enabled' : 'Disabled'}</Badge></Td>
                  <Td className="text-right space-x-2 whitespace-nowrap">
                    <Button size="sm" onClick={() => toggle(e)}>{e.enabled ? 'Disable' : 'Enable'}</Button>
                    <Button size="sm" variant="danger" onClick={() => remove(e.id)}><Trash2 size={12} /></Button>
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        </Card>
      )}
    </div>
  )
}
