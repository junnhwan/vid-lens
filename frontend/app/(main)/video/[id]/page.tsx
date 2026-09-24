'use client'

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useRouter } from 'next/navigation'
import Image from 'next/image'
import { api, ApiError } from '@/lib/api'
import {
  TaskStatusEnum,
  type RAGIndexResult,
  type TimelineAtom,
  type VideoTask,
  type VideoTimeline,
} from '@/lib/types'
import { fmtRelTime, taskTitle } from '@/lib/format'
import { formatTime } from '@/components/Citation'
import { ModalityTag } from '@/components/ui/ModalityTag'
import { VideoPlayer, type VideoPlayerHandle } from '@/components/player/VideoPlayer'
import { MarkdownAnswer } from '@/components/chat/MarkdownAnswer'
import { useCrumb, useShell } from '@/components/shell/AppShell'
import { useToast } from '@/components/Toast'
import { Icon } from '@/components/ui/Icon'
import { ConfirmModal, Modal } from '@/components/ui/Modal'
import KBModal from '@/components/KBModal'
import { expandTranscript } from '@/lib/transcript'
import { ProcessStrip } from '@/components/ProcessStrip'
import { TranscriptionProgressPanel } from '@/components/TranscriptionProgressPanel'
import { taskStateView } from '@/lib/taskStatus'
import { summaryFailureView } from '@/lib/summaryFailure'
import { VideoStill } from '@/components/VideoPoster'

// 视频工作台:播放器钉住 + 右栏时间轴/画面/索引。摘要走阅读弹窗。
// 对应原型 #/video/:id。播放源用 /playback 签名 URL;时间轴/画面证据来自
// /timeline 的原子(转写、OCR、画面描述同轨)。画面卡片读取对应已保存帧；
// 点击卡片仍按该帧时间跳转播放器。

type TabKey = 'tl' | 'vf' | 'idx'
type ActionKind = 'transcribe' | 'analyze' | 'index' | 'download'
type ConfirmAction = {
  kind: Exclude<ActionKind, 'download'>
  force?: boolean
  title: string
  body: string
  confirmLabel: string
}

function indexActionLabel(index: RAGIndexResult | null): string {
  if (!index) return '索引状态不可用'
  if (index.status === 'queued') return '索引排队中…'
  if (index.status === 'indexing') return '索引构建中…'
  if (index.status === 'failed') return '索引失败，重试'
  if (index.status === 'needs_rebuild' || index.needs_rebuild) return '需要重建索引'
  return index.indexed ? '重建索引' : '建立索引'
}

function indexConfirm(index: RAGIndexResult): ConfirmAction {
  const label = indexActionLabel(index)
  const replacing = index.indexed || index.needs_rebuild || index.status === 'needs_rebuild'
  return {
    kind: 'index', title: `${label}？`, confirmLabel: label,
    body: `建立后，视频问答可按内容含义找到相关片段并定位视频位置；只播放视频、查看转写或摘要无需建立索引。系统会将已有转写文字及已生成的画面文字、画面描述（如有）发送给当前配置的向量模型，调用 Embedding 并消耗额度；不会重新转写，也不会修改原视频或这些文字。${replacing ? '现有检索索引将被替换。' : ''}`,
  }
}

function indexPhase(index: RAGIndexResult): string {
  if (index.status === 'queued') return '等待索引任务启动'
  if (index.status === 'failed') return '索引失败'
  if (index.status === 'indexed') return '已完成'
  if (index.status === 'needs_rebuild') return '等待重建'
  if (index.status === 'not_indexed') return '尚未建立'
  const phases: Record<string, string> = {
    preparing: '准备文本块', embedding: '调用 Embedding 模型',
    waiting: index.wait_reason === 'local_admission' ? '等待本地模型额度' : index.wait_reason === 'provider_rate_limit' ? '等待第三方模型限流重试' : '等待重试',
    writing: '写入向量', completed: '已完成',
  }
  return phases[index.build_phase] || '等待进度更新'
}

interface VisualFrameView {
  key: string
  frameId?: number
  timeMs: number
  endMs: number
  ocr?: string
  caption?: string
  hasOcr: boolean
  hasCaption: boolean
}

function visualTipSections(text: string): { label: string; body: string }[] {
  const raw = text.trim()
  if (!raw) return []
  const chunks = raw.split(/(?=\d+\)\s*)/).map(s => s.trim()).filter(Boolean)
  if (chunks.length > 1) {
    return chunks.map(chunk => {
      const m = chunk.match(/^\d+\)\s*([^:：\n]+)[:：]?\s*([\s\S]*)$/)
      if (m) return { label: m[1].trim(), body: m[2].trim() }
      return { label: '', body: chunk }
    })
  }
  return [{ label: '', body: raw }]
}

function groupVisualAtoms(atoms: TimelineAtom[]): VisualFrameView[] {
  const map = new Map<string, VisualFrameView>()
  for (const atom of atoms) {
    if (atom.modality !== 'visual_ocr' && atom.modality !== 'visual_caption') continue
    const key = atom.source_refs?.[0]?.stable_id || atom.id
    let view = map.get(key)
    if (!view) {
      view = { key, frameId: atom.source_refs?.[0]?.source_row_id, timeMs: atom.start_ms, endMs: atom.end_ms, hasOcr: false, hasCaption: false }
      map.set(key, view)
    }
    view.timeMs = Math.min(view.timeMs, atom.start_ms)
    view.endMs = Math.max(view.endMs, atom.end_ms)
    if (atom.modality === 'visual_ocr') {
      view.ocr = atom.content
      view.hasOcr = true
    } else {
      view.caption = atom.content
      view.hasCaption = true
    }
  }
  return [...map.values()].sort((a, b) => a.timeMs - b.timeMs)
}

function FramePreview({ src, timeMs }: { src: string | null; timeMs: number }) {
  const [failed, setFailed] = useState(false)
  return src && !failed
    ? <Image src={src} alt={`${formatTime(timeMs)} 的已保存画面帧`} fill sizes="(max-width: 900px) 100vw, 300px" unoptimized onError={() => setFailed(true)} style={{ objectFit: 'cover' }} />
    : <div className="muted" role="status" style={{ padding: 12, fontSize: 12 }}>帧预览不可用</div>
}

function FrameRead({ children }: { children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [canToggle, setCanToggle] = useState(false)

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const measure = () => {
      if (open) return
      setCanToggle(el.scrollHeight > el.clientHeight + 2)
    }
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [children, open])

  return (
    <div>
      <div ref={ref} className={`frame-read${open ? ' open' : ''}`}>{children}</div>
      {canToggle && (
        <button
          type="button"
          className="frame-more"
          onClick={e => { e.stopPropagation(); setOpen(v => !v) }}
        >
          {open ? '收起' : '展开'}
        </button>
      )}
    </div>
  )
}

export default function VideoWorkbenchPage({ params, searchParams }: { params: { id: string }; searchParams?: { t?: string } }) {
  const taskId = Number(params.id)
  const router = useRouter()
  const toast = useToast()
  const { user } = useShell()
  const readOnly = user?.role === 'DEMO'
  const playerRef = useRef<VideoPlayerHandle>(null)
  const prevTransRef = useRef(false)

  const [task, setTask] = useState<VideoTask | null>(null)
  const [timeline, setTimeline] = useState<VideoTimeline | null>(null)
  const [index, setIndex] = useState<RAGIndexResult | null>(null)
  const [playbackUrl, setPlaybackUrl] = useState<string | null>(null)
  const [videoDurationMs, setVideoDurationMs] = useState(0)
  const [visualSettingBusy, setVisualSettingBusy] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [tab, setTab] = useState<TabKey>('tl')
  const [seen, setSeen] = useState<Record<TabKey, boolean>>({ tl: true, vf: false, idx: false })
  const openTab = (key: TabKey) => {
    setTab(key)
    setSeen(s => (s[key] ? s : { ...s, [key]: true }))
  }
  const [playheadMs, setPlayheadMs] = useState(0)
  const [busy, setBusy] = useState<ActionKind | ''>('')
  const [headSnap, setHeadSnap] = useState(false)
  const [summaryOpen, setSummaryOpen] = useState(false)
  const [railTip, setRailTip] = useState<{ left: number; text: string; timeMs: number } | null>(null)
  const liveRowRef = useRef<HTMLDivElement>(null)
  const [editingTitle, setEditingTitle] = useState(false)
  const [titleDraft, setTitleDraft] = useState('')
  const [titleBusy, setTitleBusy] = useState(false)
  const [kbOpen, setKbOpen] = useState(false)
  const [pendingAction, setPendingAction] = useState<ConfirmAction | null>(null)

  useCrumb([
    { label: '视频库', href: '/library' },
    { label: task ? taskTitle(task) : `视频 #${taskId}` },
  ])

  useEffect(() => {
    let active = true
    setLoading(true)
    setLoadError('')
    setTask(null)
    setTimeline(null)
    setIndex(null)
    setPlaybackUrl(null)
    setVideoDurationMs(0)
    setPlayheadMs(0)
    void (async () => {
      let detail: VideoTask
      try {
        detail = await api.getTask(taskId)
      } catch (e) {
        if (!active) return
        setLoadError(e instanceof ApiError ? e.message : '视频详情加载失败')
        setLoading(false)
        return
      }
      if (!active) return
      setTask(detail)
      prevTransRef.current = detail.has_transcription
      setLoading(false)
      const [tl, idx, playback] = await Promise.all([
        api.getTimeline(taskId).catch(() => null),
        api.getRagIndex(taskId).catch(() => null),
        api.playbackSrc(taskId).catch(() => null),
      ])
      if (!active) return
      setTimeline(tl)
      setIndex(idx)
      setPlaybackUrl(playback)
    })()
    return () => { active = false }
  }, [taskId])

  const processing = !!task && (task.status === TaskStatusEnum.Queued || task.status === TaskStatusEnum.Running)
  const awaitingSummaryRetry = !!task && !!summaryFailureView(task)?.scheduled

  // 处理中或等待摘要自动重试时轮询；重试调度会清除 next_retry_at 并重新入队。
  useEffect(() => {
    if (!processing && !awaitingSummaryRetry) return
    const iv = setInterval(() => {
      void (async () => {
        try {
          const fresh = await api.getTask(taskId)
          const prev = prevTransRef.current
          prevTransRef.current = fresh.has_transcription
          setTask(fresh)
          const transJustDone = !prev && fresh.has_transcription
          const justCompleted = fresh.status === TaskStatusEnum.Completed
          if (transJustDone || justCompleted) {
            const [tl, idx] = await Promise.all([
              api.getTimeline(taskId).catch(() => null),
              api.getRagIndex(taskId).catch(() => null),
            ])
            setTimeline(tl)
            setIndex(idx)
          }
        } catch { /* 下个周期重试 */ }
      })()
    }, 5000)
    return () => clearInterval(iv)
  }, [processing, awaitingSummaryRetry, taskId])

  useEffect(() => {
    if (!processing && busy !== 'index' && index?.status !== 'indexing' && index?.status !== 'queued') return
    const iv = setInterval(() => { void api.getRagIndex(taskId).then(setIndex).catch(() => {}) }, 5000)
    return () => clearInterval(iv)
  }, [processing, busy, index?.status, taskId])

  const transcriptAtoms = useMemo(
    () => (timeline?.atoms || []).filter(a => a.modality === 'transcript'),
    [timeline],
  )
  const transcriptRows = useMemo(() => expandTranscript(transcriptAtoms), [transcriptAtoms])
  const visualAtoms = useMemo(
    () => (timeline?.atoms || []).filter(a => a.modality === 'visual_ocr' || a.modality === 'visual_caption'),
    [timeline],
  )
  const frames = useMemo(() => groupVisualAtoms(timeline?.atoms || []), [timeline])
  const timelineMs = useMemo(
    () => (timeline?.atoms || []).reduce((max, a) => Math.max(max, a.end_ms, a.start_ms), 0),
    [timeline],
  )

  const seek = useCallback((ms: number, autoplay = true, cue?: string) => {
    playerRef.current?.seek(ms, autoplay, cue)
    setPlayheadMs(ms)
    setHeadSnap(true)
    window.setTimeout(() => setHeadSnap(false), 280)
  }, [])

  // 播放地址现在是站内路径 + 任务级凭证,不再因 5 分钟签名到期而失效;
  // 这里仅在加载失败时重取一次,用于凭证过期或对象稍后才可用的情形。
  const refreshPlaybackUrl = useCallback(async () => {
    try {
      const src = await api.playbackSrc(taskId)
      if (src) {
        setPlaybackUrl(src)
        return src
      }
    } catch { /* 保持失败态 */ }
    return null
  }, [taskId, setPlaybackUrl])

  const liveIndex = transcriptRows.findIndex(
    a => playheadMs >= a.start_ms && playheadMs < Math.max(a.end_ms, a.start_ms + 1),
  )

  useEffect(() => {
    const el = liveRowRef.current
    if (!el) return
    const root = el.closest('.rail-pane')
    if (!(root instanceof HTMLElement)) return
    const rootBox = root.getBoundingClientRect()
    const box = el.getBoundingClientRect()
    if (box.top < rootBox.top + 12 || box.bottom > rootBox.bottom - 12) {
      el.scrollIntoView({ block: 'center', behavior: 'smooth' })
    }
  }, [liveIndex])

  const runAction = async (kind: Exclude<ActionKind, 'download'>, force = false) => {
    if (!task || busy) return
    setBusy(kind)
    try {
      if (kind === 'transcribe') {
        await api.transcribe(task.id, force)
        toast.success(force ? '重新转写已排队，将再次调用语音识别' : '转写任务已排队，等待并发名额')
      } else if (kind === 'analyze') {
        await api.analyze(task.id, force)
        toast.success('摘要任务已加入队列,完成后会出现在这里')
      } else {
        const r = await api.triggerRagIndex(task.id)
        setIndex(r)
        if (r.status === 'indexed') toast.success('索引构建完成')
        else toast.info(r.status === 'queued' ? '索引任务正在排队' : '索引正在构建中')
      }
      const fresh = await api.getTask(task.id).catch(() => null)
      if (fresh) {
        prevTransRef.current = fresh.has_transcription
        setTask(fresh)
      }
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '操作失败')
    } finally {
      setBusy('')
    }
  }

  const downloadAudio = async () => {
    if (!task || busy) return
    setBusy('download')
    try {
      const r = await api.downloadAudio(task.id)
      const a = document.createElement('a')
      a.href = r.download_url
      a.download = r.filename || ''
      document.body.appendChild(a)
      a.click()
      a.remove()
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '获取下载链接失败')
    } finally {
      setBusy('')
    }
  }

  const saveTitle = async () => {
    if (!task || titleBusy) return
    const next = titleDraft.trim()
    if (!next) { toast.info('标题不能为空'); return }
    setTitleBusy(true)
    try {
      const fresh = await api.updateTaskTitle(task.id, next)
      setTask(fresh)
      setEditingTitle(false)
      toast.success('标题已更新')
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '保存标题失败')
    } finally {
      setTitleBusy(false)
    }
  }

  const confirmAndRun = async () => {
    if (!pendingAction || busy) return
    const { kind, force } = pendingAction
    setPendingAction(null)
    await runAction(kind, force)
  }

  const setVisualDisabled = async (disabled: boolean) => {
    if (!task || visualSettingBusy) return
    setVisualSettingBusy(true)
    try {
      setTask(await api.setTaskVisualDisabled(task.id, disabled))
      toast.success(disabled ? '已关闭此视频后续画面证据生成' : '已开启此视频后续画面证据生成')
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '画面证据设置保存失败')
    } finally {
      setVisualSettingBusy(false)
    }
  }

  if (loading) {
    return <div className="page"><div className="empty"><b>加载中…</b></div></div>
  }
  if (loadError || !task) {
    return (
      <div className="page">
        <div className="card">
          <div className="empty">
            <Icon name="alert" size="lg" />
            <b>视频加载失败</b>
            <p>{loadError || '任务不存在'}</p>
            <button className="btn btn-sm" style={{ marginTop: 8 }} onClick={() => router.push('/library')}>
              <Icon name="chev-l" size="sm" />返回视频库
            </button>
          </div>
        </div>
      </div>
    )
  }

  const failed = task.status === TaskStatusEnum.Failed || task.status === TaskStatusEnum.Dead
  const summaryFailure = summaryFailureView(task)
  const urlJob = failed && task.last_job_type === 'download'
  const title = taskTitle(task)

  const durPct = timelineMs > 0 ? timelineMs : 1
  const hasRail = timelineMs > 0

  const renderTL = () => {
    if (!task.has_transcription) {
      return (
        <div className="empty">
          <Icon name="activity" size="lg" />
          <b>转写完成后就能看到时间轴</b>
        </div>
      )
    }
    if (transcriptAtoms.length === 0 && visualAtoms.length === 0) {
      return (
        <div className="empty">
          <Icon name="activity" size="lg" />
          <b>时间轴暂无数据</b>
        </div>
      )
    }
    return (
      <>
        {hasRail && (
          <>
            <div className="tl-rail-wrap">
            <div
              className="tl-rail"
              onPointerDown={e => {
                const rect = e.currentTarget.getBoundingClientRect()
                const ratio = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
                seek(ratio * timelineMs)
              }}
              onPointerMove={e => {
                const rect = e.currentTarget.getBoundingClientRect()
                const ratio = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
                const ms = ratio * timelineMs
                const hits = visualAtoms.filter(a => ms >= a.start_ms && ms <= Math.max(a.end_ms, a.start_ms + 1))
                const atom = hits.length > 0 ? hits[hits.length - 1] : null
                if (!atom?.content) { setRailTip(null); return }
                setRailTip({
                  left: Math.max(16, Math.min(rect.width - 16, e.clientX - rect.left)),
                  text: atom.content,
                  timeMs: atom.start_ms,
                })
              }}
              onPointerLeave={() => setRailTip(null)}
            >
              {transcriptAtoms.map(a => (
                <div
                  key={`tt-${a.id}`}
                  className="tl-seg t-transcript"
                  style={{ left: `${(a.start_ms / durPct) * 100}%`, width: `${Math.max(0.3, ((a.end_ms - a.start_ms) / durPct) * 100)}%` }}
                />
              ))}
              {visualAtoms.map(a => (
                <div
                  key={`tv-${a.id}`}
                  className={`tl-seg ${a.modality === 'visual_ocr' ? 't-ocr' : 't-caption'}`}
                  style={{ left: `${(a.start_ms / durPct) * 100}%`, width: `max(6px, ${((a.end_ms - a.start_ms) / durPct) * 100}%)` }}
                />
              ))}
              <div className={`tl-head${headSnap ? ' snap' : ''}`} style={{ left: `${(playheadMs / durPct) * 100}%` }} />
            </div>
            {railTip && (
              <div className="tl-tip" style={{ ['--tip-x' as string]: `${railTip.left}px` }} role="tooltip">
                <span className="tl-tip-time">{formatTime(railTip.timeMs)}</span>
                {visualTipSections(railTip.text).map((sec, i) => (
                  <div key={i} className="tl-tip-sec">
                    {sec.label && <div className="tl-tip-k">{sec.label}</div>}
                    <div className="tl-tip-v">{sec.body}</div>
                  </div>
                ))}
              </div>
            )}
            </div>
            <div className="tl-scale mono">
              {Array.from({ length: 6 }, (_, i) => (
                <span key={i}>{formatTime((timelineMs * i) / 5)}</span>
              ))}
            </div>
            <div className="tl-legend">
              <span><i style={{ background: 'rgba(154,145,127,.5)' }} />解说转写</span>
              <span><i style={{ background: 'rgba(226,168,75,.6)' }} />画面 OCR</span>
              <span><i style={{ background: 'rgba(134,180,201,.55)' }} />画面描述</span>
              <span style={{ marginLeft: 'auto', color: 'var(--tx-4)' }}>悬停色块看画面 · 点击跳转</span>
            </div>
          </>
        )}
        {transcriptRows.length > 0 && (
          <div className="transcript-list">
            {transcriptRows.map((a, i) => (
              <div
                key={a.id}
                ref={i === liveIndex ? liveRowRef : undefined}
                className={`t-row${i === liveIndex ? ' live' : ''}`}
                onClick={() => seek(a.start_ms)}
              >
                <span className="ts">{formatTime(a.start_ms)}</span>
                <span className="tx">{a.content}</span>
              </div>
            ))}
          </div>
        )}
      </>
    )
  }

  const renderVF = () => {
    const coverage = timeline?.visual_coverage
    const tailUncovered = !!coverage && videoDurationMs > 0 &&
      coverage.last_ms + Math.max(60_000, coverage.largest_gap_ms * 1.5) < videoDurationMs
    const evidenceTailUncovered = !!coverage && coverage.evidence_last_ms !== undefined && videoDurationMs > 0 &&
      coverage.evidence_last_ms + Math.max(60_000, coverage.largest_gap_ms * 1.5) < videoDurationMs
    return (
      <>
        <label style={{ display: 'flex', gap: 8, alignItems: 'center', fontSize: 13, marginBottom: 8 }}>
          <input type="checkbox" checked={!task.visual_disabled} disabled={readOnly || processing || visualSettingBusy} onChange={e => void setVisualDisabled(!e.target.checked)} />
          后续生成画面证据
        </label>
        <p className="muted" style={{ fontSize: 12, marginBottom: 10 }}>
          关闭后，下次转写会跳过抽帧、OCR 和画面描述，后续问答也不会按需抽帧；转写与摘要照常进行。已有画面证据和检索索引保留，重建索引仍会纳入已有画面文字。开启后需再次转写才会重新生成，不会立即补做；成功生成的新帧会替换旧视觉记录，索引会提示重建。{processing ? '当前任务结束后可修改。' : ''}
        </p>
        {coverage ? <p style={{ fontSize: 13, color: 'var(--tx-3)', marginBottom: 10 }} role="status">
          已保存采样帧 {coverage.sampled_frames} 张，其中 {coverage.evidence_frames} 张生成了 OCR 或描述、{coverage.preview_frames} 张有预览。
          采样时间 {formatTime(coverage.first_ms)}–{formatTime(coverage.last_ms)}{videoDurationMs > 0 ? ` / 视频总长 ${formatTime(videoDurationMs)}` : '；视频总长待加载'}。
          {coverage.evidence_first_ms !== undefined && coverage.evidence_last_ms !== undefined ? ` 有文字证据的时间范围 ${formatTime(coverage.evidence_first_ms)}–${formatTime(coverage.evidence_last_ms)}。` : ' 尚无可用的 OCR 或描述文字。'}
          {tailUncovered ? ` 后段尚无采样帧，采样仅到 ${formatTime(coverage.last_ms)}。` : evidenceTailUncovered ? ' 后段采样帧尚未产出文字证据。' : ''}
        </p> : <p className="muted" style={{ fontSize: 13, marginBottom: 10 }}>尚无已保存的画面采样帧。</p>}
        {frames.length === 0 && <div className="empty"><Icon name="eye" size="lg" /><b>没有画面文字证据</b></div>}
        <div className="frames-list">
          {frames.map(f => (
            <div className="frame-row" key={f.key} onClick={() => seek(f.timeMs)}>
              <div className="frame-still">
                <FramePreview key={`${f.frameId}-${playbackUrl || ''}`} src={f.frameId ? api.visualFrameSrc(taskId, f.frameId, playbackUrl) : null} timeMs={f.timeMs} />
              </div>
              <div className="frame-copy">
                <div className="frame-meta">
                  <span className="frame-time">{formatTime(f.timeMs)}</span>
                  <span style={{ display: 'inline-flex', gap: 4 }}>
                    {f.hasOcr && <ModalityTag modality="visual_ocr" />}
                    {f.hasCaption && <ModalityTag modality="visual_caption" />}
                  </span>
                </div>
                <FrameRead>
                  {f.caption && <div className="frame-caption">{f.caption}</div>}
                  {f.ocr && <div className="frame-ocr">{f.ocr}</div>}
                </FrameRead>
              </div>
            </div>
          ))}
        </div>
      </>
    )
  }

  const renderIdx = () => {
    if (!index) {
      return (
        <div className="empty">
          <Icon name="layers" size="lg" />
          <b>索引状态不可用</b>
        </div>
      )
    }
    const stateView = index.indexed
      ? { chip: 'chip-ok', text: '已建立' }
      : index.status === 'indexing'
        ? { chip: 'chip-acc', text: '构建中' }
        : index.status === 'queued'
          ? { chip: 'chip-mute', text: '排队中' }
          : index.status === 'failed'
            ? { chip: 'chip-bad', text: '失败' }
      : index.needs_rebuild
        ? { chip: 'chip-warn', text: '需要重建' }
        : { chip: 'chip-mute', text: '未建立' }
    return (
      <>
        <div className="idx-list">
          <div className="idx-row"><span className="k">状态</span><span className="v"><span className={`chip ${stateView.chip}`}>{stateView.text}</span></span></div>
          <div className="idx-row"><span className="k">阶段</span><span className="v">{indexPhase(index)}</span></div>
          <div className="idx-row"><span className="k">证据块</span><span className="v mono">{index.status === 'indexing' && index.total_chunks > 0 ? `${index.completed_chunks} / ${index.total_chunks} 块已完成 Embedding` : `${index.chunks} 块`}</span></div>
          <div className="idx-row"><span className="k">向量模型</span><span className="v mono">{index.embedding_model || '—'}</span></div>
          {index.next_retry_at && <div className="idx-row"><span className="k">下次重试</span><span className="v">{new Date(index.next_retry_at).toLocaleString()}</span></div>}
          {index.progress_at && <div className="idx-row"><span className="k">最近进度</span><span className="v">{fmtRelTime(index.progress_at)}</span></div>}
          {index.last_error && (
            <div className="idx-row"><span className="k">最近错误</span><span className="v" style={{ color: 'var(--bad)', fontSize: 12 }}>{index.last_error}</span></div>
          )}
        </div>
        {index.needs_rebuild && (
          <p style={{ fontSize: 13, color: 'var(--tx-4)', marginTop: 12 }}>索引已过期,需要重建</p>
        )}
        <button
          className="btn btn-sm"
          style={{ marginTop: 14 }}
          disabled={busy !== '' || index.status === 'indexing' || index.status === 'queued'}
          onClick={() => setPendingAction(indexConfirm(index))}
        >
          <Icon name="layers" size="sm" />
          {indexActionLabel(index)}
        </button>
      </>
    )
  }

  return (
    <div className="page-fill">
      <div className="ws">
        <div className="ws-stage">
          <div className="ws-heading">
            {editingTitle ? (
              <form
                className="ws-title-form"
                onSubmit={e => { e.preventDefault(); void saveTitle() }}
              >
                <input
                  className="input"
                  value={titleDraft}
                  maxLength={60}
                  autoFocus
                  onChange={e => setTitleDraft(e.target.value)}
                  onKeyDown={e => { if (e.key === 'Escape') setEditingTitle(false) }}
                />
                <button className="btn btn-sm btn-primary" type="submit" disabled={titleBusy}>保存</button>
                <button className="btn btn-sm" type="button" onClick={() => setEditingTitle(false)}>取消</button>
              </form>
            ) : (
              <>
                <h2>{title}</h2>
                {!readOnly && (
                  <button
                    className="btn btn-ic btn-ghost"
                    aria-label="编辑标题"
                    title="编辑标题"
                    onClick={() => { setTitleDraft(task.title || task.filename || ''); setEditingTitle(true) }}
                  >
                    <Icon name="pencil" size="sm" />
                  </button>
                )}
              </>
            )}
          </div>
          <VideoPlayer
            key={`${taskId}-${searchParams?.t || '0'}`}
            initialTimeMs={searchParams?.t ? Number(searchParams.t) : undefined}
            ref={playerRef}
            src={playbackUrl}
            title={title}
            onPlayhead={ms => setPlayheadMs(ms)}
            onDuration={setVideoDurationMs}
            onNeedRefresh={refreshPlaybackUrl}
            fallbackText={failed ? '任务处理失败,暂无可用播放源' : '播放源暂不可用,文件可能仍在处理'}
          />
          {processing && (
            <div style={{ marginTop: 12, flex: 'none' }}>
              <ProcessStrip status={task.status} stage={task.stage} has_transcription={task.has_transcription} last_job_type={task.last_job_type} />
            </div>
          )}
          {(task.stage === 'transcribing' || task.last_job_type === 'transcribe') && !task.has_transcription && <TranscriptionProgressPanel task={task} />}

          <div className="ws-actions">
            {task.has_summary ? (
              <button className="btn" onClick={() => setSummaryOpen(true)}>
                <Icon name="file" size="sm" />查看摘要
              </button>
            ) : (
              <button
                className="btn"
                disabled={busy !== '' || processing || !task.has_transcription}
                title={!task.has_transcription ? '转写完成后才能生成摘要' : processing ? `当前${taskStateView(task).text}；等待该任务结束后可生成摘要` : undefined}
                onClick={() => void runAction('analyze')}
              >
                <Icon name="wand" size="sm" />{busy === 'analyze' ? '已加入队列…' : '生成摘要'}
              </button>
            )}
            {task.has_transcription ? (
              <button className="btn" disabled={busy !== ''} onClick={() => setPendingAction({
                kind: 'transcribe',
                force: true,
                title: '重新转写?',
                body: '会清除旧分片并再次调用语音识别，可能产生新的 ASR 费用。',
                confirmLabel: '重新转写',
              })}>
                <Icon name="refresh" size="sm" />重新转写
              </button>
            ) : processing ? (
              <button className="btn" disabled>
                <Icon name="activity" size="sm" />等待转写
              </button>
            ) : (
              <button className="btn" disabled={busy !== ''} onClick={() => void runAction('transcribe')}>
                <Icon name="activity" size="sm" />开始转写
              </button>
            )}
            <button className="btn" disabled={busy !== '' || !index || index.status === 'indexing' || index.status === 'queued'} onClick={() => index && setPendingAction(indexConfirm(index))}>
              <Icon name="layers" size="sm" />{indexActionLabel(index)}
            </button>
            <button className="btn" disabled={busy !== ''} onClick={() => void downloadAudio()}>
              <Icon name="download" size="sm" />下载音频
            </button>
            <button className="btn" disabled={readOnly} title={readOnly ? '演示账号不可修改知识库' : undefined} onClick={() => setKbOpen(true)}>
              <Icon name="folder" size="sm" />加入知识库
            </button>
            <span style={{ flex: 1 }} />
            <button className="btn btn-primary" onClick={() => router.push(`/chat/v/${task.id}`)}>
              <Icon name="message" size="sm" />进入问答
            </button>
          </div>
          {((!task.has_summary && task.has_transcription && processing) || (!task.has_summary && task.summary_progress)) && (
            <div className="ws-action-status">
              {!task.has_summary && task.has_transcription && processing && (
                <span className="muted" style={{ fontSize: 12 }} role="status">当前{taskStateView(task).text}，任务结束后可生成摘要</span>
              )}
              {!task.has_summary && task.summary_progress && (
                <span className="muted" style={{ fontSize: 12 }} role="status">
                  摘要{task.summary_progress.phase === 'merging' ? '合并总结' : '分段处理'}：{task.summary_progress.completed}/{task.summary_progress.total}
                  {task.summary_progress.current > 0 ? ` · 第 ${task.summary_progress.current} 段${task.summary_progress.end_ms > task.summary_progress.start_ms ? `（${Math.floor(task.summary_progress.start_ms / 1000)}–${Math.ceil(task.summary_progress.end_ms / 1000)} 秒）` : '（时间未记录）'}` : ''}
                  {task.summary_progress.failed_part ? task.summary_progress.phase === 'merging' ? ` · 第 ${task.summary_progress.failed_part} 组合并失败，完整总结尚未生成` : ` · 第 ${task.summary_progress.failed_part} 段失败，尚未覆盖全片` : ''}
                </span>
              )}
            </div>
          )}

          {failed && (
            <div className="card card-pad" style={{ marginTop: 14, flex: 'none', borderColor: 'rgba(224,131,115,.35)' }}>
              <div style={{ display: 'flex', gap: 10, alignItems: 'flex-start' }}>
                <span style={{ color: 'var(--bad)' }}><Icon name="alert" /></span>
                <div style={{ flex: 1 }}>
                  <b style={{ fontSize: 13 }}>{summaryFailure?.category || (task.status === TaskStatusEnum.Dead ? '任务已废弃' : '处理失败')}</b>
                  {summaryFailure ? <>
                    <p style={{ fontSize: 12, color: 'var(--tx-3)', marginTop: 4 }}>{summaryFailure.retry}</p>
                    <p style={{ fontSize: 12, color: 'var(--tx-3)', marginTop: 4 }}>{summaryFailure.advice}</p>
                    <button className="btn btn-sm" style={{ marginTop: 8 }} onClick={() => {
                      void navigator.clipboard.writeText(summaryFailure.diagnosticId).then(() => toast.success('诊断编号已复制')).catch(() => toast.error('复制失败，请手动复制编号'))
                    }}>诊断编号：{summaryFailure.diagnosticId} · 复制</button>
                  </> : <p style={{ fontSize: 12, color: 'var(--tx-3)', marginTop: 4 }}>
                    {task.error_msg || task.last_error_msg || '处理过程中出现错误'}
                    {task.max_retries > 0 ? ` · 重试 ${task.retry_count}/${task.max_retries}` : ''}
                  </p>}
                  {urlJob ? (
                    <span className="chip chip-mute" style={{ marginTop: 10 }}>URL 任务,请删除后重新添加</span>
                  ) : !summaryFailure?.scheduled && (
                    <button
                      className="btn btn-sm"
                      style={{ marginTop: 10 }}
                      disabled={busy !== ''}
                      onClick={() => setPendingAction({
                        kind: task.last_job_type === 'analyze' ? 'analyze' : 'transcribe',
                        title: '重新提交任务?',
                        body: summaryFailure ? `${summaryFailure.advice} 重新提交可能再次消耗模型额度。` : '失败步骤会重新入队,可能再次消耗模型额度。',
                        confirmLabel: '重试',
                      })}
                    >
                      重试
                    </button>
                  )}
                </div>
              </div>
            </div>
          )}
        </div>

        <div className="card ws-rail">
          <div className="rail-tabs" style={{ padding: '10px 14px 0' }}>
            {([['tl', '转写时间轴'], ['vf', '画面证据'], ['idx', '检索索引']] as const).map(([key, label]) => (
              <button key={key} className={`rail-tab${tab === key ? ' on' : ''}`} onClick={() => openTab(key)}>
                {label}
              </button>
            ))}
          </div>
          <div className="rail-body">
            <div className={`rail-pane${tab === 'tl' ? ' on' : ''}`}>{seen.tl ? renderTL() : null}</div>
            <div className={`rail-pane${tab === 'vf' ? ' on' : ''}`}>{seen.vf ? renderVF() : null}</div>
            <div className={`rail-pane${tab === 'idx' ? ' on' : ''}`}>{seen.idx ? renderIdx() : null}</div>
          </div>
        </div>
      </div>
      {summaryOpen && task.summary && (
        <Modal title="AI 摘要" className="modal-read" onClose={() => setSummaryOpen(false)}>
          <p className="summary-modal-meta">
            {task.summary.model_name} · {fmtRelTime(task.summary.created_at)}
          </p>
          <div className="summary-body">
            <MarkdownAnswer content={task.summary.content} domainTags />
          </div>
        </Modal>
      )}
      {kbOpen && (
        <KBModal
          mode="assign"
          taskId={task.id}
          indexed={!!index?.indexed}
          onClose={() => setKbOpen(false)}
          onChanged={() => toast.success('知识库成员已更新')}
        />
      )}
      {pendingAction && (
        <ConfirmModal
          title={pendingAction.title}
          confirmLabel={pendingAction.confirmLabel}
          busy={busy !== ''}
          onClose={() => setPendingAction(null)}
          onConfirm={() => void confirmAndRun()}
        >
          {pendingAction.body}
        </ConfirmModal>
      )}
    </div>
  )
}
