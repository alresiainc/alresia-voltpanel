import React from 'react'

export default function Settings({ headers }: { headers: Record<string,string>}) {
  return <div className="space-y-2">
    <div>Paste your token in the top bar to authenticate.</div>
    <div>To regenerate token, delete ~/.alresia-volt/config.json and restart voltpanel.</div>
  </div>
}
