import React from 'react'

export default function Settings() {
  return <div className="space-y-2">
    <div>Paste your token in the top bar to authenticate.</div>
    <div>To regenerate the token, delete ~/.volt/config.json and restart voltpanel.</div>
  </div>
}
