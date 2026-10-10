import { useEffect, useRef, useState } from 'react'
import { useRouter } from '@/lib/router'
import { api, ApiError } from '@/lib/api'
import type { SummaryProcessingOptions } from '@/lib/types'
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
  file?: File
  autoSummary?: boolean
}

export default function UploadModal({ onClose, onUploaded }: { onClose: () => void; onUploaded?: (taskId: number) => void }) {
  const toast = useToast()
  const router = useRouter()
  const [tab, setTab] = useState<'file' | 'url'>('file')
  const [rows, setRows] = useState<UploadRow[]>([])
  const [dragOver, setDragOver] = useState(false)
  const [url, setUrl] = useState('')
  const [urlBusy, setUrlBusy] = useState(false)
  const [options,setOptions]=useState<SummaryProcessingOptions>({auto_summary:true,text_source_policy:'prefer_platform',preferred_language:'',summary_visual_enabled:true,output_mode:'auto',mindmap_enabled:true,summary_instruction:'',auto_tags_enabled:true})
  const receipts=useRef(new WeakMap<File,{key:string;options:SummaryProcessingOptions}>())
  const urlReceipt=useRef<{payload:string;key:string}|null>(null)
  const urlInFlight=useRef(false)
  const newKey=()=>crypto.randomUUID()
  const setOption=<K extends keyof SummaryProcessingOptions>(key:K,value:SummaryProcessingOptions[K])=>setOptions(previous=>({...previous,[key]:value}))
  const [urlImportEnabled, setURLImportEnabled] = useState<boolean | null>(null)
  const [slowUploadNotice, setSlowUploadNotice] = useState(false)
  const selectedImport = useRef(false)
  useEffect(() => {
    let active = true
    api.importOptions().then(options => {
      if (!active) return
      setURLImportEnabled(options.url_import_enabled)
      setSlowUploadNotice(options.slow_upload_notice)
      if (options.url_import_enabled && !options.slow_upload_notice && !selectedImport.current) setTab('url')
    })
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

  async function uploadFile(file: File,retryID?:number) {
    const controller = new AbortController()
    const { signal } = controller
    transfers.current.add(controller)
    const id = retryID??++seq.current
    const receipt=receipts.current.get(file)||{key:newKey(),options:{...options,text_source_policy:'force_asr' as const}}
    receipts.current.set(file,receipt)
    setRows(prev => [...prev.filter(row=>row.id!==id), {
      id, file, autoSummary:receipt.options.auto_summary,name: file.name, sizeLabel: fmtSize(file.size), phase: 'hashing', pct: 0,
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
        file_md5: fileMd5, filename: file.name, total_chunks: totalChunks, file_size: file.size, chunk_size: CHUNK_SIZE,...receipt.options,
      }, signal,receipt.key)
      signal.throwIfAborted()
      patchRow(id, { phase: 'done', pct: 100, taskId: result.task_id, alreadyProcessed: result.status === 3 })
      toast.success(receipt.options.auto_summary?'已导入，摘要处理会在后台继续':'文件已上传，可在视频详情中开始处理')
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
    if(urlInFlight.current)return
    urlInFlight.current=true
    const frozen={...options},payload=JSON.stringify({url:u,...frozen})
    if(urlReceipt.current?.payload!==payload)urlReceipt.current={payload,key:newKey()}
    const controller=new AbortController();transfers.current.add(controller)
    setUrlBusy(true)
    try {
      const r = await api.uploadUrl(u,frozen,urlReceipt.current!.key,controller.signal)
      if(controller.signal.aborted)return
      toast.success(frozen.auto_summary?'导入与摘要任务已创建，可以离开此页':'导入任务已创建，可以离开此页')
      onUploaded?.(r.task_id)
      onClose()
    } catch (e) {
      if(!controller.signal.aborted)toast.error(e instanceof ApiError ? e.message : 'URL 入库失败')
    } finally {
      transfers.current.delete(controller);urlInFlight.current=false
      if(!controller.signal.aborted)setUrlBusy(false)
    }
  }

  const phaseText = (r: UploadRow) => {
    switch (r.phase) {
      case 'hashing': return '校验文件…'
      case 'checking': return '检查续传进度…'
      case 'uploading': return `上传中 · ${r.pct}%`
      case 'merging': return '合并分片…'
      case 'done': return r.autoSummary?'已导入 · 摘要在后台处理':r.alreadyProcessed ? '文件已上传 · 已复用现有处理结果' : '文件已上传 · 可开始处理'
      case 'error': return r.error || '失败'
    }
  }

  let partNotice=''
  try {const parsed=new URL(url.trim());if(/(^|\.)bilibili\.com$/.test(parsed.hostname)){const part=parsed.searchParams.get('p')||'1';partNotice=/^[1-9][0-9]*$/.test(part)?`仅导入第 ${part} P；服务端会核对这段的媒体和字幕。`:'分 P 参数无效，请填写正整数。'}else if(parsed.hostname==='b23.tv')partNotice='短链会由服务端解析，并保留它指定的分 P。'}catch{/* Input may still be incomplete. */}
  const inFlight = urlBusy || rows.some(r => r.phase === 'hashing' || r.phase === 'checking' || r.phase === 'uploading' || r.phase === 'merging')

  return (
    <Modal
      title="导入并生成摘要"
      onClose={onClose}
      confirmOnClose={inFlight || !!url ? '关闭会中断未完成的上传,已填内容也会丢失。确定关闭?' : false}
    >
          <div className="seg" style={{ marginBottom: 14 }}>
            <button className={tab === 'file' ? 'on' : ''} onClick={() => { selectedImport.current=true; setTab('file') }}>本地文件</button>
            <button className={tab === 'url' ? 'on' : ''} disabled={urlImportEnabled !== true} style={urlImportEnabled !== true ? { opacity: 0.45, cursor: 'not-allowed' } : undefined} title={urlImportEnabled === false ? '链接导入暂未开放' : undefined} onClick={() => { selectedImport.current=true; setTab('url') }}>视频链接{urlImportEnabled === false ? ' · 暂未开放' : urlImportEnabled === null ? ' · 读取状态中' : ''}</button>
          </div>
          {(!slowUploadNotice || tab === 'url') && <p className="muted upload-helper">{tab === 'file' ? '上传速度取决于当前网络和服务器；文件合并完成前，请保持页面打开。' : '粘贴 B 站或 b23.tv 链接。任务创建后可以离开，系统会确认分 P，再读取字幕或转写并生成摘要。'}</p>}

          <label className="field-label" style={{marginBottom:12}}>关注点（可选）<textarea className="input" rows={2} disabled={inFlight} value={options.summary_instruction} onChange={event=>{const text=event.target.value;if(Array.from(text).length<=2000)setOption('summary_instruction',text)}} placeholder="例如：关注核心结论、操作步骤和适用条件" /></label>
          <details className="upload-summary-options" style={{marginBottom:16}}><summary>高级处理选项</summary><fieldset disabled={inFlight} style={{border:0,display:'grid',gap:12,paddingTop:12}}>
            <label><input type="checkbox" checked={options.auto_summary} onChange={event=>setOption('auto_summary',event.target.checked)} /> 导入后自动生成摘要</label>
            <label>文字来源<select className="input" value={tab==='file'?'force_asr':options.text_source_policy} disabled={tab==='file'} onChange={event=>setOption('text_source_policy',event.target.value as SummaryProcessingOptions['text_source_policy'])}><option value="prefer_platform">优先平台字幕，不可用时转写</option><option value="force_asr">直接语音转写</option></select></label>
            <label>字幕语言<select className="input" value={options.preferred_language} onChange={event=>setOption('preferred_language',event.target.value)}><option value="">自动选择</option><option value="zh">中文</option><option value="en">英语</option></select></label>
            <label><input type="checkbox" checked={options.summary_visual_enabled} onChange={event=>{const enabled=event.target.checked;setOptions(previous=>({...previous,summary_visual_enabled:enabled,output_mode:!enabled?'text':previous.output_mode}))}} /> 允许查看必要画面并配图</label>
            <label>摘要形式<select className="input" value={options.output_mode} onChange={event=>setOption('output_mode',event.target.value as SummaryProcessingOptions['output_mode'])}><option value="auto">自动选择</option><option value="text">文字摘要</option><option value="image_text" disabled={!options.summary_visual_enabled}>图文摘要</option><option value="keyframes" disabled={!options.summary_visual_enabled}>关键帧摘要</option></select></label>
            <label><input type="checkbox" checked={options.mindmap_enabled} onChange={event=>setOption('mindmap_enabled',event.target.checked)} /> 提供摘要结构与导图</label>
            <label><input type="checkbox" checked={options.auto_tags_enabled} onChange={event=>setOption('auto_tags_enabled',event.target.checked)} /> 自动分类标签</label>
          </fieldset></details>
          {tab === 'file' ? (
            <div>
              {slowUploadNotice && <div className="upload-guidance upload-guidance-online" role="note">
                <div className="upload-guidance-row">
                  <span className="upload-guidance-icon"><Icon name="clock" /></span>
                  <div className="upload-guidance-copy">
                    <h4>线上文件上传较慢</h4>
                    <p>当前线上实例上传大文件耗时较长。{urlImportEnabled === true && 'B 站视频建议优先通过链接导入。'}本地文件仍可上传，请保持页面打开直到合并完成。</p>
                    {urlImportEnabled === true && <button type="button" className="btn btn-primary btn-sm upload-guidance-cta" onClick={() => { selectedImport.current = true; setTab('url') }}><Icon name="link" />使用 B 站链接</button>}
                  </div>
                </div>
              </div>}
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
                    {r.taskId && <button className="btn btn-sm" style={{ marginTop: 4 }} onClick={() => { onClose(); router.push(`/video/${r.taskId}`) }}>{r.autoSummary?'打开摘要':r.alreadyProcessed ? '查看视频' : '查看视频并开始处理'}</button>}
                  </div>
                  {r.phase==='error'&&r.file&&<button className="btn btn-sm" onClick={()=>void uploadFile(r.file!,r.id)}>继续上传</button>}
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
                disabled={urlBusy}
                onChange={e => setUrl(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter' && !urlBusy) void uploadUrl() }}
                placeholder="https://www.bilibili.com/video/… 或 https://b23.tv/…"
                autoFocus
              />
              {partNotice&&<p className="muted" role="status" style={{marginTop:8}}>{partNotice}</p>}
              <button className={`btn btn-primary${urlBusy ? ' is-loading' : ''}`} aria-busy={urlBusy || undefined} style={{ marginTop: 12 }} disabled={urlBusy} onClick={() => void uploadUrl()}>
                {options.auto_summary?'导入并生成摘要':'仅导入视频'}
              </button>
            </div>
          )}
    </Modal>
  )
}
