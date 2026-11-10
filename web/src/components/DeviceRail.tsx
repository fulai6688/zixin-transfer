import { useRef, useState } from 'react'
import { formatAgo, peerName, type LANPeer } from '../types'
import { IconBolt, IconLink, IconPencil, IconRefresh, IconTrash } from './Icons'

interface Props {
  selfAlias: string
  selfFingerprint: string
  peers: LANPeer[]
  manual: LANPeer[]
  target: string | null
  scanning: boolean
  needFirewall: boolean
  onSelect: (fingerprint: string | null) => void
  onRefresh: () => void
  onScan: () => void
  onInstallFirewall: () => void
  onAddManual: (ip: string, port: number) => void
  onRemoveManual: (fingerprint: string) => void
  onRename: (fingerprint: string, alias: string) => void
  onOpenSettings: () => void
}

/** The tag tells the user how the device is reachable right now. */
function tagFor(p: LANPeer): string {
  if (p.source === 'manual') return 'MAN'
  return p.online === false ? 'OFF' : 'LAN'
}

export default function DeviceRail({
  selfAlias,
  selfFingerprint,
  peers,
  manual,
  target,
  scanning,
  needFirewall,
  onSelect,
  onRefresh,
  onScan,
  onInstallFirewall,
  onAddManual,
  onRemoveManual,
  onRename,
  onOpenSettings,
}: Props) {
  const [adding, setAdding] = useState(false)
  const [ip, setIp] = useState('')
  const [port, setPort] = useState('8080')
  const [editing, setEditing] = useState<string | null>(null)
  const [draft, setDraft] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  const submit = () => {
    const p = Number(port)
    if (!ip.trim() || !Number.isFinite(p) || p <= 0) return
    onAddManual(ip.trim(), p)
    setIp('')
    setAdding(false)
  }

  const startEdit = (p: LANPeer) => {
    setEditing(p.fingerprint)
    setDraft(p.aliasOverride ?? '')
    window.setTimeout(() => inputRef.current?.select(), 30)
  }

  const commitEdit = () => {
    if (editing) onRename(editing, draft.trim())
    setEditing(null)
  }

  const onlineCount = peers.filter((p) => p.online).length

  return (
    <aside className="rail">
      <div className="rail__scroll">
        <div className="section-title">本机</div>
        <div className="me">
          <div className="me__avatar">{(selfAlias || '?').slice(0, 1).toUpperCase()}</div>
          <div style={{ minWidth: 0, flex: 1 }}>
            <div className="me__name">{selfAlias}</div>
            <div className="me__meta">
              <span className="dot dot--live" />
              <span className="mono">{selfFingerprint.slice(0, 8)}</span>
            </div>
          </div>
          <button className="iconbtn" onClick={onOpenSettings} title="设置">
            ⚙
          </button>
        </div>

        <div className="section-title">
          设备
          <span style={{ flex: 1 }} />
          <span className="hint" style={{ fontSize: 11 }}>
            {onlineCount} 在线
          </span>
          <button className="iconbtn" onClick={onRefresh} title="刷新">
            <IconRefresh size={15} />
          </button>
          <button className="iconbtn" onClick={onScan} title="扫描局域网" disabled={scanning}>
            <IconBolt size={15} />
          </button>
        </div>

        {needFirewall && (
          <div className="card card--pad" style={{ marginTop: 8, padding: 12 }}>
            <div style={{ fontSize: 12.5, lineHeight: 1.6, color: 'var(--text-dim)' }}>
              Windows 防火墙默认拦截入站连接，其它设备无法发现本机。
            </div>
            <button className="btn btn--sm btn--primary" style={{ marginTop: 10 }} onClick={onInstallFirewall}>
              一键放行（需要管理员）
            </button>
          </div>
        )}

        {peers.length === 0 && manual.length === 0 && (
          <div style={{ padding: '10px 10px 4px', color: 'var(--text-mute)', fontSize: 12.5 }}>
            正在扫描局域网… 同一 WiFi 下打开 子欣互传 即可被发现。
            {scanning ? '（正在扫描…）' : ''}
          </div>
        )}

        {peers.map((p) => {
          const offline = p.online === false
          const name = peerName(p)
          return (
            <div
              key={p.fingerprint}
              className={`dev${target === p.fingerprint ? ' dev--active' : ''}${offline ? ' dev--offline' : ''}`}
              onClick={() => !offline && onSelect(target === p.fingerprint ? null : p.fingerprint)}
              title={offline ? '离线设备：等它重新上线后再发送' : `${p.ipv4}:${p.port}`}
            >
              <div className={`dev__avatar${offline ? '' : ' dev__avatar--live'}`}>
                {(name || '?').slice(0, 1).toUpperCase()}
              </div>
              <div className="dev__body">
                {editing === p.fingerprint ? (
                  <input
                    ref={inputRef}
                    className="field"
                    style={{ minHeight: 28, height: 28, padding: '0 8px', fontSize: 13 }}
                    value={draft}
                    placeholder="给这台设备起个名字"
                    onClick={(e) => e.stopPropagation()}
                    onChange={(e) => setDraft(e.target.value)}
                    onKeyDown={(e) => {
                      e.stopPropagation()
                      if (e.key === 'Enter') commitEdit()
                      if (e.key === 'Escape') setEditing(null)
                    }}
                    onBlur={commitEdit}
                    autoFocus
                  />
                ) : (
                  <div className="dev__name">{name}</div>
                )}
                <div className="dev__meta">
                  <span className={`tag ${offline ? 'tag--off' : 'tag--lan'}`}>{tagFor(p)}</span>
                  <span>{offline ? `离线 · ${formatAgo(p.lastSeen)}` : p.ipv4}</span>
                </div>
              </div>
              <button
                className="iconbtn"
                title="备注名称"
                onClick={(e) => {
                  e.stopPropagation()
                  startEdit(p)
                }}
              >
                <IconPencil size={14} />
              </button>
            </div>
          )
        })}

        {manual.map((p) => (
          <div
            key={p.fingerprint}
            className={`dev${target === p.fingerprint ? ' dev--active' : ''}`}
            onClick={() => onSelect(target === p.fingerprint ? null : p.fingerprint)}
          >
            <div className="dev__avatar dev__avatar--live" style={{ background: 'linear-gradient(135deg,#0ea5e9,#6366f1)' }}>
              {(peerName(p) || '?').slice(0, 1).toUpperCase()}
            </div>
            <div className="dev__body">
              <div className="dev__name">{peerName(p)}</div>
              <div className="dev__meta">
                <span className="tag tag--lan">MAN</span>
                <span>
                  {p.ipv4}:{p.port}
                </span>
              </div>
            </div>
            <button
              className="iconbtn"
              title="移除"
              onClick={(e) => {
                e.stopPropagation()
                onRemoveManual(p.fingerprint)
              }}
            >
              <IconTrash size={14} />
            </button>
          </div>
        ))}

        {adding ? (
          <div className="card card--pad" style={{ marginTop: 8, padding: 12 }}>
            <div className="row" style={{ gap: 6 }}>
              <input
                className="field"
                style={{ minHeight: 34, height: 34, padding: '0 10px', fontSize: 13 }}
                placeholder="192.168.1.20"
                value={ip}
                autoFocus
                onChange={(e) => setIp(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && submit()}
              />
              <input
                className="field"
                style={{ minHeight: 34, height: 34, width: 78, padding: '0 10px', fontSize: 13 }}
                placeholder="8080"
                value={port}
                onChange={(e) => setPort(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && submit()}
              />
            </div>
            <div className="row" style={{ gap: 8, marginTop: 10, justifyContent: 'flex-end' }}>
              <button className="btn btn--sm btn--ghost" onClick={() => setAdding(false)}>
                取消
              </button>
              <button className="btn btn--sm btn--primary" onClick={submit}>
                添加
              </button>
            </div>
          </div>
        ) : (
          <button className="btn btn--sm btn--ghost" style={{ width: '100%', marginTop: 8 }} onClick={() => setAdding(true)}>
            + 手动添加设备
          </button>
        )}

        <div className="section-title">跨网段</div>
        <div className="dev" onClick={() => onSelect('__cloud__')}>
          <div className="dev__avatar dev__avatar--live" style={{ background: 'linear-gradient(135deg,#8b5cf6,#22d3ee)' }}>
            <IconBolt size={16} />
          </div>
          <div className="dev__body">
            <div className="dev__name">取件码 / P2P</div>
            <div className="dev__meta">
              <span className="tag tag--p2p">P2P</span>
              <span>经服务器中转握手</span>
            </div>
          </div>
        </div>

        <div className="dev" onClick={() => onSelect('__relay__')}>
          <div className="dev__avatar" style={{ background: 'linear-gradient(135deg,#f59e0b,#f87171)' }}>
            <IconLink size={16} />
          </div>
          <div className="dev__body">
            <div className="dev__name">强制中转</div>
            <div className="dev__meta">
              <span className="tag tag--relay">RELAY</span>
              <span>P2P 失败时兜底</span>
            </div>
          </div>
        </div>
      </div>

      <div className="rail__foot">
        <div className="hint" style={{ textAlign: 'center' }}>
          子欣互传 · 我们自己的传输工具
        </div>
      </div>
    </aside>
  )
}
