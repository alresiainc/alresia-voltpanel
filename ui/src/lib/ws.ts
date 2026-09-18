// One shared WebSocket connection to /api/v1/ws with a subscribe/callback
// model, replacing every page owning its own private socket (§11). Today
// there's still just one broadcast topic ("log" events), but the listener
// registry here is what a future per-topic subscribe message plugs into
// without every consumer needing to change.

type Listener = (data: any) => void

class WsClient {
  private socket: WebSocket | null = null
  private listeners = new Set<Listener>()
  private token = ''

  connect(token: string) {
    this.token = token
    this.socket?.close()
    const url = (location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/api/v1/ws'
    const socket = new WebSocket(url)
    // Browsers can't set custom headers on a WS handshake. If a session
    // cookie already exists it authenticates the connection automatically
    // at handshake time; this first-message frame is the fallback for
    // clients (or dev flows) that only have the raw token.
    socket.onopen = () => socket.send(JSON.stringify({ type: 'auth', token: this.token }))
    socket.onmessage = (e) => {
      let data: any
      try {
        data = JSON.parse(e.data)
      } catch {
        return
      }
      this.listeners.forEach((l) => l(data))
    }
    this.socket = socket
  }

  subscribe(listener: Listener): () => void {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  close() {
    this.socket?.close()
    this.socket = null
  }
}

export const wsClient = new WsClient()
