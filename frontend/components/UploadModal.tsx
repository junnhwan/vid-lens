'use client'

import { useState, useRef, useEffect } from 'react'
import { X, UploadCloud, Link2, Loader2 } from 'lucide-react'
import { api, ApiError } from '@/lib/api'

export default function UploadModal({ onClose, onUploaded }: { onClose: () => void; onUploaded: (taskId: number) => void }) {
  const [tab, setTab] = useState<'file' | 'url'>('file')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [dragOver, setDragOver] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)
  const [url, setUrl] = useState('')

  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onClose])

  const doFile = async (file: File) => {
    setBusy(true); setErr('')
    try {
      const r = await api.uploadFile(file)
      onUploaded(r.task_id)
      onClose()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '上传失败')
    } finally { setBusy(false) }
  }
  const doUrl = async () => {
    if (!url.trim()) { setErr('请输入视频 URL'); return }
    setBusy(true); setErr('')
    try {
      const r = await api.uploadUrl(url.trim())
      onUploaded(r.task_id)
      onClose()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'URL 入库失败')
    } finally { setBusy(false) }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-stone-900/40 ui-backdrop" onClick={onClose}>
      <div className="bg-white border border-stone-200 rounded-xl w-full max-w-md ui-modal-in shadow-xl" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center px-5 h-12 border-b border-stone-200">
          <div className="text-[14px] font-medium text-stone-900">上传视频</div>
          <button onClick={onClose} className="ml-auto w-7 h-7 rounded-lg flex items-center justify-center text-stone-400 hover:text-stone-700 hover:bg-stone-100">
            <X className="w-4 h-4" />
          </button>
        </div>
        <div className="flex border-b border-stone-200 px-5">
          {(['file', 'url'] as const).map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={`py-2.5 mr-5 text-[12px] border-b-2 -mb-px transition-colors ${
                tab === t ? 'border-amber-600 text-stone-900 font-medium' : 'border-transparent text-stone-400'
              }`}
            >
              {t === 'file' ? '文件上传' : 'URL 下载'}
            </button>
          ))}
        </div>
        <div className="p-5">
          {tab === 'file' ? (
            <div
              onDragOver={(e) => { e.preventDefault(); setDragOver(true) }}
              onDragLeave={() => setDragOver(false)}
              onDrop={(e) => {
                e.preventDefault(); setDragOver(false)
                const f = e.dataTransfer.files?.[0]
                if (f) doFile(f)
              }}
              onClick={() => fileRef.current?.click()}
              className={`border border-dashed rounded-xl h-40 flex flex-col items-center justify-center gap-2 cursor-pointer transition-colors ${
                dragOver ? 'border-amber-500 bg-amber-50' : 'border-stone-300 hover:border-amber-400 hover:bg-amber-50/50'
              }`}
            >
              {busy ? <Loader2 className="w-6 h-6 text-stone-400 animate-spin" /> : <UploadCloud className="w-6 h-6 text-stone-400" />}
              <div className="text-[13px] text-stone-600">{busy ? '上传中…' : '拖拽文件到此处，或点击选择'}</div>
              <div className="text-[11px] text-stone-400">支持 mp4 / mov / mkv / webm 等常见格式</div>
              <input ref={fileRef} type="file" accept="video/*" className="hidden" onChange={(e) => { const f = e.target.files?.[0]; if (f) doFile(f) }} />
            </div>
          ) : (
            <div className="space-y-3">
              <div className="flex items-center gap-2 h-10 px-3 rounded-lg border border-stone-200 bg-white focus-within:ring-2 focus-within:ring-amber-600/20 focus-within:border-amber-400">
                <Link2 className="w-3.5 h-3.5 text-stone-400" />
                <input
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                  onKeyDown={(e) => { if (e.key === 'Enter' && !busy) doUrl() }}
                  placeholder="https://www.youtube.com/watch?v=… 或直链"
                  className="w-full bg-transparent font-mono text-[12.5px] text-stone-800 placeholder:text-stone-400"
                  autoFocus
                />
              </div>
              <p className="text-[11px] text-stone-400">后端用 yt-dlp 下载，支持主流视频站点。</p>
              <button
                onClick={doUrl}
                disabled={busy}
                className="h-8 px-3.5 rounded-lg bg-stone-900 text-white text-[12px] font-medium flex items-center gap-1.5 ui-btn-lift disabled:opacity-50"
              >
                {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Link2 className="w-3.5 h-3.5" />} 提交
              </button>
            </div>
          )}
          {err && <div className="mt-3 text-[12px] text-red-600">{err}</div>}
        </div>
      </div>
    </div>
  )
}
