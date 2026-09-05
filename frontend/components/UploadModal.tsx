'use client'

import { useEffect, useRef, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import { MD5 } from '@/lib/md5'
import { fmtSize } from '@/lib/format'
import { useToast } from '@/components/Toast'
import { Icon } from '@/components/ui/Icon'

// 上传模态:本地文件(分片 + 断点续传)与视频链接两个通道。
// 分片大小与后端 config.yaml 的 upload.chunk_size 默认值(5MB)一致。

const CHUNK_SIZE = 5 * 1024 * 1024
const HASH_SLICE = 4 * 1024 * 1024

interface UploadRow {
  id: number
  name: string
  sizeLabel: string
  phase: 'hashing' | 'uploading' | 'merging' | 'done' | 'error'
  pct: number
  error?: string
}

export default function UploadModal({ onClose, onUploaded }: { onClose: () => void; onUploaded?: (taskId: number) => void }) {
  const toast = useToast()
  const [tab, setTab] = useState<'file' | 'url'>('file')
  const [rows, setRows] = useState<UploadRow[]>([])
  const [dragOver, setDragOver] = useState(false)
  const [url, setUrl] = useState('')
  const [urlBusy, setUrlBusy] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)
  const seq = useRef(0)
  const rowsRef = useRef<UploadRow[]>([])
  rowsRef.current = rows

  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onClose])

  const patchRow = (id: number, patch: Partial<UploadRow>) => {
    setRows(prev => prev.map(r => (r.id === id ? { ...r, ...patch } : r)))
  }

  async function uploadFile(file: File) {
    const id = ++seq.current
    setRows(prev => [...prev, {
      id, name: file.name, sizeLabel: fmtSize(file.size), phase: 'hashing', pct: 4,
    }])
    try {
      // 1) 计算真实 MD5(后端以其为分片会话键与资产去重键)
      const hasher = new MD5()
      for (let off = 0; off < file.size; off += HASH_SLICE) {
        const buf = await file.slice(off, Math.min(off + HASH_SLICE, file.size)).arrayBuffer()
        hasher.update(buf)
        patchRow(id, { pct: Math.min(4, Math.round((off / file.size) * 4)) })
      }
      const fileMd5 = hasher.digestHex()

      // 2) 询问服务端已收到的分片(断点续传;已完成资产则直接合并)
      const totalChunks = Math.max(1, Math.ceil(file.size / CHUNK_SIZE))
      const progress = await api.checkUpload(fileMd5, file.size, CHUNK_SIZE, totalChunks)
      const uploaded = new Set(progress.status === 'completed' ? [] : progress.uploaded)

      // 3) 顺序补传缺失分片
      for (let i = 0; i < totalChunks; i++) {
        if (uploaded.has(i)) continue
        const chunk = file.slice(i * CHUNK_SIZE, Math.min((i + 1) * CHUNK_SIZE, file.size))
        await api.uploadChunk(fileMd5, i, chunk)
        const doneCount = uploaded.size + [...Array(i + 1).keys()].filter(n => !uploaded.has(n)).length
        patchRow(id, { phase: 'uploading', pct: 4 + Math.round((doneCount / totalChunks) * 92) })
      }

      // 4) 合并建任务
      patchRow(id, { phase: 'merging', pct: 97 })
      const result = await api.mergeChunks({
        file_md5: fileMd5, filename: file.name, total_chunks: totalChunks, file_size: file.size, chunk_size: CHUNK_SIZE,
      })
      patchRow(id, { phase: 'done', pct: 100 })
      toast.success('上传完成,任务已创建并进入队列')
      onUploaded?.(result.task_id)
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : '上传失败'
      patchRow(id, { phase: 'error', error: msg })
      toast.error(msg)
    }
  }

  async function uploadUrl() {
    const u = url.trim()
    if (!u) { toast.info('先粘贴一个视频链接'); return }
    setUrlBusy(true)
    try {
      const r = await api.uploadUrl(u)
      toast.success('下载任务已创建,可关闭窗口继续工作')
      onUploaded?.(r.task_id)
      onClose()
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : 'URL 入库失败')
    } finally {
      setUrlBusy(false)
    }
  }

  const phaseText = (r: UploadRow) => {
    switch (r.phase) {
      case 'hashing': return '校验文件…'
      case 'uploading': return '上传中'
      case 'merging': return '合并分片…'
      case 'done': return '已上传,等待处理'
      case 'error': return r.error || '失败'
    }
  }

  return (
    <div className="overlay" onClick={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className="modal">
        <div className="modal-head">
          <h3>上传视频</h3>
          <button className="btn btn-ic btn-ghost" onClick={onClose} aria-label="关闭"><Icon name="x" /></button>
        </div>
        <div className="modal-body">
          <div className="seg" style={{ marginBottom: 14 }}>
            <button className={tab === 'file' ? 'on' : ''} onClick={() => setTab('file')}>本地文件</button>
            <button className={tab === 'url' ? 'on' : ''} onClick={() => setTab('url')}>视频链接</button>
          </div>

          {tab === 'file' ? (
            <div>
              <div
                className={`dropzone${dragOver ? ' over' : ''}`}
                onClick={() => fileRef.current?.click()}
                onDragOver={e => { e.preventDefault(); setDragOver(true) }}
                onDragLeave={() => setDragOver(false)}
                onDrop={e => {
                  e.preventDefault(); setDragOver(false)
                  for (const f of Array.from(e.dataTransfer.files)) void uploadFile(f)
                }}
              >
                <Icon name="upload" />
                <b>拖入视频文件,或点击选择</b>
                <span>分片上传,中断后重选同一文件可续传 · 单文件 2 GB 以内</span>
              </div>
              <input
                ref={fileRef}
                type="file"
                accept="video/*"
                multiple
                className="hidden"
                onChange={e => {
                  for (const f of Array.from(e.target.files || [])) void uploadFile(f)
                  e.target.value = ''
                }}
              />
              {rows.map(r => (
                <div className="uprow" key={r.id}>
                  <div className="un">
                    <b>{r.name}</b>
                    <span>{r.sizeLabel} · {phaseText(r)}</span>
                  </div>
                  <div className={`meter${r.phase === 'done' ? ' meter-ok' : ''}${r.phase === 'error' ? ' meter-err' : ''}`}>
                    <i style={{ width: `${r.pct}%` }} />
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div>
              <label className="field-label">视频页面链接</label>
              <input
                className="input"
                value={url}
                onChange={e => setUrl(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter' && !urlBusy) void uploadUrl() }}
                placeholder="https://www.bilibili.com/video/… 或任意可下载地址"
                autoFocus
              />
              <p className="field-help">服务端用 yt-dlp 拉取,下载阶段单独排队,失败可独立重试。</p>
              <button className="btn btn-primary" style={{ marginTop: 12 }} disabled={urlBusy} onClick={() => void uploadUrl()}>
                创建下载任务
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
