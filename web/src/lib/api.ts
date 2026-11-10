import type {
  Device,
  Health,
  InboundRequest,
  LANPeer,
  ReceivedFile,
  SessionInfo,
  Settings,
} from '../types'

const JSON_HEADERS = { 'Content-Type': 'application/json' }

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init)
  if (!res.ok) {
    const body = await res.text().catch(() => '')
    let msg = `${res.status} ${res.statusText}`
    try {
      const parsed = JSON.parse(body) as { error?: string }
      if (parsed.error) msg = parsed.error
    } catch {
      if (body) msg += ` — ${body.slice(0, 160)}`
    }
    throw new Error(msg)
  }
  return (await res.json()) as T
}

const post = <T>(path: string, body: unknown = {}) =>
  req<T>(path, { method: 'POST', headers: JSON_HEADERS, body: JSON.stringify(body) })

export const api = {
  health: () => req<Health>('/api/health'),

  /* ------------------------------------------------------------- session */

  /** Creates a pickup session. `base` points at the relay for cross-network transfers. */
  createSession: (base = '') =>
    req<{ id: string; code: string; expiresAt: number }>(`${base}/api/session`, {
      method: 'POST',
      headers: JSON_HEADERS,
      body: '{}',
    }),

  sessionById: (id: string, base = '') => req<SessionInfo>(`${base}/api/session/${id}`),

  sessionByCode: (code: string, base = '') => req<SessionInfo>(`${base}/api/session/code/${code}`),

  destroySession: (id: string) => req<{ ok: boolean }>(`/api/session/${id}`, { method: 'DELETE' }),

  /* ------------------------------------------------------------ devices */

  lanPeers: () => req<{ devices: LANPeer[] }>('/api/lan/devices'),

  lanScan: () => req<{ devices: LANPeer[] }>('/api/lan/scan'),

  knownDevices: () => req<{ devices: Device[] }>('/api/devices'),

  renameDevice: (fingerprint: string, alias: string) =>
    post<{ ok: boolean; device: Device; displayName: string }>('/api/devices/rename', {
      fingerprint,
      alias,
    }),

  /* ------------------------------------------------------------- receive */

  pending: () => req<{ pending: InboundRequest[] }>('/api/lan/pending'),

  received: () => req<{ files: ReceivedFile[] }>('/api/lan/received'),

  decide: (id: string, accept: boolean, savePath = '', reason = '') =>
    post<{ status: string; token?: string; error?: string }>('/api/lan/accept', {
      id,
      accept,
      reason,
      savePath,
    }),

  inboxState: (base: string, id: string) =>
    req<{ status: string; token?: string }>(`${base}/api/lan/inbox/${id}`),

  inquire: (base: string, payload: Record<string, unknown>) =>
    post<{ id: string; status: string }>(`${base}/api/lan/inquiry`, payload),

  pair: (fingerprint: string, alias: string) => post<{ ok: boolean }>('/api/pair', { fingerprint, alias }),

  /* ------------------------------------------------------------ settings */

  settings: () => req<Settings>('/api/settings'),

  saveSettings: (downloadDir: string) => post<Settings>('/api/settings', { downloadDir }),

  pickFolder: (start = '') =>
    post<{ ok: boolean; path?: string; cancelled?: boolean; error?: string }>('/api/system/pick-folder', {
      start,
    }),

  firewallStatus: () => req<{ installed: boolean; supported: boolean }>('/api/firewall/status'),

  installFirewall: () => post<{ ok: boolean; error?: string }>('/api/firewall/install'),

  /* --------------------------------------------- host-side file actions */

  reveal: (path: string) => post<{ ok: boolean }>('/api/system/reveal', { path }),

  openFile: (path: string) => post<{ ok: boolean }>('/api/system/open', { path }),
}

/** XHR upload so we get real progress events, which fetch still lacks. */
export function uploadWithProgress(
  url: string,
  body: Blob,
  onProgress: (sent: number, total: number) => void,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('POST', url, true)
    xhr.withCredentials = false
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress(e.loaded, e.total)
    }
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) resolve()
      else reject(new Error(`上传失败：${xhr.status} ${xhr.responseText.slice(0, 160)}`))
    }
    xhr.onerror = () => reject(new Error('上传网络错误'))
    xhr.ontimeout = () => reject(new Error('上传超时'))
    xhr.send(body)
  })
}

export const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))
