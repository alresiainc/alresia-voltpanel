import React, { useEffect, useState } from 'react'
import { api, Metrics } from '../lib/api'

export default function Dashboard() {
  const [metrics, setMetrics] = useState<Metrics | null>(null)
  useEffect(() => {
    api.systemMetrics().then(setMetrics).catch(() => {})
  }, [])
  if (!metrics) return <div>Loading metrics...</div>
  return (
    <div className="grid gap-4 grid-cols-2">
      <div className="border p-3">CPU: {metrics.cpuPercent?.toFixed?.(1)}%</div>
      <div className="border p-3">Mem: {(metrics.memUsed/1024/1024).toFixed(0)} MB / {(metrics.memTotal/1024/1024).toFixed(0)} MB</div>
      <div className="border p-3">Disk: {(metrics.diskUsed/1024/1024/1024).toFixed(1)} GB / {(metrics.diskTotal/1024/1024/1024).toFixed(1)} GB</div>
      <div className="border p-3">Open Ports: {metrics.openLocalPorts?.join(', ')}</div>
    </div>
  )
}
