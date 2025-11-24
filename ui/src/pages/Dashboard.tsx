import React, { useEffect, useState } from 'react'

export default function Dashboard({ headers }: { headers: Record<string,string>}) {
  const [metrics, setMetrics] = useState<any>(null)
  useEffect(() => {
    fetch('/metrics', { headers }).then(r=>r.json()).then(setMetrics).catch(()=>{})
  }, [headers])
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
