import { useRef, useState } from 'react'
import { formatBytes } from '../types'
import { IconFile, IconTrash, IconUpload } from './Icons'

interface Props {
  files: File[]
  onFiles: (files: File[]) => void
  onRemove: (index: number) => void
  onSend: () => void
  targetName: string | null
  busy: boolean
}

export default function DropZone({ files, onFiles, onRemove, onSend, targetName, busy }: Props) {
  const [over, setOver] = useState(false)
  const input = useRef<HTMLInputElement>(null)

  const pick = (list: FileList | null) => {
    if (!list) return
    onFiles(Array.from(list))
  }

  return (
    <div className="stack">
      <div
        className={`drop${over ? ' drop--over' : ''}`}
        onClick={() => input.current?.click()}
        onDragOver={(e) => {
          e.preventDefault()
          setOver(true)
        }}
        onDragLeave={() => setOver(false)}
        onDrop={(e) => {
          e.preventDefault()
          setOver(false)
          pick(e.dataTransfer.files)
        }}
      >
        <input
          ref={input}
          type="file"
          multiple
          hidden
          onChange={(e) => {
            pick(e.target.files)
            e.target.value = ''
          }}
        />
        <div>
          <div className="drop__icon">
            <IconUpload size={26} />
          </div>
          <div className="drop__title">拖放文件到这里</div>
          <div className="drop__hint">或点击选择 · 支持多文件 · 大文件自动分块</div>
        </div>
      </div>

      {files.length > 0 && (
        <>
          <div className="filelist">
            {files.map((f, i) => (
              <div className="filerow" key={`${f.name}-${f.size}-${i}`}>
                <span style={{ color: 'var(--brand-3)', display: 'flex' }}>
                  <IconFile size={17} />
                </span>
                <div style={{ minWidth: 0, flex: 1 }}>
                  <div className="filerow__name">{f.name}</div>
                </div>
                <span className="filerow__size">{formatBytes(f.size)}</span>
                <button className="iconbtn" onClick={() => onRemove(i)} title="移除">
                  <IconTrash size={15} />
                </button>
              </div>
            ))}
          </div>

          <div className="row" style={{ justifyContent: 'flex-end' }}>
            <span className="hint" style={{ marginRight: 'auto' }}>
              {targetName ? `目标：${targetName}` : '请先在左侧选择接收方'}
            </span>
            <button className="btn btn--ghost" onClick={() => onFiles([])} disabled={busy}>
              清空
            </button>
            <button className="btn btn--primary" onClick={onSend} disabled={busy || !targetName}>
              {busy ? '传输中…' : `发送 ${files.length} 个文件`}
            </button>
          </div>
        </>
      )}
    </div>
  )
}
