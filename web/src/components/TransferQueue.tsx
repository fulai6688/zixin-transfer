import { formatBytes, formatSpeed, type ReceivedFile, type Transfer } from '../types'
import { IconChevron, IconClose, IconExternal, IconFile, IconFolder } from './Icons'

interface Props {
  txs: Transfer[]
  received: ReceivedFile[]
  open: boolean
  local: boolean
  onToggle: () => void
  onReveal: (path: string) => void
  onOpenFile: (path: string) => void
}

export default function TransferQueue({ txs, received, open, local, onToggle, onReveal, onOpenFile }: Props) {
  const active = txs.filter((t) => t.state === 'active' || t.state === 'queued').length
  return (
    <div className="queue">
      <div className="queue__head" onClick={onToggle}>
        <span
          style={{
            display: 'flex',
            transform: open ? 'rotate(180deg)' : 'none',
            transition: 'transform 200ms cubic-bezier(.22,1,.36,1)',
            color: 'var(--text-mute)',
          }}
        >
          <IconChevron size={16} />
        </span>
        <span className="queue__title">传输队列</span>
        <span className="pill" style={{ height: 22, fontSize: 11.5 }}>
          {active > 0 ? `${active} 进行中` : `${txs.length} 条记录`}
        </span>
        <span className="spacer" />
      </div>

      {open && (
        <div className="queue__list">
          {txs.length === 0 && received.length === 0 && (
            <div style={{ color: 'var(--text-mute)', fontSize: 12.5, padding: '6px 8px 10px' }}>
              还没有传输记录
            </div>
          )}
          {txs.map((t) => {
            const pct = t.size > 0 ? Math.min(100, (t.sent / t.size) * 100) : 0
            return (
              <div className="tx" key={t.id}>
                <span
                  style={{
                    color:
                      t.state === 'done'
                        ? 'var(--ok)'
                        : t.state === 'error' || t.state === 'rejected'
                          ? 'var(--danger)'
                          : 'var(--brand-3)',
                    display: 'flex',
                  }}
                >
                  <IconClose
                    size={15}
                    className={t.state === 'done' ? '' : 'rot'}
                  />
                </span>
                <div style={{ minWidth: 0 }}>
                  <div className="tx__name">{t.name}</div>
                  <div className="tx__meta">
                    <span>{t.direction === 'send' ? '发送' : '接收'}</span>
                    <span>·</span>
                    <span>{formatBytes(t.sent)} / {formatBytes(t.size)}</span>
                    {t.state === 'active' && (
                      <>
                        <span>·</span>
                        <span>{formatSpeed(t.speed)}</span>
                      </>
                    )}
                    {t.peer && (
                      <>
                        <span>·</span>
                        <span>{t.peer}</span>
                      </>
                    )}
                    {t.error && (
                      <>
                        <span>·</span>
                        <span style={{ color: 'var(--danger)' }}>{t.error}</span>
                      </>
                    )}
                  </div>
                  <div className="bar">
                    <div
                      className={`bar__fill${t.state === 'done' ? ' bar__fill--done' : ''}${
                        t.state === 'error' || t.state === 'rejected' ? ' bar__fill--err' : ''
                      }`}
                      style={{ width: `${t.state === 'done' ? 100 : pct}%` }}
                    />
                  </div>
                </div>
                <span className="tx__meta">{t.channel ?? '—'}</span>
              </div>
            )
          })}

          {received.length > 0 && (
            <div className="queue__section">已接收（本机下载目录）</div>
          )}
          {received.slice(0, 12).map((f) => (
            <div className="tx" key={`${f.path}-${f.receivedAt}`}>
              <span style={{ color: 'var(--ok)', display: 'flex' }}>
                <IconFile size={15} />
              </span>
              <div style={{ minWidth: 0, flex: 1 }}>
                <div className="tx__name">{f.name}</div>
                <div className="tx__meta">
                  <span>接收</span>
                  <span>·</span>
                  <span>{formatBytes(f.size)}</span>
                  <span>·</span>
                  <span>{f.from || '对端'}</span>
                  <span>·</span>
                  <span className="mono">{new Date(f.receivedAt).toLocaleTimeString()}</span>
                </div>
                <div className="tx__meta mono" style={{ opacity: 0.75 }} title={f.path}>
                  {f.path}
                </div>
              </div>
              <div className="row" style={{ gap: 6 }}>
                <button
                  className="iconbtn"
                  title={local ? '打开文件' : '只能在运行 子欣互传 的电脑上打开'}
                  disabled={!local}
                  onClick={() => onOpenFile(f.path)}
                >
                  <IconExternal size={14} />
                </button>
                <button
                  className="iconbtn"
                  title={local ? '在文件夹中显示' : '只能在运行 子欣互传 的电脑上显示'}
                  disabled={!local}
                  onClick={() => onReveal(f.path)}
                >
                  <IconFolder size={14} />
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
