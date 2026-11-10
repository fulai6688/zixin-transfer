import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import DeviceRail from './components/DeviceRail'
import DropZone from './components/DropZone'
import TransferQueue from './components/TransferQueue'
import SettingsPanel from './components/SettingsPanel'
import ChatRoom from './components/ChatRoom'
import { CodeModal, IncomingModal } from './components/Modals'
import {
  IconBolt,
  IconChat,
  IconGear,
  IconMoon,
  IconScreen,
  IconSun,
  IconText,
  IconUpload,
} from './components/Icons'
import { api, sleep, uploadWithProgress } from './lib/api'
import { P2PConnection } from './lib/p2p'
import {
  peerName,
  type Device,
  type Health,
  type InboundRequest,
  type LANPeer,
  type ReceivedFile,
  type Settings,
  type Toast,
  type Transfer,
} from './types'

const CHUNK = 256 * 1024

export default function App() {
  const [health, setHealth] = useState<Health | null>(null)
  const [peers, setPeers] = useState<LANPeer[]>([])
  const [manual, setManual] = useState<LANPeer[]>(() => {
    try {
      return JSON.parse(localStorage.getItem('zx.manual') || '[]') as LANPeer[]
    } catch {
      return []
    }
  })
  const [target, setTarget] = useState<string | null>(null)
  const [files, setFiles] = useState<File[]>([])
  const [txs, setTxs] = useState<Transfer[]>([])
  const [received, setReceived] = useState<ReceivedFile[]>([])
  const [inbound, setInbound] = useState<InboundRequest[]>([])
  const [tab, setTab] = useState<'files' | 'chat' | 'text' | 'screen'>('files')
  const [chatUnread, setChatUnread] = useState(0)
  const [theme, setTheme] = useState<'dark' | 'light'>('dark')
  const [queueOpen, setQueueOpen] = useState(true)
  const [busy, setBusy] = useState(false)
  const [toast, setToast] = useState<Toast | null>(null)
  const [handled, setHandled] = useState<Set<string>>(new Set())
  const [codeMode, setCodeMode] = useState<'join' | 'offer' | null>(null)
  const [myCode, setMyCode] = useState<string | null>(null)
  const [text, setText] = useState('')
  const [incomingText, setIncomingText] = useState('')
  const [linkState, setLinkState] = useState('idle')
  const [scanning, setScanning] = useState(false)
  const [known, setKnown] = useState<Device[]>([])
  const [settings, setSettings] = useState<Settings | null>(null)
  const [settingsOpen, setSettingsOpen] = useState(false)

  const p2pRef = useRef<P2PConnection | null>(null)
  const chunksRef = useRef<Map<number, ArrayBuffer>>(new Map())
  const toastTimer = useRef<number | null>(null)
  /** Peers found by the subnet scan, kept across mDNS polls. */
  const scanCache = useRef<Map<string, LANPeer>>(new Map())
  /** Guards the one-shot retry after an empty sweep. */
  const retryRef = useRef(false)

  const say = useCallback((text: string, kind: Toast['kind'] = 'info') => {
    setToast({ id: Date.now(), text, kind })
    if (toastTimer.current) window.clearTimeout(toastTimer.current)
    toastTimer.current = window.setTimeout(() => setToast(null), 3200)
  }, [])

  /** Merge two peer lists, keeping the freshest sighting of each device. */
  const mergePeers = useCallback((a: LANPeer[], b: LANPeer[]) => {
    const byFP = new Map<string, LANPeer>()
    for (const p of [...a, ...b]) {
      const prev = byFP.get(p.fingerprint)
      if (!prev || (p.lastSeen ?? 0) >= (prev.lastSeen ?? 0)) byFP.set(p.fingerprint, p)
    }
    return [...byFP.values()].sort((x, y) => (y.lastSeen ?? 0) - (x.lastSeen ?? 0))
  }, [])

  /* ---------------------------------------------------------- lifecycle */

  useEffect(() => {
    document.documentElement.dataset.theme = theme
    localStorage.setItem('zx.theme', theme)
  }, [theme])

  useEffect(() => {
    const saved = localStorage.getItem('zx.theme')
    if (saved === 'light' || saved === 'dark') setTheme(saved)
  }, [])

  useEffect(() => {
    let alive = true
    const load = async () => {
      try {
        const h = await api.health()
        if (alive) setHealth(h)
      } catch {
        /* node may still be starting */
      }
    }
    void load()
    const t = window.setInterval(load, 8000)
    return () => {
      alive = false
      window.clearInterval(t)
    }
  }, [])

  // Local network discovery: mDNS table plus the cached subnet-scan results.
  useEffect(() => {
    let alive = true
    const scan = async () => {
      try {
        const { devices } = await api.lanPeers()
        if (!alive) return
        const cutoff = Date.now() - 90_000
        const cached = [...scanCache.current.values()].filter((p) => (p.lastSeen ?? 0) > cutoff)
        const merged = mergePeers(devices, cached)
        setPeers(merged)
        setTarget((cur) => (cur && (merged.some((d) => d.fingerprint === cur) || manual.some((m) => m.fingerprint === cur)) ? cur : null))
      } catch {
        /* ignore transient errors */
      }
    }
    void scan()
    const t = window.setInterval(scan, 3000)
    return () => {
      alive = false
      window.clearInterval(t)
    }
  }, [manual, mergePeers])

  // Inbound requests awaiting a decision.
  useEffect(() => {
    let alive = true
    const poll = async () => {
      try {
        const { pending } = await api.pending()
        if (!alive) return
        setInbound(pending.filter((p) => !handled.has(p.id)))
      } catch {
        /* ignore */
      }
    }
    void poll()
    const t = window.setInterval(poll, 1500)
    return () => {
      alive = false
      window.clearInterval(t)
    }
  }, [handled])

  // Files this node has already received (any channel).
  useEffect(() => {
    let alive = true
    const poll = async () => {
      try {
        const { files } = await api.received()
        if (alive) setReceived(files.slice().reverse())
      } catch {
        /* ignore */
      }
    }
    void poll()
    const t = window.setInterval(poll, 3000)
    return () => {
      alive = false
      window.clearInterval(t)
    }
  }, [])

  // Known devices: remembered across restarts, so a peer never "vanishes".
  useEffect(() => {
    let alive = true
    const poll = async () => {
      try {
        const { devices } = await api.knownDevices()
        if (alive) setKnown(devices)
      } catch {
        /* ignore */
      }
    }
    void poll()
    const t = window.setInterval(poll, 5000)
    return () => {
      alive = false
      window.clearInterval(t)
    }
  }, [])

  // Settings: the receive directory is shown in the UI and used by the node.
  useEffect(() => {
    let alive = true
    const load = async () => {
      try {
        const s = await api.settings()
        if (alive) setSettings(s)
      } catch {
        /* ignore */
      }
    }
    void load()
    return () => {
      alive = false
    }
  }, [])

  const pushTx = useCallback((t: Transfer) => setTxs((prev) => [t, ...prev].slice(0, 60)), [])
  const patchTx = useCallback(
    (id: string, patch: Partial<Transfer>) =>
      setTxs((prev) => prev.map((t) => (t.id === id ? { ...t, ...patch } : t))),
    [],
  )

  /** Devices merged from mDNS, the subnet scan, the registry and manual entries. */
  const allPeers = useMemo<LANPeer[]>(() => {
    const byFP = new Map<string, LANPeer>()
    const put = (p: LANPeer) => {
      const prev = byFP.get(p.fingerprint)
      if (!prev) {
        byFP.set(p.fingerprint, p)
        return
      }
      byFP.set(p.fingerprint, {
        ...prev,
        ...p,
        aliasOverride: p.aliasOverride ?? prev.aliasOverride,
        online: p.online ?? prev.online,
      })
    }
    const now = Date.now()
    for (const d of known) {
      put({
        instance: `known-${d.fingerprint.slice(0, 6)}`,
        host: d.address ?? '',
        ipv4: d.address ?? '',
        port: d.port ?? 0,
        fingerprint: d.fingerprint,
        alias: d.alias,
        aliasOverride: d.aliasOverride,
        platform: d.platform,
        lastSeen: d.lastSeen,
        source: 'known',
        online: now - d.lastSeen < 120_000,
      })
    }
    for (const p of peers) put({ ...p, online: true })
    for (const m of manual) put({ ...m, source: 'manual', online: true })
    return [...byFP.values()].sort((a, b) => {
      const byOnline = Number(b.online ?? false) - Number(a.online ?? false)
      if (byOnline !== 0) return byOnline
      return (b.lastSeen ?? 0) - (a.lastSeen ?? 0)
    })
  }, [known, peers, manual])

  /** Subnet scan: the fallback when mDNS is blocked by the firewall. */
  const runScan = useCallback(
    async (silent = false) => {
      setScanning(true)
      try {
        const { devices } = await api.lanScan()
        const now = Date.now()
        for (const d of devices) scanCache.current.set(d.fingerprint, { ...d, lastSeen: now })
        if (devices.length) {
          setPeers((prev) => mergePeers(prev, devices))
        } else {
          setPeers((prev) => prev.filter((p) => (p.lastSeen ?? 0) > now - 90_000))
        }
        const k = await api.knownDevices()
        setKnown(k.devices)
        if (!silent) {
          say(
            devices.length ? `扫描到 ${devices.length} 台设备` : '未发现其他设备（检查防火墙/同一 WiFi）',
            devices.length ? 'ok' : 'err',
          )
        }
        // An empty sweep is often a warm-up miss: try once more shortly.
        if (!devices.length && !retryRef.current) {
          retryRef.current = true
          window.setTimeout(() => {
            retryRef.current = false
            void runScan(true)
          }, 2500)
        }
      } catch (e) {
        if (!silent) say((e as Error).message, 'err')
      } finally {
        setScanning(false)
      }
    },
    [mergePeers, say],
  )

  // First-run discovery plus a slow refresh: mDNS is blocked on many
  // machines, so keep the subnet sweep going instead of scanning once.
  useEffect(() => {
    const first = window.setTimeout(() => void runScan(true), 1200)
    const repeat = window.setInterval(() => {
      if (document.visibilityState === 'visible') void runScan(true)
    }, 25_000)
    return () => {
      window.clearTimeout(first)
      window.clearInterval(repeat)
    }
  }, [runScan])

  /** Store a user-chosen name for a device. */
  const renameDevice = useCallback(
    async (fingerprint: string, alias: string) => {
      try {
        await api.renameDevice(fingerprint, alias)
        const k = await api.knownDevices()
        setKnown(k.devices)
        say(alias ? `已备注为“${alias}”` : '已恢复默认名称', 'ok')
      } catch (e) {
        say((e as Error).message, 'err')
      }
    },
    [say],
  )

  /* ------------------------------------------------------------ settings */

  const saveSettings = useCallback(
    async (dir: string) => {
      const s = await api.saveSettings(dir)
      setSettings(s)
      say(`接收目录已改为 ${s.downloadDir}`, 'ok')
    },
    [say],
  )

  const pickFolder = useCallback(
    async (start: string) => {
      const r = await api.pickFolder(start)
      if (!r.ok) {
        say(r.error || '无法打开目录选择器', 'err')
        return null
      }
      return r.path ?? null
    },
    [say],
  )

  const revealPath = useCallback(
    async (path: string) => {
      try {
        await api.reveal(path)
      } catch (e) {
        say((e as Error).message, 'err')
      }
    },
    [say],
  )

  const openFilePath = useCallback(
    async (path: string) => {
      try {
        await api.openFile(path)
      } catch (e) {
        say((e as Error).message, 'err')
      }
    },
    [say],
  )

  /** Ask Windows for permission to accept inbound connections. */
  const installFirewall = useCallback(async () => {
    try {
      const r = await api.installFirewall()
      if (r.ok) {
        say('防火墙规则已放行，现在其它设备可以连进来', 'ok')
        setHealth((h) => (h ? { ...h, firewall: true } : h))
      } else {
        say(r.error || '放行失败', 'err')
      }
    } catch (e) {
      say((e as Error).message, 'err')
    }
  }, [say])

  const addManual = useCallback((ip: string, port: number) => {
    setManual((prev) => {
      const next = [
        ...prev,
        {
          instance: `manual-${ip}`,
          host: ip,
          ipv4: ip,
          port,
          fingerprint: `manual-${ip}-${port}`,
          alias: `手动设备 ${ip}`,
          platform: 'unknown',
          lastSeen: Date.now(),
        },
      ]
      localStorage.setItem('zx.manual', JSON.stringify(next))
      return next
    })
    say(`已添加手动设备 ${ip}:${port}`, 'ok')
  }, [say])

  const removeManual = useCallback((fingerprint: string) => {
    setManual((prev) => {
      const next = prev.filter((m) => m.fingerprint !== fingerprint)
      localStorage.setItem('zx.manual', JSON.stringify(next))
      return next
    })
  }, [])

  /* ------------------------------------------------------------ sending */

  const sendViaLAN = useCallback(
    async (peer: LANPeer, file: File) => {
      const id = Math.random().toString(36).slice(2)
      pushTx({
        id,
        name: file.name,
        size: file.size,
        direction: 'send',
        state: 'active',
        sent: 0,
        speed: 0,
        channel: 'lan',
        peer: peer.alias,
      })
      try {
        const base = `http://${peer.ipv4}:${peer.port}`
        const inquiry = await api.inquire(base, {
          name: file.name,
          size: file.size,
          mime: file.type,
          fingerprint: health?.device ?? '',
          alias: health?.alias ?? '',
        })
        let state: { status: string; token?: string } = { status: 'pending' }
        for (let i = 0; i < 240; i++) {
          state = await api.inboxState(base, inquiry.id)
          if (state.status !== 'pending') break
          await sleep(500)
        }
        if (state.status !== 'accepted' || !state.token) {
          patchTx(id, { state: 'rejected', error: state.status === 'rejected' ? '对方已拒绝' : '等待确认超时' })
          return
        }
        let last = performance.now()
        let lastSent = 0
        await uploadWithProgress(
          `${base}/api/lan/upload?token=${encodeURIComponent(state.token)}&name=${encodeURIComponent(file.name)}`,
          file,
          (sent) => {
            const now = performance.now()
            const dt = (now - last) / 1000
            const speed = dt > 0 ? (sent - lastSent) / dt : 0
            last = now
            lastSent = sent
            patchTx(id, { sent, speed })
          },
        )
        patchTx(id, { state: 'done', sent: file.size, speed: 0 })
      } catch (e) {
        patchTx(id, { state: 'error', error: (e as Error).message })
      }
    },
    [health, pushTx, patchTx],
  )

  const sendViaP2P = useCallback(
    async (conn: P2PConnection, file: File) => {
      const id = Math.random().toString(36).slice(2)
      pushTx({
        id,
        name: file.name,
        size: file.size,
        direction: 'send',
        state: 'active',
        sent: 0,
        speed: 0,
        channel: conn.relay ? 'relay' : 'p2p',
        peer: 'P2P 对端',
      })
      try {
        conn.sendMeta({ name: file.name, size: file.size, mime: file.type })
        let offset = 0
        let last = performance.now()
        let lastSent = 0
        while (offset < file.size) {
          const blob = file.slice(offset, Math.min(offset + CHUNK, file.size))
          const buf = await blob.arrayBuffer()
          conn.sendChunk(offset, buf)
          offset += buf.byteLength
          const now = performance.now()
          const dt = (now - last) / 1000
          const speed = dt > 0 ? (offset - lastSent) / dt : 0
          last = now
          lastSent = offset
          patchTx(id, { sent: offset, speed })
          await sleep(6)
        }
        conn.sendDone()
        patchTx(id, { state: 'done', sent: file.size, speed: 0 })
      } catch (e) {
        patchTx(id, { state: 'error', error: (e as Error).message })
      }
    },
    [pushTx, patchTx],
  )

  const handleSend = useCallback(async () => {
    if (!files.length) return
    if (target === '__cloud__' || target === '__relay__') {
      setCodeMode('offer')
      return
    }
    const peer = allPeers.find((p) => p.fingerprint === target)
    if (!peer) {
      say('请先在左侧选择一个局域网设备', 'err')
      return
    }
    setBusy(true)
    for (const f of files) await sendViaLAN(peer, f)
    setFiles([])
    setBusy(false)
  }, [files, target, allPeers, sendViaLAN, say])

  /* ---------------------------------------------------------- receiving */

  const decide = useCallback(
    async (accept: boolean, savePath = '') => {
      const req = inbound[0]
      if (!req) return
      setHandled((prev) => new Set(prev).add(req.id))
      try {
        const r = await api.decide(req.id, accept, savePath)
        if (savePath && r.status === 'accepted') {
          say(`本次保存到 ${savePath}`, 'ok')
        }
      } catch (e) {
        say((e as Error).message, 'err')
      }
    },
    [inbound, say],
  )

  const saveInbound = useCallback(
    async (name: string, size: number, mime: string) => {
      const map = chunksRef.current
      const offsets = [...map.keys()].sort((a, b) => a - b)
      const parts: BlobPart[] = []
      let total = 0
      for (const off of offsets) {
        const buf = map.get(off)!
        parts.push(buf)
        total += buf.byteLength
      }
      map.clear()
      if (!total) return
      const id = Math.random().toString(36).slice(2)
      pushTx({
        id,
        name,
        size,
        direction: 'receive',
        state: 'active',
        sent: 0,
        speed: 0,
        channel: 'p2p',
        peer: 'P2P 对端',
      })
      try {
        const res = await fetch('/api/inbox/save?name=' + encodeURIComponent(name) + '&mime=' + encodeURIComponent(mime), {
          method: 'POST',
          body: new Blob(parts, { type: mime || 'application/octet-stream' }),
        })
        if (!res.ok) throw new Error(await res.text())
        patchTx(id, { state: 'done', sent: total, speed: 0 })
        say(`已接收 ${name}`, 'ok')
      } catch (e) {
        patchTx(id, { state: 'error', error: (e as Error).message })
      }
    },
    [pushTx, patchTx, say],
  )

  const makeLink = useCallback(
    async (role: 'sender' | 'receiver', sessionId: string) => {
      chunksRef.current = new Map()
      const conn = new P2PConnection(
        {
          sessionId,
          role,
          fingerprint: health?.device ?? '',
          alias: health?.alias ?? '',
          signalingBase: health?.relay || undefined,
        },
        {
          onState: (s, ch) => setLinkState(`${s}·${ch}`),
          onMeta: (meta) => {
            ;(window as any).__fluxMeta = meta
            const id = Math.random().toString(36).slice(2)
            pushTx({
              id,
              name: meta.name,
              size: meta.size,
              direction: 'receive',
              state: 'active',
              sent: 0,
              speed: 0,
              channel: 'p2p',
              peer: 'P2P 对端',
            })
          },
          onBinary: (offset, payload) => {
            chunksRef.current.set(offset, payload)
          },
          onText: (body) => setIncomingText(body),
          onDone: () => {
            const meta = (window as any).__fluxMeta
            if (meta) void saveInbound(meta.name, meta.size, meta.mime)
          },
          onError: (e) => say(e.message, 'err'),
        },
      )
      p2pRef.current = conn
      await conn.start()
      return conn
    },
    [health, pushTx, saveInbound, say],
  )

  const offerCode = useCallback(async () => {
    try {
      const relay = health?.relay ?? ''
      const { id, code } = await api.createSession(relay)
      setMyCode(code)
      setCodeMode('offer')
      const conn = await makeLink('sender', id)
      ;(window as any).__zxSender = conn
      say(relay ? '取件码已生成（经云端中转）' : '取件码已生成，等待对方加入', 'ok')
    } catch (e) {
      say((e as Error).message, 'err')
    }
  }, [makeLink, say, health])

  const joinCode = useCallback(
    async (code: string) => {
      try {
        const relay = health?.relay ?? ''
        const info = await api.sessionByCode(code, relay)
        setCodeMode(null)
        const conn = await makeLink('receiver', info.id)
        ;(window as any).__fluxReceiver = conn
        say('已加入，正在建立直连…', 'ok')
      } catch (e) {
        say((e as Error).message, 'err')
      }
    },
    [makeLink, say, health],
  )

  const sendTextOverLink = useCallback(() => {
    const conn = (window as any).__zxSender as P2PConnection | undefined
    if (!conn || !text.trim()) {
      say('请先生成取件码', 'err')
      return
    }
    conn.sendText(text)
    say('文本已发送', 'ok')
  }, [text, say])

  /* ------------------------------------------------------------- render */

  const targetName = useMemo(() => {
    if (!target) return null
    if (target === '__cloud__') return 'P2P 取件码'
    if (target === '__relay__') return '强制中转'
    const p = allPeers.find((x) => x.fingerprint === target)
    return p ? peerName(p) : null
  }, [target, allPeers])

  const selfAlias = health?.alias ?? '本机'
  const selfFP = health?.device ?? '00000000'

  return (
    <div className="shell">
      <div className="brand">
        <div className="brand__mark">
          <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="#fff" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M4 12h11" />
            <path d="m12 8 4 4-4 4" />
            <path d="M20 4v16" opacity=".55" />
          </svg>
        </div>
        <div>
          <div className="brand__name">子欣互传</div>
          <div className="brand__tag">our own transfer</div>
        </div>
      </div>

      <div className="topbar">
        <span className={`pill${linkState.startsWith('open') ? ' pill--live' : ''}`}>
          <span className={`dot ${linkState.startsWith('open') ? 'dot--live' : linkState === 'idle' ? '' : 'dot--busy'}`} />
          {linkState === 'idle' ? '就绪' : linkState}
        </span>
        <span className="pill">
          <span className="dot dot--live" />
          {allPeers.filter((p) => p.online).length} 台在线 / {allPeers.length} 台已知
        </span>
        {settings && (
          <span className="pill pill--path" title={settings.downloadDir} onClick={() => setSettingsOpen(true)}>
            存到 <span className="mono">{settings.downloadDir.split(/[\\/]/).pop() || settings.downloadDir}</span>
          </span>
        )}
        <span className="spacer" />
        <button className="btn btn--sm btn--ghost" onClick={offerCode}>
          <IconBolt size={14} /> 生成取件码
        </button>
        <button className="iconbtn" onClick={() => setSettingsOpen(true)} title="设置">
          <IconGear size={18} />
        </button>
        <button className="iconbtn" onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')} title="切换主题">
          {theme === 'dark' ? <IconSun size={18} /> : <IconMoon size={18} />}
        </button>
      </div>

      <DeviceRail
        selfAlias={selfAlias}
        selfFingerprint={selfFP}
        peers={allPeers}
        manual={manual}
        target={target}
        scanning={scanning}
        needFirewall={health?.firewall === false}
        onSelect={(f) => setTarget(f)}
        onAddManual={addManual}
        onRemoveManual={removeManual}
        onRefresh={() => api.lanPeers().then(({ devices }) => setPeers(devices))}
        onScan={runScan}
        onInstallFirewall={installFirewall}
        onRename={renameDevice}
        onOpenSettings={() => setSettingsOpen(true)}
      />

      <main className="main">
        <div className="tabs">
          <button className={`tab${tab === 'files' ? ' tab--on' : ''}`} onClick={() => setTab('files')}>
            <IconUpload size={16} /> 文件
          </button>
          <button className={`tab${tab === 'chat' ? ' tab--on' : ''}`} onClick={() => setTab('chat')}>
            <IconChat size={16} /> 聊天室
            {chatUnread > 0 && (
              <span className="tab__badge">{chatUnread > 99 ? '99+' : chatUnread}</span>
            )}
          </button>
          <button className={`tab${tab === 'text' ? ' tab--on' : ''}`} onClick={() => setTab('text')}>
            <IconText size={16} /> 文本
          </button>
          <button className={`tab${tab === 'screen' ? ' tab--on' : ''}`} onClick={() => setTab('screen')}>
            <IconScreen size={16} /> 桌面
          </button>
        </div>

        <div className="workspace">
          {/* The room stays mounted so the socket keeps receiving while the
              user is on another tab (that is what the unread badge counts). */}
          <div className="tabpanel" style={{ display: tab === 'chat' ? 'flex' : 'none' }}>
            <ChatRoom
              health={health}
              active={tab === 'chat'}
              onToast={say}
              onUnread={setChatUnread}
            />
          </div>

          {tab === 'files' && (
            <DropZone
              files={files}
              onFiles={setFiles}
              onRemove={(i) => setFiles((prev) => prev.filter((_, k) => k !== i))}
              onSend={handleSend}
              targetName={targetName}
              busy={busy}
            />
          )}

          {tab === 'text' && (
            <div className="stack">
              <textarea
                className="field"
                placeholder="输入要分享的文本…"
                value={text}
                onChange={(e) => setText(e.target.value)}
              />
              <div className="row" style={{ justifyContent: 'flex-end' }}>
                <button className="btn btn--primary" onClick={sendTextOverLink}>
                  通过 P2P 发送文本
                </button>
              </div>
              {incomingText && (
                <div className="card card--pad">
                  <div className="section-title" style={{ padding: '0 0 8px' }}>
                    收到的文本
                  </div>
                  <div style={{ whiteSpace: 'pre-wrap' }}>{incomingText}</div>
                </div>
              )}
            </div>
          )}

          {tab === 'screen' && (
            <div className="card card--pad empty">
              <div style={{ color: 'var(--text-dim)', marginBottom: 6 }}>桌面共享</div>
              <div style={{ maxWidth: 460, margin: '0 auto' }}>
                基于 WebRTC 视频轨的实时屏幕共享,将在 v1.1 开放 —— 信令与传输通道已就绪,
                剩余工作是轨道协商与 UI 渲染。
              </div>
            </div>
          )}
        </div>

        <TransferQueue
          txs={txs}
          received={received}
          open={queueOpen}
          local={health?.local === true}
          onToggle={() => setQueueOpen((v) => !v)}
          onReveal={revealPath}
          onOpenFile={openFilePath}
        />
      </main>

      <IncomingModal
        request={inbound[0] ?? null}
        defaultDir={settings?.downloadDir ?? ''}
        local={health?.local === true}
        onPickDir={pickFolder}
        onDecide={(accept, savePath) => void decide(accept, savePath)}
      />

      <SettingsPanel
        open={settingsOpen}
        settings={settings}
        local={health?.local === true}
        onSave={saveSettings}
        onPick={pickFolder}
        onReveal={revealPath}
        onClose={() => setSettingsOpen(false)}
      />

      <CodeModal
        open={codeMode === 'join'}
        title="输入取件码"
        subtitle="向对方索取 6 位取件码并输入，即可建立跨网段直连"
        onSubmit={(c) => void joinCode(c)}
        onClose={() => setCodeMode(null)}
      />

      {codeMode === 'offer' && (
        <div className="overlay" onClick={() => setCodeMode(null)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <div className="modal__head">
              <div className="modal__title">把取件码发给对方</div>
              <div className="modal__sub">对方输入这 6 位码后，两台设备将尝试直接连接</div>
            </div>
            <div className="modal__body">
              <div
                style={{
                  fontSize: 42,
                  letterSpacing: 12,
                  textAlign: 'center',
                  fontFamily: 'var(--mono)',
                  fontWeight: 700,
                  padding: '8px 0 4px',
                  background: 'linear-gradient(135deg,var(--brand-1),var(--accent))',
                  WebkitBackgroundClip: 'text',
                  WebkitTextFillColor: 'transparent',
                }}
              >
                {myCode}
              </div>
              <div className="hint" style={{ textAlign: 'center' }}>
                会话 1 小时内有效
              </div>
            </div>
            <div className="modal__foot">
              <button
                className="btn btn--ghost"
                onClick={() => {
                  void navigator.clipboard?.writeText(myCode ?? '')
                  say('取件码已复制', 'ok')
                }}
              >
                复制
              </button>
              <button
                className="btn btn--primary"
                onClick={() => {
                  setCodeMode(null)
                  const conn = (window as any).__zxSender as P2PConnection | undefined
                  if (conn && files.length) {
                    setBusy(true)
                    void (async () => {
                      for (const f of files) await sendViaP2P(conn, f)
                      setFiles([])
                      setBusy(false)
                    })()
                  }
                }}
              >
                开始发送
              </button>
            </div>
          </div>
        </div>
      )}

      {toast && (
        <div
          className="toast"
          style={{
            borderColor:
              toast.kind === 'ok'
                ? 'rgba(52,211,153,.4)'
                : toast.kind === 'err'
                  ? 'rgba(248,113,113,.4)'
                  : undefined,
          }}
        >
          {toast.text}
        </div>
      )}
    </div>
  )
}
