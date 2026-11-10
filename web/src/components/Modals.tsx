import { useEffect, useRef, useState } from 'react'
import { formatBytes, type InboundRequest } from '../types'
import { IconCheck, IconClose, IconFolder } from './Icons'

interface IncomingProps {
  request: InboundRequest | null
  defaultDir: string
  local: boolean
  onPickDir: (start: string) => Promise<string | null>
  onDecide: (accept: boolean, savePath: string) => void
}

export function IncomingModal({ request, defaultDir, local, onPickDir, onDecide }: IncomingProps) {
  const [dir, setDir] = useState(defaultDir)
  const [custom, setCustom] = useState(false)

  useEffect(() => {
    setDir(defaultDir)
    setCustom(false)
  }, [request?.id, defaultDir])

  if (!request) return null

  const target = custom && dir.trim() ? dir.trim() : ''

  return (
    <div className="overlay" onClick={() => onDecide(false, '')}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal__head">
          <div className="modal__title">收到一个传输请求</div>
          <div className="modal__sub">{request.alias || '未知设备'} 想发送一个文件</div>
        </div>
        <div className="modal__body">
          <div className="row" style={{ gap: 14 }}>
            <div className="avatar-lg" style={{ margin: 0 }}>
              {(request.alias || '?').slice(0, 1).toUpperCase()}
            </div>
            <div style={{ minWidth: 0 }}>
              <div style={{ fontWeight: 560 }}>{request.name}</div>
              <div className="hint mono">{formatBytes(request.size)}</div>
            </div>
          </div>

          <div style={{ marginTop: 16 }}>
            <div className="section-title" style={{ padding: '0 0 8px' }}>
              保存到
            </div>
            {custom ? (
              <div className="row" style={{ gap: 8 }}>
                <input
                  className="field"
                  value={dir}
                  onChange={(e) => setDir(e.target.value)}
                  placeholder="D:\Downloads\子欣互传"
                  spellCheck={false}
                />
                <button
                  className="btn btn--ghost"
                  disabled={!local}
                  onClick={async () => {
                    const picked = await onPickDir(dir)
                    if (picked) setDir(picked)
                  }}
                >
                  <IconFolder size={15} />
                </button>
              </div>
            ) : (
              <div className="row" style={{ gap: 8 }}>
                <div className="hint mono" style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis' }}>
                  {defaultDir || '—'}
                </div>
                <button className="btn btn--sm btn--ghost" onClick={() => setCustom(true)}>
                  更改…
                </button>
              </div>
            )}
          </div>
        </div>
        <div className="modal__foot">
          <button className="btn btn--ghost" onClick={() => onDecide(false, '')}>
            <IconClose size={15} /> 拒绝
          </button>
          <button className="btn btn--primary" onClick={() => onDecide(true, target)}>
            <IconCheck size={15} /> 接受
          </button>
        </div>
      </div>
    </div>
  )
}

interface CodeProps {
  open: boolean
  title: string
  subtitle: string
  onSubmit: (code: string) => void
  onClose: () => void
  cta?: string
}

export function CodeModal({ open, title, subtitle, onSubmit, onClose, cta = '连接' }: CodeProps) {
  const [digits, setDigits] = useState<string[]>(['', '', '', '', '', ''])
  const refs = useRef<(HTMLInputElement | null)[]>([])

  useEffect(() => {
    if (open) {
      setDigits(['', '', '', '', '', ''])
      setTimeout(() => refs.current[0]?.focus(), 60)
    }
  }, [open])

  if (!open) return null

  const setDigit = (i: number, v: string) => {
    const clean = v.replace(/[^0-9a-zA-Z]/g, '').toUpperCase().slice(-1)
    const next = [...digits]
    next[i] = clean
    setDigits(next)
    if (clean && i < 5) refs.current[i + 1]?.focus()
  }

  const onKey = (i: number, e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Backspace' && !digits[i] && i > 0) refs.current[i - 1]?.focus()
    if (e.key === 'Enter' && digits.every(Boolean)) {
      onSubmit(digits.join(''))
    }
  }

  const code = digits.join('')

  return (
    <div className="overlay" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal__head">
          <div className="modal__title">{title}</div>
          <div className="modal__sub">{subtitle}</div>
        </div>
        <div className="modal__body">
          <div className="codebox">
            {digits.map((d, i) => (
              <input
                key={i}
                ref={(el) => {
                  refs.current[i] = el
                }}
                value={d}
                maxLength={1}
                inputMode="text"
                onChange={(e) => setDigit(i, e.target.value)}
                onKeyDown={(e) => onKey(i, e)}
                onPaste={(e) => {
                  e.preventDefault()
                  const txt = e.clipboardData.getData('text').toUpperCase().replace(/[^0-9A-Z]/g, '')
                  if (!txt) return
                  const next = [...digits]
                  for (let k = 0; k < 6 && k < txt.length; k++) next[k] = txt[k]
                  setDigits(next)
                  refs.current[Math.min(5, txt.length - 1)]?.focus()
                }}
              />
            ))}
          </div>
          <div className="hint" style={{ textAlign: 'center', marginTop: 10 }}>
            6 位取件码,不区分大小写
          </div>
        </div>
        <div className="modal__foot">
          <button className="btn btn--ghost" onClick={onClose}>
            取消
          </button>
          <button className="btn btn--primary" disabled={code.length !== 6} onClick={() => onSubmit(code)}>
            {cta}
          </button>
        </div>
      </div>
    </div>
  )
}
