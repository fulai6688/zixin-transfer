import { useEffect, useState } from 'react'
import type { Health, Settings } from '../types'
import { IconClose, IconFolder, IconRefresh } from './Icons'

interface Props {
  open: boolean
  settings: Settings | null
  health: Health | null
  local: boolean
  onSave: (dir: string) => Promise<void> | void
  onPick: (start: string) => Promise<string | null>
  onReveal: (path: string) => void
  onClose: () => void
}

export default function SettingsPanel({ open, settings, health, local, onSave, onPick, onReveal, onClose }: Props) {
  const [dir, setDir] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  useEffect(() => {
    if (open) {
      setDir(settings?.downloadDir ?? '')
      setErr('')
    }
  }, [open, settings])

  if (!open) return null

  const pick = async () => {
    setBusy(true)
    setErr('')
    try {
      const picked = await onPick(dir)
      if (picked) setDir(picked)
    } finally {
      setBusy(false)
    }
  }

  const save = async () => {
    setBusy(true)
    setErr('')
    try {
      await onSave(dir)
      onClose()
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="overlay" onClick={onClose}>
      <div className="modal" style={{ maxWidth: 560 }} onClick={(e) => e.stopPropagation()}>
        <div className="modal__head">
          <div className="modal__title">设置</div>
          <div className="modal__sub">接收文件默认保存在哪里</div>
        </div>

        <div className="modal__body">
          <div className="section-title" style={{ padding: '0 0 8px' }}>
            默认保存位置
          </div>
          <div className="row" style={{ gap: 8 }}>
            <input
              className="field"
              value={dir}
              onChange={(e) => setDir(e.target.value)}
              placeholder="D:\Downloads\子欣互传"
              spellCheck={false}
            />
            <button className="btn btn--ghost" onClick={pick} disabled={busy || !local} title={local ? '用系统对话框选择' : '只能在运行 子欣互传 的电脑上选择'}>
              <IconFolder size={15} /> 选择…
            </button>
          </div>
          <div className="hint" style={{ marginTop: 8 }}>
            也可以直接粘贴路径。保存时会自动建目录并验证可写。
          </div>

          <div className="row" style={{ gap: 8, marginTop: 10 }}>
            <button className="btn btn--sm btn--ghost" onClick={() => onReveal(dir)} disabled={!local}>
              在文件夹中打开
            </button>
            <button
              className="btn btn--sm btn--ghost"
              onClick={() => setDir(settings?.defaultDir ?? '')}
              title="恢复系统默认目录"
            >
              <IconRefresh size={14} /> 恢复默认
            </button>
            {!local && (
              <span className="hint" style={{ marginLeft: 'auto' }}>
                远程打开时不能选本机目录
              </span>
            )}
          </div>

          {settings && (
            <div className="hint" style={{ marginTop: 14 }}>
              系统默认：<span className="mono">{settings.defaultDir}</span>
              {settings.savedDir && settings.savedDir !== settings.defaultDir ? (
                <>
                  <br />
                  当前生效：<span className="mono">{settings.downloadDir}</span>
                </>
              ) : null}
            </div>
          )}

          {err && (
            <div className="hint" style={{ marginTop: 10, color: 'var(--danger)' }}>
              {err}
            </div>
          )}

          <div className="section-title" style={{ padding: '16px 0 8px', borderTop: '1px solid var(--line)', marginTop: 16 }}>
            关于
          </div>
          <div className="hint" style={{ lineHeight: 1.9 }}>
            版本 <span className="mono">{health?.version ?? '—'}</span>
            {health?.commit && health.commit !== 'none' ? <> · 提交 <span className="mono">{health.commit.slice(0, 7)}</span></> : null}
            {health?.builtAt && health.builtAt !== 'unknown' ? <> · 构建 <span className="mono">{health.builtAt}</span></> : null}
            <br />
            本机地址 <span className="mono">{health?.address ? `http://${health.address}:${health.port}` : '—'}</span>
            {health?.relay ? <><br />中转节点 <span className="mono">{health.relay}</span></> : null}
            <br />
            看左上角是否显示「子欣互传」，就知道浏览器有没有拿到新版本。
          </div>
        </div>

        <div className="modal__foot">
          <button className="btn btn--ghost" onClick={onClose}>
            <IconClose size={15} /> 取消
          </button>
          <button className="btn btn--primary" onClick={save} disabled={busy || !dir.trim()}>
            {busy ? '保存中…' : '保存'}
          </button>
        </div>
      </div>
    </div>
  )
}
