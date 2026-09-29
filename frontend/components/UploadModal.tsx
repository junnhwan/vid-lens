import { useEffect, useRef, useState } from 'react'
import { useRouter } from '@/lib/router'
import { api, ApiError } from '@/lib/api'
import { MD5 } from '@/lib/md5'
import { fmtSize } from '@/lib/format'
import { useToast } from '@/components/Toast'
import { Icon } from '@/components/ui/Icon'
import { Modal } from '@/components/ui/Modal'

// 上传模态:本地文件(分片 + 断点续传)与视频链接两个通道。
// 分片大小与后端 config.yaml 的 upload.chunk_size 默认值(5MB)一致。

const CHUNK_SIZE = 5 * 1024 * 1024
const HASH_SLICE = 4 * 1024 * 1024

interface UploadRow {
  id: number
  name: string
  sizeLabel: string
  phase: 'hashing' | 'checking' | 'uploading' | 'merging' | 'done' | 'error'
  pct: number
  error?: string
  taskId?: number
  alreadyProcessed?: boolean
}

export default function UploadModal({ onClose, onUploaded }: { onClose: () => void; onUploaded?: (taskId: number) => void }) {
  const toast = useToast()
  const router = useRouter()
  const [tab, setTab] = useState<'file' | 'url'>('file')
  const [rows, setRows] = useState<UploadRow[]>([])
  const [dragOver, setDragOver] = useState(false)
  const [url, setUrl] = useState('')
  const [urlBusy, setUrlBusy] = useState(false)
  const [urlImportEnabled, setURLImportEnabled] = useState<boolean | null>(null)
  const selectedImport = useRef(false)
  useEffect(() => {
    let active = true
    api.importOptions().then(options => { if (active) { setURLImportEnabled(options.url_import_enabled); if (options.url_import_enabled && !selectedImport.current) setTab('url') } })
      .catch(() => { if (active) setURLImportEnabled(false) })
    return () => { active = false }
  }, [])
  const fileRef = useRef<HTMLInputElement>(null)
  const seq = useRef(0)
  const transfers = useRef(new Set<AbortController>())
  useEffect(() => {
    const active = transfers.current
    return () => { for (const transfer of active) transfer.abort(); active.clear() }
  }, [])

  const patchRow = (id: number, patch: Partial<UploadRow>) => {
    setRows(prev => prev.map(r => (r.id === id ? { ...r, ...patch } : r)))
  }

  async function uploadFile(file: File) {
    const controller = new AbortController()
    const { signal } = controller
    transfers.current.add(controller)
    const id = ++seq.current
    setRows(prev => [...prev, {
      id, name: file.name, sizeLabel: fmtSize(file.size), phase: 'hashing', pct: 0,
    }])
    try {
      if (file.size === 0 || file.size > 2 * 1024 * 1024 * 1024) throw new ApiError(400, '请选择大小在 2 GB 以内的非空视频文件')
      // 1) 计算真实 MD5(后端以其为分片会话键与资产去重键)
      const hasher = new MD5()
      for (let off = 0; off < file.size; off += HASH_SLICE) {
        const buf = await file.slice(off, Math.min(off + HASH_SLICE, file.size)).arrayBuffer()
        signal.throwIfAborted()
        hasher.update(buf)
        patchRow(id, { pct: Math.round(Math.min(off + HASH_SLICE, file.size) / file.size * 4) })
      }
      const fileMd5 = hasher.digestHex()

      // 2) 询问服务端已收到的分片(断点续传;已完成资产则直接合并)
      const totalChunks = Math.max(1, Math.ceil(file.size / CHUNK_SIZE))
      patchRow(id, { phase: 'checking', pct: 4 })
      const progress = await api.checkUpload(fileMd5, file.size, CHUNK_SIZE, totalChunks, signal)
      signal.throwIfAborted()
      const uploaded = new Set(progress.uploaded.filter(n => Number.isInteger(n) && n >= 0 && n < totalChunks))
      let savedBytes = [...uploaded].reduce((sum, n) => sum + Math.min(CHUNK_SIZE, file.size - n * CHUNK_SIZE), 0)
      const showProgress = (currentBytes = 0) => patchRow(id, {
        phase: 'uploading', pct: 4 + Math.round(Math.min(1, (savedBytes + currentBytes) / file.size) * 92),
      })

      // 3) 顺序补传缺失分片
      if (progress.status !== 'completed') showProgress()
      for (let i = 0; progress.status !== 'completed' && i < totalChunks; i++) {
        if (uploaded.has(i)) continue
        signal.throwIfAborted()
        const chunk = file.slice(i * CHUNK_SIZE, Math.min((i + 1) * CHUNK_SIZE, file.size))
        await api.uploadChunk(fileMd5, i, chunk, { signal, onProgress: showProgress })
        signal.throwIfAborted()
        savedBytes += chunk.size
        showProgress()
      }

      // 4) 合并建任务
      patchRow(id, { phase: 'merging', pct: 97 })
      const result = await api.mergeChunks({
        file_md5: fileMd5, filename: file.name, total_chunks: totalChunks, file_size: file.size, chunk_size: CHUNK_SIZE,
      }, signal)
      signal.throwIfAborted()
      patchRow(id, { phase: 'done', pct: 100, taskId: result.task_id, alreadyProcessed: result.status === 3 })
      toast.success(result.status === 3 ? '文件已上传，已复用现有转写和摘要' : '文件已上传。请打开视频详情，点击“开始转写”才会启动处理')
      onUploaded?.(result.task_id)
    } catch (e) {
      if (signal.aborted) return
      const msg = e instanceof ApiError ? e.message : '上传失败'
      patchRow(id, { phase: 'error', error: msg })
      toast.error(msg)
    } finally {
      transfers.current.delete(controller)
    }
  }

  async function uploadUrl() {
    if (urlImportEnabled !== true) { toast.info('链接导入暂未开放，请上传本地视频文件'); return }
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
      case 'checking': return '检查续传进度…'
      case 'uploading': return `上传中 · ${r.pct}%`
      case 'merging': return '合并分片…'
      case 'done': return r.alreadyProcessed ? '文件已上传 · 已复用现有处理结果' : '文件已上传 · 转写尚未开始'
      case 'error': return r.error || '失败'
    }
  }

  const inFlight = urlBusy || rows.some(r => r.phase === 'hashing' || r.phase === 'checking' || r.phase === 'uploading' || r.phase === 'merging')

  return (
    <Modal
      title="导入视频"
      onClose={onClose}
      confirmOnClose={inFlight || !!url ? '关闭会中断未完成的上传,已填内容也会丢失。确定关闭?' : false}
    >
          <div className="seg" style={{ marginBottom: 14 }}>
            <button className={tab === 'file' ? 'on' : ''} onClick={() => { selectedImport.current=true; setTab('file') }}>本地文件</button>
            <button className={tab === 'url' ? 'on' : ''} disabled={urlImportEnabled !== true} style={urlImportEnabled !== true ? { opacity: 0.45, cursor: 'not-allowed' } : undefined} title={urlImportEnabled === false ? '链接导入暂未开放' : undefined} onClick={() => { selectedImport.current=true; setTab('url') }}>视频链接{urlImportEnabled === false ? ' · 暂未开放' : urlImportEnabled === null ? ' · 读取状态中' : ''}</button>
          </div>
          <p className="muted upload-helper">{tab === 'file' ? '上传速度取决于当前网络和服务器；文件合并完成前，请保持页面打开。' : '粘贴 B 站或 b23.tv 链接。任务创建后可以离开，下载完成后选择转写或画面分析。'}</p>

          {tab === 'file' ? (
            <div>
              <details className="upload-deploy-help"><summary>大文件上传与部署帮助</summary><p>较大文件可考虑在自己的电脑部署，减少远程传输。链接导入是否开放以当前服务设置为准。</p><a className="upload-guidance-link" href="https://github.com/junnhwan/vid-lens#技术栈与启动" target="_blank" rel="noopener noreferrer">查看部署说明<Icon name="chev-r" /></a></details>
              <div
                className={`dropzone${dragOver ? ' over' : ''}`}
                role="button"
                tabIndex={0}
                aria-label="选择本地视频"
                onKeyDown={e => { if(e.key === 'Enter' || e.key === ' ') { e.preventDefault(); fileRef.current?.click() } }}
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
                <span>单文件 2 GB 以内</span>
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
                    {r.taskId && <button className="btn btn-sm" style={{ marginTop: 4 }} onClick={() => { onClose(); router.push(`/video/${r.taskId}`) }}>{r.alreadyProcessed ? '查看视频' : '查看视频并开始转写'}</button>}
                  </div>
                  <div className={`meter${r.phase === 'done' ? ' meter-ok' : ''}${r.phase === 'error' ? ' meter-err' : ''}`}>
                    <i style={{ width: `${r.pct}%` }} />
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div>
              <label className="field-label">B 站视频链接</label>
              <input
                className="input"
                value={url}
                onChange={e => setUrl(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter' && !urlBusy) void uploadUrl() }}
                placeholder="https://www.bilibili.com/video/… 或 https://b23.tv/…"
                autoFocus
              />
              <button className={`btn btn-primary${urlBusy ? ' is-loading' : ''}`} aria-busy={urlBusy || undefined} style={{ marginTop: 12 }} disabled={urlBusy} onClick={() => void uploadUrl()}>
                创建下载任务
              </button>
            </div>
          )}
    </Modal>
  )
}
