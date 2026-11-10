export type Channel = 'lan' | 'p2p' | 'relay'

export interface Health {
  ok: boolean
  version: string
  alias: string
  platform: string
  device: string
  port: number
  announce: boolean
  firewall?: boolean
  relay?: string
  /** 默认接收目录 */
  download?: string
  /** 请求是否来自运行本机的那台电脑（决定能否打开文件夹） */
  local?: boolean
  /** 本机 LAN 地址：给别的设备用的下载地址必须基于它 */
  address?: string
}

/** A peer discovered on the local network via mDNS or the subnet scan. */
export interface LANPeer {
  instance: string
  host: string
  ipv4: string
  port: number
  fingerprint: string
  alias: string
  platform: string
  lastSeen: number
  source?: 'mdns' | 'scan' | 'known' | 'manual'
  /** 用户给这台设备起的名字，优先显示 */
  aliasOverride?: string
  online?: boolean
}

/** A device remembered by this node (registry), online or not. */
export interface Device {
  fingerprint: string
  alias: string
  aliasOverride?: string
  platform: string
  address?: string
  port?: number
  channel: string
  online: boolean
  paired: boolean
  lastSeen: number
}

export interface Settings {
  downloadDir: string
  defaultDir: string
  savedDir: string
  pickerSupported: boolean
  platform: string
}

export interface SessionInfo {
  id: string
  code: string
  expiresAt: number
  senderOnline: boolean
  receiverOnline: boolean
}

/** A file a remote peer wants to send us. */
export interface InboundRequest {
  id: string
  name: string
  size: number
  mime: string
  fingerprint: string
  alias: string
  channel: Channel
  createdAt: number
}

export interface ReceivedFile {
  name: string
  path: string
  size: number
  sha256: string
  from: string
  receivedAt: number
  fingerprint: string
}

export type TxState = 'queued' | 'active' | 'done' | 'error' | 'rejected'

export interface Transfer {
  id: string
  name: string
  size: number
  direction: 'send' | 'receive'
  state: TxState
  sent: number
  speed: number
  channel?: Channel
  peer?: string
  error?: string
}

export interface Toast {
  id: number
  text: string
  kind: 'info' | 'ok' | 'err'
}

/** 设备在界面上的显示名：备注 > 对方自称 > 指纹前 8 位 */
export function peerName(p: { alias?: string; aliasOverride?: string; fingerprint: string }): string {
  return p.aliasOverride?.trim() || p.alias?.trim() || p.fingerprint.slice(0, 8)
}

export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return '—'
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v >= 100 ? 0 : v >= 10 ? 1 : 2)} ${units[i]}`
}

export function formatSpeed(bps: number): string {
  if (!bps || bps <= 0) return '—'
  return `${formatBytes(bps)}/s`
}

/** 相对时间，用于“上次出现 3 分钟前” */
export function formatAgo(ts: number): string {
  if (!ts) return '—'
  const s = Math.max(0, Math.round((Date.now() - ts) / 1000))
  if (s < 60) return `${s} 秒前`
  if (s < 3600) return `${Math.round(s / 60)} 分钟前`
  if (s < 86400) return `${Math.round(s / 3600)} 小时前`
  return `${Math.round(s / 86400)} 天前`
}
