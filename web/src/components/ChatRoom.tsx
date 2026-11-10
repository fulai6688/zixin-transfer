import { useCallback, useEffect, useRef, useState } from 'react'
import { formatBytes, peerName, type Health, type Toast } from '../types'
import { IconDownload, IconFile, IconPaperclip, IconSend } from './Icons'

interface Props {
  health: Health | null
  active: boolean
  onToast: (text: string, kind?: Toast['kind']) => void
  onUnread: (n: number) => void
}

interface FileExtra {
  id: string
  name: string
  size: number
  sha256: string
  mime?: string
  url: string
}

interface ChatMessage {
  id: string
  from: string
  alias: string
  body: string
  kind: 'text' | 'file' | 'system'
  extra?: string
  timestamp: number
}

interface PresenceInfo {
  event: 'join' | 'leave'
  alias: string
  count: number
}

function parseExtra(raw?: string): FileExtra | null {
  if (!raw) return null
  try {
    const v = JSON.parse(raw) as FileExtra
    return v && v.url ? v : null
  } catch {
    return null
  }
}

/**
 * 局域网聊天室。
 *
 * 房间由本机节点托管（/api/chat/ws），所有打开这个页面的浏览器都在同一间房；
 * 勾选“同步到其它设备”时，文本与文件公告会同时推给局域网里其它 子欣互传 节点。
 *
 * 文件只需上传一次：字节留在发送方节点（/api/chat/file/{id}），房间成员点一下就下载，
 * 所以“同一个文件发给很多台”不需要一台一台发。
 */
export default function ChatRoom({ health, active, onToast, onUnread }: Props) {
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [draft, setDraft] = useState('')
  const [online, setOnline] = useState(1)
  const [connected, setConnected] = useState(false)
  const [fanout, setFanout] = useState(true)
  const [uploading, setUploading] = useState<string | null>(null)
  const [dragOver, setDragOver] = useState(false)

  const wsRef = useRef<WebSocket | null>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const retryRef = useRef(0)
  const fileInput = useRef<HTMLInputElement>(null)
  const activeRef = useRef(active)
  const unreadRef = useRef(0)
  const selfFP = health?.device ?? ''
  const selfAlias = health?.alias ?? '我'

  activeRef.current = active

  const scrollDown = useCallback((smooth = true) => {
    window.setTimeout(() => {
      const el = listRef.current
      if (el) el.scrollTo({ top: el.scrollHeight, behavior: smooth ? 'smooth' : 'auto' })
    }, 40)
  }, [])

  // Mark everything read when the tab becomes visible.
  useEffect(() => {
    if (active && unreadRef.current !== 0) {
      unreadRef.current = 0
      onUnread(0)
    }
  }, [active, onUnread])

  /* --------------------------------------------------------------- socket */

  useEffect(() => {
    let alive = true
    let ws: WebSocket | null = null
    let timer: number | null = null

    const connect = () => {
      if (!alive) return
      const proto = location.protocol === 'https:' ? 'wss' : 'ws'
      const url =
        `${proto}://${location.host}/api/chat/ws?alias=${encodeURIComponent(selfAlias)}` +
        `&fingerprint=${encodeURIComponent(selfFP)}&kind=browser`
      ws = new WebSocket(url)
      wsRef.current = ws

      ws.onopen = () => {
        if (!alive) return
        setConnected(true)
        retryRef.current = 0
      }
      ws.onmessage = (ev) => {
        if (!alive) return
        let frame: { type: string; data: unknown }
        try {
          frame = JSON.parse(ev.data as string)
        } catch {
          return
        }
        if (frame.type === 'history') {
          setMessages((frame.data as ChatMessage[]) ?? [])
          scrollDown(false)
          return
        }
        if (frame.type === 'chat') {
          const m = frame.data as ChatMessage
          setMessages((prev) => [...prev.slice(-199), m])
          scrollDown()
          if (!activeRef.current && m.from !== selfFP) {
            unreadRef.current += 1
            onUnread(unreadRef.current)
          }
          return
        }
        if (frame.type === 'presence') {
          const p = frame.data as PresenceInfo
          setOnline(p.count)
          setMessages((prev) => [
            ...prev.slice(-199),
            {
              id: `sys-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
              from: '',
              alias: '',
              body: p.event === 'join' ? `${p.alias} 加入了房间` : `${p.alias} 离开了房间`,
              kind: 'system',
              timestamp: Date.now(),
            },
          ])
          scrollDown()
        }
      }
      ws.onclose = () => {
        if (!alive) return
        setConnected(false)
        const wait = Math.min(8000, 400 * Math.pow(2, retryRef.current++))
        timer = window.setTimeout(connect, wait)
      }
      ws.onerror = () => ws?.close()
    }

    fetch('/api/chat/history')
      .then((r) => r.json())
      .then((j: { messages?: ChatMessage[] }) => {
        if (alive && Array.isArray(j.messages) && j.messages.length) {
          setMessages(j.messages)
          scrollDown(false)
        }
      })
      .catch(() => undefined)

    connect()
    return () => {
      alive = false
      if (timer) window.clearTimeout(timer)
      ws?.close()
    }
  }, [selfAlias, selfFP, onUnread, scrollDown])

  /* ---------------------------------------------------------------- send */

  const send = useCallback(() => {
    const body = draft.trim()
    if (!body) return
    // The node relays this to other devices itself (it owns the device table),
    // so the UI never has to guess who is online.
    fetch('/api/chat/post', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ from: selfFP, alias: selfAlias, body, relay: true }),
    })
      .then((r) => r.json())
      .then((j: { delivered?: number }) => {
        if (j.delivered && j.delivered > 0) onToast(`已发送（同步到 ${j.delivered} 台设备）`, 'ok')
      })
      .catch(() => onToast('发送失败，节点未响应', 'err'))
    setDraft('')
  }, [draft, selfAlias, selfFP, onToast])

  /* -------------------------------------------------------------- upload */

  const shareFiles = useCallback(
    async (files: File[]) => {
      for (const file of files) {
        setUploading(file.name)
        try {
          const res = await fetch(
            `/api/chat/upload?name=${encodeURIComponent(file.name)}&mime=${encodeURIComponent(file.type || '')}`,
            { method: 'POST', body: file },
          )
          if (!res.ok) throw new Error(await res.text())
          // 文件只上传一次；服务端广播公告，并把下载地址改写成 LAN 地址。
          const j = (await res.json()) as { file: FileExtra; delivered?: number }
          const delivered = j.delivered ?? 0
          onToast(
            delivered > 0
              ? `已分享「${j.file.name}」：房间 + ${delivered} 台设备可下载`
              : `已分享「${j.file.name}」到房间`,
            'ok',
          )
        } catch (e) {
          onToast(`分享失败：${(e as Error).message}`, 'err')
        } finally {
          setUploading(null)
        }
      }
    },
    [onToast],
  )

  const header = messages.filter((m) => m.kind !== 'system')
  const speakers = new Set(header.map((m) => m.alias).filter(Boolean)).size

  return (
    <div
      className={`chat${dragOver ? ' chat--over' : ''}`}
      onDragOver={(e) => {
        e.preventDefault()
        setDragOver(true)
      }}
      onDragLeave={() => setDragOver(false)}
      onDrop={(e) => {
        e.preventDefault()
        setDragOver(false)
        const files = Array.from(e.dataTransfer.files ?? [])
        if (files.length) void shareFiles(files)
      }}
    >
      <div className="chat__head">
        <span className={`pill${connected ? ' pill--live' : ''}`}>
          <span className={`dot ${connected ? 'dot--live' : 'dot--busy'}`} />
          {connected ? `房间在线 · ${online} 人` : '连接中…'}
        </span>
        <span className="hint">
          {header.length} 条消息 · {speakers} 人参与
        </span>
        <span className="spacer" />
        <label className="hint chat__opt" title="文本与文件公告同时推给局域网里其它 子欣互传 设备">
          <input type="checkbox" checked={fanout} onChange={(e) => setFanout(e.target.checked)} />
          同步到其它设备
        </label>
      </div>

      <div className="chat__list" ref={listRef}>
        {messages.length === 0 && (
          <div className="chat__empty">
            还没有人说话。把文件拖进来，或直接发言 —— 同一个房间里的设备都能看到。
          </div>
        )}
        {messages.map((m) => {
          if (m.kind === 'system') {
            return (
              <div className="chat__sys" key={m.id}>
                {m.body}
              </div>
            )
          }
          const mine = selfFP !== '' && m.from === selfFP
          const file = m.kind === 'file' ? parseExtra(m.extra) : null
          return (
            <div className={`chat__row${mine ? ' chat__row--mine' : ''}`} key={m.id}>
              <div className="chat__bubble">
                {!mine && <div className="chat__who">{m.alias || peerName({ fingerprint: m.from })}</div>}
                {file ? (
                  <a className="chat__file" href={file.url} download={file.name}>
                    <span className="chat__file-ico">
                      <IconFile size={18} />
                    </span>
                    <span className="chat__file-body">
                      <span className="chat__file-name">{file.name}</span>
                      <span className="chat__file-meta">
                        {formatBytes(file.size)} · SHA-256 {file.sha256.slice(0, 8)}…
                      </span>
                    </span>
                    <span className="chat__file-dl">
                      <IconDownload size={16} />
                    </span>
                  </a>
                ) : (
                  <div className="chat__text">{m.body}</div>
                )}
                <div className="chat__time">{new Date(m.timestamp).toLocaleTimeString()}</div>
              </div>
            </div>
          )
        })}
        {uploading && <div className="chat__sys">正在上传「{uploading}」…</div>}
      </div>

      <div className="chat__composer">
        <input
          ref={fileInput}
          type="file"
          multiple
          hidden
          onChange={(e) => {
            const files = Array.from(e.target.files ?? [])
            e.target.value = ''
            if (files.length) void shareFiles(files)
          }}
        />
        <button
          className="iconbtn"
          title="发送文件到房间（只需上传一次，房间里的设备各自下载）"
          onClick={() => fileInput.current?.click()}
          disabled={uploading !== null}
        >
          <IconPaperclip size={17} />
        </button>
        <input
          className="field"
          placeholder={connected ? '说点什么…（Enter 发送，可拖文件进来）' : '连接中，Enter 也能发'}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault()
              send()
            }
          }}
        />
        <button className="btn btn--primary" onClick={send} disabled={!draft.trim()}>
          <IconSend size={15} /> 发送
        </button>
      </div>
    </div>
  )
}
