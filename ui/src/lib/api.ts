// Typed API client for VoltPanel's /api/v1/* routes. Centralizes the
// auth-header/session-cookie handling and error formatting that every page
// used to hand-roll around its own fetch() calls (§11 of the plan).

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

let currentToken = ''
export function setToken(t: string) {
  currentToken = t
}
export function getToken() {
  return currentToken
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = { ...(init.headers as Record<string, string> | undefined) }
  if (currentToken) headers['X-Volt-Token'] = currentToken
  const res = await fetch(`/api/v1${path}`, { ...init, headers, credentials: 'include' })
  if (!res.ok) {
    let message = res.statusText
    try {
      const body = await res.json()
      message = body.error || message
    } catch {
      /* non-JSON error body, fall back to statusText */
    }
    throw new ApiError(res.status, message)
  }
  const contentType = res.headers.get('content-type') || ''
  if (contentType.includes('application/json')) return res.json()
  return res.text() as unknown as T
}

export interface Service {
  id: string
  name: string
  command: string
  args: string[]
  cwd: string
  env: Record<string, string>
  pid: number
  status: string
  logFile: string
}

export interface FileEntry {
  name: string
  path: string
  isDir: boolean
}

export interface RuntimeVersion {
  id: string
  version: string
  installPath: string
  isDefault: boolean
  status: string
}

export interface Runtime {
  id: string
  kind: string
  name: string
  versions: RuntimeVersion[]
}

export interface Metrics {
  cpuPercent: number
  memUsed: number
  memTotal: number
  diskUsed: number
  diskTotal: number
  openLocalPorts: number[]
}

export interface Project {
  id: string
  name: string
  path: string
  runtimeId?: string
  runtimeVersion?: string
  detectedKind: string
  runCommand: string
  createdAt: string
  updatedAt: string
}

export interface DockerPort {
  ip?: string
  privatePort: number
  publicPort?: number
  type: string
}

export interface DockerContainer {
  id: string
  names: string[]
  image: string
  command: string
  state: string
  status: string
  labels: Record<string, string>
  ports: DockerPort[]
  project?: string
}

export interface DockerContainerDetail extends DockerContainer {
  env: string[]
  mounts: { type: string; source: string; destination: string; rw: boolean }[]
  restartCount: number
}

export interface DockerImage {
  id: string
  tags: string[]
  size: number
  created: number
}

export interface DockerVolume {
  name: string
  driver: string
  mountpoint: string
  labels: Record<string, string>
}

export interface DockerNetwork {
  id: string
  name: string
  driver: string
  scope: string
  labels: Record<string, string>
}

export interface ComposeProject {
  name: string
  containers: DockerContainer[]
}

export interface DockerExecResult {
  output: string
  exitCode: number
}

export interface Integration {
  id: string
  kind: string
  accountRef: string
  scopes: string[]
  createdAt: string
}

export interface GitRepo {
  id: string
  name: string
  cloneUrl: string
}

export interface GitBranch {
  name: string
  sha: string
}

export const api = {
  verifyToken: (token: string) =>
    request<{ ok: boolean }>('/auth/token/verify', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token }),
    }),

  listRuntimes: () => request<Runtime[]>('/runtimes'),
  detectRuntime: (kind: string) => request<Runtime>(`/runtimes/${encodeURIComponent(kind)}/detect`, { method: 'POST' }),
  setDefaultRuntimeVersion: (kind: string, version: string) =>
    request<Runtime>(`/runtimes/${encodeURIComponent(kind)}/versions/${encodeURIComponent(version)}/default`, { method: 'POST' }),

  listServices: () => request<Service[]>('/services'),
  startService: (id: string, body: { name: string; command: string; args?: string[]; cwd?: string; env?: Record<string, string> }) =>
    request(`/services/${encodeURIComponent(id)}/start`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
  stopService: (id: string) => request(`/services/${encodeURIComponent(id)}/stop`, { method: 'POST' }),
  restartService: (id: string) => request(`/services/${encodeURIComponent(id)}/restart`, { method: 'POST' }),
  serviceLogs: (id: string, tail = true) => request<string>(`/services/${encodeURIComponent(id)}/logs?tail=${tail}`),

  listFiles: (path: string) => request<FileEntry[]>(`/files?path=${encodeURIComponent(path)}`),
  writeFile: (path: string, content: string) =>
    request('/files', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ path, content }) }),
  deleteFile: (path: string) => request(`/files?path=${encodeURIComponent(path)}&confirm=true`, { method: 'DELETE' }),
  mkdir: (path: string) => request('/files/mkdir', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ path }) }),
  moveFile: (src: string, dst: string) => request('/files/move', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ src, dst }) }),
  copyFile: (src: string, dst: string) => request('/files/copy', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ src, dst }) }),

  systemMetrics: () => request<Metrics>('/system/metrics'),

  listProjects: () => request<Project[]>('/projects'),
  createProject: (name: string, path: string) =>
    request<Project>('/projects', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name, path }) }),
  getProject: (id: string) => request<Project>(`/projects/${encodeURIComponent(id)}`),
  deleteProject: (id: string) => request(`/projects/${encodeURIComponent(id)}?confirm=true`, { method: 'DELETE' }),
  detectProject: (id: string) => request<Project>(`/projects/${encodeURIComponent(id)}/detect`, { method: 'POST' }),

  listDockerContainers: () => request<DockerContainer[]>('/docker/containers'),
  listDockerContainersGrouped: () => request<ComposeProject[]>('/docker/containers?group=compose'),
  getDockerContainer: (id: string) => request<DockerContainerDetail>(`/docker/containers/${encodeURIComponent(id)}`),
  startDockerContainer: (id: string) => request(`/docker/containers/${encodeURIComponent(id)}/start`, { method: 'POST' }),
  stopDockerContainer: (id: string) => request(`/docker/containers/${encodeURIComponent(id)}/stop`, { method: 'POST' }),
  restartDockerContainer: (id: string) => request(`/docker/containers/${encodeURIComponent(id)}/restart`, { method: 'POST' }),
  dockerContainerLogs: (id: string, tail = 200) => request<string>(`/docker/containers/${encodeURIComponent(id)}/logs?tail=${tail}`),
  execDockerContainer: (id: string, cmd: string[]) =>
    request<DockerExecResult>(`/docker/containers/${encodeURIComponent(id)}/exec`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ cmd }),
    }),
  listDockerImages: () => request<DockerImage[]>('/docker/images'),
  listDockerVolumes: () => request<DockerVolume[]>('/docker/volumes'),
  listDockerNetworks: () => request<DockerNetwork[]>('/docker/networks'),

  listIntegrations: () => request<Integration[]>('/integrations'),
  createIntegration: (kind: string, token: string) =>
    request<Integration>('/integrations', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ kind, token }) }),
  deleteIntegration: (id: string) => request(`/integrations/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  listGitRepos: (integrationId: string) => request<GitRepo[]>(`/git/repos?integrationId=${encodeURIComponent(integrationId)}`),
  gitBranches: (integrationId: string, owner: string, repo: string) =>
    request<GitBranch[]>(`/git/repos/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}/branches?integrationId=${encodeURIComponent(integrationId)}`),
  cloneGitRepo: (integrationId: string, repo: GitRepo, dest: string) =>
    request('/git/clone', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ integrationId, repo, dest }) }),
}
