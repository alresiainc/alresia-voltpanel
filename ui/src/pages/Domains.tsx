import React, { useEffect, useState } from 'react'
import { api, ApiError, CAInfo, Domain } from '../lib/api'

export default function Domains() {
  const [domains, setDomains] = useState<Domain[]>([])
  const [hostname, setHostname] = useState('')
  const [ca, setCa] = useState<CAInfo | null>(null)
  const [error, setError] = useState('')

  const load = () => api.listDomains().then(setDomains).catch(() => {})
  useEffect(() => { load() }, [])

  const add = () => {
    setError('')
    api.createDomain(hostname)
      .then(() => { setHostname(''); load() })
      .catch(e => setError(e instanceof ApiError ? e.message : 'failed to add domain'))
  }

  const remove = (id: string) => {
    if (!window.confirm('Remove this domain? This also removes its hosts-file entry.')) return
    api.deleteDomain(id).then(load).catch(e => setError(e instanceof ApiError ? e.message : 'failed to delete domain'))
  }

  const ensureCA = () => {
    api.ensureCA().then(setCa).catch(e => setError(e instanceof ApiError ? e.message : 'failed to create local CA'))
  }

  const issueCert = (id: string) => {
    api.issueCertificate(id).then(load).catch(e => setError(e instanceof ApiError ? e.message : 'failed to issue certificate'))
  }

  const trustCA = () => {
    if (!window.confirm(
      'This will add VoltPanel\'s local CA to your OS/browser trust store, so browsers stop warning about certificates it issues. ' +
      'This changes system trust settings on your machine. Continue?'
    )) return
    api.trustCA(true).then(() => setCa(c => c ? { ...c, trusted: true } : c)).catch(e => setError(e instanceof ApiError ? e.message : 'failed to trust CA'))
  }

  return (
    <div className="space-y-4">
      <div className="border p-3 space-y-2">
        <div className="flex gap-2">
          <input placeholder="myapp.test" value={hostname} onChange={e => setHostname(e.target.value)} className="border px-2 w-72" />
          <button onClick={add} className="border px-3">Add domain</button>
        </div>
        {error && <div className="text-red-600 text-sm">{error}</div>}
      </div>

      <div className="border p-3 space-y-2">
        <div className="font-medium">Local HTTPS</div>
        {!ca && <button onClick={ensureCA} className="border px-3">Ensure local CA</button>}
        {ca && (
          <div className="text-sm space-y-1">
            <div>CA: {ca.commonName} (expires {ca.notAfter})</div>
            <div>Trusted by this OS: {ca.trusted ? 'yes' : 'no'}</div>
            {!ca.trusted && <button onClick={trustCA} className="border px-3">Trust CA (modifies OS trust store)</button>}
          </div>
        )}
      </div>

      <table className="w-full text-sm">
        <thead>
          <tr>
            <th className="text-left">Hostname</th>
            <th>SSL</th>
            <th>Enabled</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {domains.map(d => (
            <tr key={d.id} className="border-b">
              <td>{d.hostname}</td>
              <td className="text-center">{d.sslEnabled ? 'yes' : 'no'}</td>
              <td className="text-center">{d.enabled ? 'yes' : 'no'}</td>
              <td className="text-right space-x-2">
                {!d.sslEnabled && <button onClick={() => issueCert(d.id)} className="border px-2">Issue cert</button>}
                <button onClick={() => remove(d.id)} className="border px-2">Remove</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
