import type { Channel } from '../types'

/** STUN servers used for NAT traversal. Public, no credentials. */
export const DEFAULT_ICE: RTCIceServer[] = [
  { urls: 'stun:stun.l.google.com:19302' },
  { urls: 'stun:stun.cloudflare.com:3478' },
  { urls: 'stun:stun.qq.com:3478' },
  { urls: 'stun:stun.miwifi.com:3478' },
  { urls: 'stun:stun.sip.163.com:3478' },
]

export interface P2POptions {
  sessionId: string
  role: 'sender' | 'receiver'
  fingerprint: string
  alias: string
  iceServers?: RTCIceServer[]
  /**
   * Base address of the signalling server. Defaults to the page origin
   * (same-node transfers); point it at the cloud relay for cross-network
   * transfers so both peers meet on one server.
   */
  signalingBase?: string
}

export interface P2PEvents {
  onState?: (state: string, channel: Channel) => void
  onMeta?: (meta: { name: string; size: number; mime: string }) => void
  onBinary?: (offset: number, payload: ArrayBuffer) => void
  onText?: (body: string) => void
  onDone?: () => void
  onError?: (err: Error) => void
}

/**
 * P2PConnection establishes a WebRTC data channel through the 子欣互传
 * signalling relay. The relay never sees payload bytes: it only forwards
 * the SDP/ICE handshake and, if the direct path never comes up, the
 * encrypted payload frames.
 */
export class P2PConnection {
  private pc: RTCPeerConnection
  private ws: WebSocket | null = null
  private dc: RTCDataChannel | null = null
  private opts: P2POptions
  private ev: P2PEvents
  private pendingCandidates: RTCIceCandidateInit[] = []
  private fallbackTimer: number | null = null
  private useRelay = false
  private answerReceived = false

  constructor(opts: P2POptions, ev: P2PEvents) {
    this.opts = opts
    this.ev = ev
    this.pc = new RTCPeerConnection({ iceServers: opts.iceServers ?? DEFAULT_ICE })

    this.pc.onicecandidate = (e) => {
      if (e.candidate) this.signal('ice-candidate', e.candidate.toJSON())
    }
    this.pc.onconnectionstatechange = () => {
      const s = this.pc.connectionState
      this.ev.onState?.(s, this.useRelay ? 'relay' : 'p2p')
      if (s === 'failed') this.ev.onState?.('failed', this.useRelay ? 'relay' : 'p2p')
    }
    this.pc.ondatachannel = (e) => this.attachChannel(e.channel)

    if (opts.role === 'sender') {
      const ch = this.pc.createDataChannel('flux', { ordered: false })
      this.attachChannel(ch)
    }
  }

  private attachChannel(ch: RTCDataChannel) {
    this.dc = ch
    ch.binaryType = 'arraybuffer'
    ch.onopen = () => {
      this.ev.onState?.('open', this.useRelay ? 'relay' : 'p2p')
      if (this.fallbackTimer !== null) {
        window.clearTimeout(this.fallbackTimer)
        this.fallbackTimer = null
      }
    }
    ch.onclose = () => this.ev.onState?.('closed', this.useRelay ? 'relay' : 'p2p')
    ch.onerror = (e) => this.ev.onError?.(new Error(`data channel error: ${String(e)}`))
    ch.onmessage = (e) => this.handleMessage(e)
  }

  private handleMessage(e: MessageEvent) {
    if (typeof e.data === 'string') {
      try {
        const frame = JSON.parse(e.data)
        if (frame.t === 'meta') {
          this.ev.onMeta?.(frame)
        } else if (frame.t === 'text') {
          this.ev.onText?.(frame.body ?? '')
        } else if (frame.t === 'done') {
          this.ev.onDone?.()
        } else if (frame.t === 'relay-begin') {
          this.useRelay = true
          this.ev.onState?.('relay', 'relay')
        }
      } catch {
        /* ignore malformed frames */
      }
      return
    }
    const buf = e.data as ArrayBuffer
    if (buf.byteLength < 8) return
    const view = new DataView(buf)
    const offset = Number(view.getBigUint64(0, false))
    this.ev.onBinary?.(offset, buf.slice(8))
  }

  /** Opens the signalling socket and starts the handshake. */
  async start(): Promise<void> {
    const base = this.opts.signalingBase || location.origin
    const proto = base.startsWith('https:') ? 'wss' : 'ws'
    const url = `${proto}://${base.replace(/^https?:\/\//, '')}/api/ws/session?session=${this.opts.sessionId}` +
      `&role=${this.opts.role}&fingerprint=${encodeURIComponent(this.opts.fingerprint)}` +
      `&alias=${encodeURIComponent(this.opts.alias)}`

    await new Promise<void>((resolve, reject) => {
      const ws = new WebSocket(url)
      this.ws = ws
      const timer = window.setTimeout(() => reject(new Error('信令连接超时')), 12000)
      ws.onopen = () => {
        window.clearTimeout(timer)
        resolve()
      }
      ws.onerror = () => {
        window.clearTimeout(timer)
        reject(new Error('信令连接失败'))
      }
      ws.onmessage = (e) => {
        // Some relays hand signal frames over as binary; accept both.
        const data = e.data
        if (typeof data === 'string') {
          void this.onSignal(data)
        } else if (data instanceof Blob) {
          void data.text().then((t) => this.onSignal(t))
        } else if (data instanceof ArrayBuffer) {
          void this.onSignal(new TextDecoder().decode(data))
        }
      }
    })

    if (this.opts.role === 'sender') {
      await this.makeOffer()
      // If ICE never converges, fall back to relaying through the server.
      this.fallbackTimer = window.setTimeout(() => {
        if (this.dc?.readyState !== 'open') {
          this.useRelay = true
          this.signal('relay-begin', {})
          this.ev.onState?.('relay', 'relay')
        }
      }, 9000)
    }
  }

  private async onSignal(raw: string) {
    let frame: any
    try {
      frame = JSON.parse(raw)
    } catch {
      return
    }
    const type = frame.type
    const payload = frame.payload ?? {}

    if (type === 'offer') {
      await this.pc.setRemoteDescription(payload)
      await this.flushCandidates()
      const answer = await this.pc.createAnswer()
      await this.pc.setLocalDescription(answer)
      this.signal('answer', answer)
    } else if (type === 'answer') {
      this.answerReceived = true
      if (!this.pc.remoteDescription) {
        await this.pc.setRemoteDescription(payload)
        await this.flushCandidates()
      }
    } else if (type === 'ice-candidate') {
      if (this.pc.remoteDescription) {
        await this.pc.addIceCandidate(payload).catch(() => undefined)
      } else {
        this.pendingCandidates.push(payload)
      }
    } else if (type === 'peer-joined') {
      // The other side just arrived. If the handshake is still pending we
      // send the offer again, so a lost first attempt cannot stall the
      // connection.
      if (this.opts.role === 'sender' && !this.answerReceived) {
        await this.makeOffer()
      }
    } else if (type === 'error') {
      this.ev.onError?.(new Error(payload.message ?? 'signalling error'))
    }
  }

  private async flushCandidates() {
    const list = this.pendingCandidates.splice(0)
    for (const c of list) {
      await this.pc.addIceCandidate(c).catch(() => undefined)
    }
  }

  private async makeOffer() {
    const offer = await this.pc.createOffer()
    await this.pc.setLocalDescription(offer)
    this.signal('offer', offer)
  }

  private signal(type: string, payload: unknown) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify({ type, payload }))
    }
  }

  get relay(): boolean {
    return this.useRelay
  }

  sendText(body: string) {
    this.dc?.send(JSON.stringify({ t: 'text', body }))
  }

  sendMeta(meta: { name: string; size: number; mime: string }) {
    this.dc?.send(JSON.stringify({ t: 'meta', ...meta }))
  }

  sendDone() {
    this.dc?.send(JSON.stringify({ t: 'done' }))
  }

  /** Sends one binary chunk prefixed with its 64 bit offset. */
  sendChunk(offset: number, payload: ArrayBuffer) {
    if (!this.dc || this.dc.readyState !== 'open') return
    const out = new ArrayBuffer(8 + payload.byteLength)
    new DataView(out).setBigUint64(0, BigInt(offset), false)
    new Uint8Array(out, 8).set(new Uint8Array(payload))
    this.dc.send(out)
  }

  close() {
    if (this.fallbackTimer !== null) window.clearTimeout(this.fallbackTimer)
    try {
      this.dc?.close()
    } catch {
      /* already closed */
    }
    try {
      this.pc.close()
    } catch {
      /* already closed */
    }
    this.ws?.close()
  }
}
