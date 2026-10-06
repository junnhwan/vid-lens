import { useAIAvailability } from '@/components/settings/useAIAvailability'
import { useVideoAIPreflight } from '@/components/settings/VideoAIPreflight'
import { studyAvailability, visualAvailability } from '@/lib/taskCapabilities'
import { ArtifactCreateDialog } from '@/components/artifacts/ArtifactCreateDialog'
import { artifactApi, artifactError } from '@/lib/artifacts/api'
import Link from '@/lib/router'
import { useQuery } from '@tanstack/react-query'

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useRouter } from '@/lib/router'
import { api, ApiError } from '@/lib/api'
import {
  TaskStatusEnum,
  type RAGIndexResult,
  type TimelineAtom,
  type TranscriptionProgress,
  type VideoTask,
  type VideoTimeline,
  type VisualMode,
} from '@/lib/types'
import { fmtRelTime, fmtDateTime, taskTitle } from '@/lib/format'
import { formatTime, formatTimeRange, needsCitationUpgrade } from '@/components/Citation'
import { ModalityTag } from '@/components/ui/ModalityTag'
import { VideoPlayer, type VideoPlayerHandle } from '@/components/player/VideoPlayer'
import { SummaryRevisionPanel } from '@/components/summary/SummaryRevisionPanel'
import { VideoQuestionsPanel } from '@/components/chat/VideoQuestionsPanel'
import { useCrumb, useShell } from '@/components/shell/AppShell'
import { useToast } from '@/components/Toast'
import { Icon } from '@/components/ui/Icon'
import { ConfirmModal, Modal } from '@/components/ui/Modal'
import KBModal from '@/components/KBModal'
import { groupTranscriptSources } from '@/lib/transcript'
import { ProcessStrip } from '@/components/ProcessStrip'
import { TranscriptionProgressPanel } from '@/components/TranscriptionProgressPanel'
import { VisualProgressPanel } from '@/components/VisualProgressPanel'
import { useStudyPosition } from '@/lib/artifacts/useStudyPosition'
import { summaryFailureView } from '@/lib/summaryFailure'
import { canGenerateSummary, summaryRunning, summaryStatusText } from '@/lib/summaryState'
import { LoadingBlock, ErrorState } from '@/components/ui/AsyncState'
import './VideoWorkbench.css'

// 视频工作台:播放器钉住 + 右栏时间轴/画面/索引。摘要走阅读弹窗。
// 对应原型 #/video/:id。播放源用 /playback 签名 URL;时间轴/画面证据来自
// /timeline 的原子(转写、OCR、画面描述同轨)。画面卡片读取对应已保存帧；
// 点击卡片仍按该帧时间跳转播放器。

type TabKey = 'tl' | 'vf' | 'idx'
type ActionKind = 'transcribe' | 'align' | 'analyze' | 'index' | 'download'
type ConfirmAction = {
  kind: Exclude<ActionKind, 'download'>
  force?: boolean
  title: string
  body: string
  confirmLabel: string
}

const visualChoices: { mode: VisualMode; title: string; description: string; icon: 'video' | 'scan' | 'photo' | 'layers' }[] = [
  { mode: 'off', title: '关闭', description: '演讲、访谈或画面变化很少时，转写通常已经足够。', icon: 'video' },
  { mode: 'ocr', title: '文字识别 OCR', description: '适合文字课件、笔记和清晰的板书，提取画面中的文字。', icon: 'scan' },
  { mode: 'caption', title: '画面描述', description: '适合图表、流程图、示意图，调用视觉模型理解画面。', icon: 'photo' },
  { mode: 'both', title: 'OCR + 画面描述', description: '适合文字与图表混合的课程，会增加处理量与模型调用。', icon: 'layers' },
]

function taskVisualMode(task: VideoTask): VisualMode {
  return task.visual_mode || (task.visual_disabled ? 'off' : 'both')
}

function visualModeLabel(mode: VisualMode): string {
  return visualChoices.find(choice => choice.mode === mode)?.title || '关闭'
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
    ? <img src={src} alt={`${formatTime(timeMs)} 的已保存画面帧`} onError={() => setFailed(true)} style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', objectFit: 'cover' }} />
    : <div className="muted" role="status" style={{ padding: 12, fontSize: 12 }}>帧预览不可用</div>
}

function ClampRead({ children, className }: { children: ReactNode; className: string }) {
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
    <div className="clamp-read">
      <div ref={ref} className={`${className}${open ? ' open' : ''}`}>{children}</div>
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

function FrameRead({ children }: { children: ReactNode }) {
  return <ClampRead className="frame-read">{children}</ClampRead>
}

export default function VideoWorkbenchPage({ params, searchParams }: { params: { id: string }; searchParams?: { t?: string; citations?: string } }) {
  const [artifactMode, setArtifactMode] = useState<'new' | 'reorganize' | null>(null)
  const taskId = Number(params.id)
  const router = useRouter()
  const toast = useToast()
  const { user } = useShell()
  const readOnly = user?.role === 'DEMO'
  const ai = useAIAvailability(readOnly, 'summary')
  const videoPreflight = useVideoAIPreflight()
  const asrAI = useAIAvailability(readOnly, 'transcribe')
  const alignAI = useAIAvailability(readOnly, 'align')
  const indexAI = useAIAvailability(readOnly, 'index')
  const playerRef = useRef<VideoPlayerHandle>(null)
  const study = useStudyPosition()
  const lastPositionWrite = useRef(0)
  const wasPlaying = useRef(false)
  const prevTransRef = useRef(false)

  const [task, setTask] = useState<VideoTask | null>(null)
  const [transcriptionProgress, setTranscriptionProgress] = useState<TranscriptionProgress | null>(null)
  const relatedArtifacts = useQuery({ queryKey: ['video-artifacts', taskId], queryFn: async ({ signal }) => {
    const first = await artifactApi.list(1, taskId, signal)
    const list = [...first.list]
    for (let page = 2; list.length < first.total && !list.some(item => item.current_version_id); page++) {
      const next = await artifactApi.list(page, taskId, signal)
      if (!next.list.length) break
      list.push(...next.list)
    }
    return { ...first, list }
  }, enabled: !!task && !!user, refetchInterval: 15_000 })
  const [timeline, setTimeline] = useState<VideoTimeline | null>(null)
  const [index, setIndex] = useState<RAGIndexResult | null>(null)
  const [playbackUrl, setPlaybackUrl] = useState<string | null>(null)
  const [videoDurationMs, setVideoDurationMs] = useState(0)
  const [visualSettingBusy, setVisualSettingBusy] = useState(false)
  const [visualBuildBusy, setVisualBuildBusy] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [reloadKey, setReloadKey] = useState(0)
  const [subError, setSubError] = useState('')
  const [subReloadTick, setSubReloadTick] = useState(0)
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
  const [questionsOpen, setQuestionsOpen] = useState(false)
  const [moreOpen, setMoreOpen] = useState(false)
  const [visualSettingsOpen, setVisualSettingsOpen] = useState(false)
  const [visualDraft, setVisualDraft] = useState<VisualMode>('off')
  const visualAI = useAIAvailability(readOnly, visualDraft === 'ocr' ? 'ocr' : 'caption')
  const ocrAI = useAIAvailability(readOnly, 'ocr')
  const [dialogInstant, setDialogInstant] = useState(false)
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
    setTranscriptionProgress(null)
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
    })()
    return () => { active = false }
  }, [taskId, reloadKey])

  // 时间轴/索引失败不再静默成空面板，给出错误态+重试；播放源失败由播放器 fallback 文案提示。
  useEffect(() => {
    let active = true
    setSubError('')
    void (async () => {
      const [tl, idx, playback] = await Promise.all([
        api.getTimeline(taskId).catch(() => null),
        api.getRagIndex(taskId).catch(() => null),
        api.playbackSrc(taskId).catch(() => null),
      ])
      if (!active) return
      setTimeline(tl)
      setIndex(idx)
      setPlaybackUrl(playback)
      if (!tl || !idx) setSubError('时间轴或索引数据加载失败')
    })()
    return () => { active = false }
  }, [taskId, subReloadTick])

  const processing = !!task && (task.status === TaskStatusEnum.Queued || task.status === TaskStatusEnum.Running)
  const generatingSummary = !!task && summaryRunning(task)
  const visualProcessing = !!task && ['queued', 'running'].includes(task.visual_status)
  const readableArtifact = relatedArtifacts.data?.list.find(item => !!item.current_version_id)
  const pendingArtifact = relatedArtifacts.data?.list.find(item => !item.current_version_id && item.latest_run && (item.latest_run.status === 'pending' || item.latest_run.status === 'running'))
  const awaitingSummaryRetry = !!task && !!summaryFailureView(task)?.scheduled

  // 处理中或等待摘要自动重试时轮询；重试调度会清除 next_retry_at 并重新入队。
  useEffect(() => {
    if (!processing && !generatingSummary && !awaitingSummaryRetry && !visualProcessing) return
    const iv = setInterval(() => {
      void (async () => {
        try {
          const fresh = await api.getTask(taskId)
          const visualJustDone = ['queued', 'running'].includes(task?.visual_status || '') && !['queued', 'running'].includes(fresh.visual_status)
          const prev = prevTransRef.current
          prevTransRef.current = fresh.has_transcription
          setTask(fresh)
          const transJustDone = !prev && fresh.has_transcription
          const justCompleted = fresh.status === TaskStatusEnum.Completed
          if (transJustDone || justCompleted || visualJustDone) {
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
  }, [processing, generatingSummary, awaitingSummaryRetry, visualProcessing, taskId, task?.visual_status])

  useEffect(() => {
    if (!processing && busy !== 'index' && index?.status !== 'indexing' && index?.status !== 'queued') return
    const iv = setInterval(() => { void api.getRagIndex(taskId).then(setIndex).catch(() => {}) }, 5000)
    return () => clearInterval(iv)
  }, [processing, busy, index?.status, taskId])

  const transcriptAtoms = useMemo(
    () => (timeline?.atoms || []).filter(a => a.modality === 'transcript'),
    [timeline],
  )
  const transcriptRows = useMemo(() => groupTranscriptSources(transcriptAtoms), [transcriptAtoms])
  const citationUpgradeAvailable = transcriptAtoms.some(atom => needsCitationUpgrade({ modality: atom.modality, startMS: atom.start_ms, endMS: atom.end_ms, timeRangeStatus: atom.time_range_status }))
  useEffect(() => {
    if (searchParams?.citations !== 'upgrade' || !timeline) return
    setTab('tl')
    const section = document.getElementById('transcript')
    section?.scrollIntoView?.({ block: 'nearest' })
  }, [searchParams?.citations, timeline])
  const visualAtoms = useMemo(
    () => (timeline?.atoms || []).filter(a => a.modality === 'visual_ocr' || a.modality === 'visual_caption'),
    [timeline],
  )
  const studyCapability = studyAvailability(task, timeline)
  const visualCapability = task ? visualAvailability(task) : { ready: false, reason: '正在读取视频' }
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

  const rowAtPlayhead = (a: TimelineAtom) => a.time_range_status !== 'unknown' && Number.isFinite(a.start_ms) && Number.isFinite(a.end_ms) && playheadMs >= a.start_ms && playheadMs < Math.max(a.end_ms, a.start_ms + 1)
  const exactLiveIndex = transcriptRows.findIndex(a => a.time_range_status === 'exact' && rowAtPlayhead(a))
  const liveIndex = exactLiveIndex >= 0 ? exactLiveIndex : transcriptRows.findIndex(rowAtPlayhead)

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

  const performAction = async (kind: Exclude<ActionKind, 'download'>, force = false) => {
    if (!task || busy) return
    const admission = kind === 'transcribe' ? asrAI : kind === 'align' ? alignAI : kind === 'index' ? indexAI : ai
    if (!admission.ready) { toast.info(admission.reason); return }
    setBusy(kind)
    try {
      if (kind === 'transcribe') {
        await api.transcribe(task.id, force)
        toast.success(force ? '重新转写已排队，将再次调用语音识别' : '转写任务已排队，等待并发名额')
      } else if (kind === 'align') {
        await api.alignTranscript(task.id)
        toast.success('逐句对齐已排队，将复用已有转写')
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

  const runAction = (kind: Exclude<ActionKind, 'download'>, force = false) => {
    const labels: Record<Exclude<ActionKind, 'download'>, string> = {
      transcribe: force ? '重新转写视频' : '转写视频',
      align: '对齐句子时间',
      analyze: '生成视频摘要',
      index: '建立视频检索索引',
    }
    videoPreflight.request(labels[kind], () => { void performAction(kind, force) }, kind === 'analyze' ? 'summary' : kind)
  }

  const downloadMedia = async () => {
    if (!task || busy) return
    setBusy('download')
    try {
      const r = await api.downloadMedia(task.id)
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

  const openVisualSettings = (event?: { detail: number }) => {
    if (!task) return
    setDialogInstant(event?.detail === 0)
    setVisualDraft(taskVisualMode(task))
    setVisualSettingsOpen(true)
  }

  const saveVisualMode = async (build = false) => {
    if (!task || visualSettingBusy) return
    if (readOnly || (build && (!visualAI.ready || visualDraft === 'both' && !ocrAI.ready || !visualCapability.ready))) { toast.info(readOnly ? '演示模式不可处理视频' : !visualAI.ready ? visualAI.reason : visualDraft === 'both' && !ocrAI.ready ? ocrAI.reason : visualCapability.reason); return }
    setVisualSettingBusy(true)
    try {
      const fresh = await api.setVisualMode(task.id, visualDraft)
      setTask(fresh)
      if (build) {
        setVisualBuildBusy(true)
        try {
          await api.buildVisual(task.id)
          const next = await api.getTask(task.id).catch(() => null)
          setTask(next ? { ...next, visual_status: ['queued', 'running'].includes(next.visual_status) ? next.visual_status : 'queued' } : { ...fresh, visual_status: 'queued' })
          toast.success('画面证据已加入处理队列')
        } finally {
          setVisualBuildBusy(false)
        }
      } else {
        toast.success(visualDraft === 'off' ? '已关闭后续画面分析' : `已保存：${visualModeLabel(visualDraft)}`)
      }
      setVisualSettingsOpen(false)
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '画面证据设置保存失败')
    } finally {
      setVisualSettingBusy(false)
    }
  }

  if (loading) {
    return <div className="page"><LoadingBlock label="正在加载…" variant="card" /></div>
  }
  if (loadError || !task) {
    return (
      <div className="page">
        <ErrorState message={loadError || '视频加载失败'} onRetry={() => setReloadKey(k => k + 1)} />
      </div>
    )
  }

  const failed = task.status === TaskStatusEnum.Failed || task.status === TaskStatusEnum.Dead
  const transcriptionRelevant = task.stage === 'transcribing' || task.stage === 'aligning' || task.stage === 'visual_indexing' || task.last_job_type === 'transcribe'
  const alignmentIncomplete = !!transcriptionProgress?.alignment_only && failed && task.last_job_type === 'transcribe'
  const transcriptionIncomplete = !transcriptionProgress?.alignment_only && transcriptionRelevant && (!!transcriptionProgress && transcriptionProgress.completed < transcriptionProgress.total || failed && task.last_job_type === 'transcribe')
  const resumeTranscription = !processing && transcriptionIncomplete
  const checkingTranscription = transcriptionRelevant && !transcriptionProgress
  const citationUpgradeLabel = resumeTranscription ? '重试补齐引用定位' : '补齐引用定位'
  const resumeTranscriptionBody = '会保留已完成的转写分片，继续处理未完成部分，再更新转写与检索索引，可能产生 ASR 和 Embedding 费用。全部完成前仍使用之前保存的内容与引用。'
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
          <b>{task.visual_status === 'completed' ? '这段视频已有画面内容' : '还没有声音转写'}</b><span>{task.visual_status === 'completed' ? '在「画面证据」中查看关键帧与文字。需要声音文字时再开始转写。' : '讲解视频可开始转写；无声演示可直接分析画面。'}</span>
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
              {transcriptRows.map(a => (
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
              <span><i style={{ background: 'color-mix(in srgb, var(--mute) 50%, transparent)' }} />解说转写</span>
              <span><i style={{ background: 'color-mix(in srgb, var(--acc) 60%, transparent)' }} />画面 OCR</span>
              <span><i style={{ background: 'color-mix(in srgb, var(--info) 55%, transparent)' }} />画面描述</span>
              <span style={{ marginLeft: 'auto', color: 'var(--tx-4)' }}>悬停色块看画面 · 点击跳转</span>
            </div>
          </>
        )}
        {transcriptRows.length > 0 && (
          <div id="transcript" className="transcript-list">
            {timeline?.alignment_available && transcriptAtoms.some(atom => atom.time_range_status !== 'exact') && (
              <div className="transcript-upgrade">
                <b>精确回放定位 · 按需开启</b>
                <p>当前可以从片段回放。需要逐句定位时，可将已有文字与视频音频对齐；此操作会运行服务端配置的本地模型，普通转写不会自动执行。</p>
                <button className="btn btn-sm" disabled={readOnly || busy !== '' || processing} onClick={() => setPendingAction({ kind: 'align', title: '对齐句子时间？', body: '会复用已有识别文字，在本地对齐音频时间并更新检索索引。对齐不会再次调用语音识别；重建索引可能产生 Embedding 费用。历史回答和引用快照保留。', confirmLabel: '开始对齐' })}>对齐句子时间</button>
              </div>
            )}
            {(citationUpgradeAvailable || resumeTranscription) && <div className="transcript-upgrade">
              <b>{resumeTranscription ? '本次引用定位补齐尚未完成' : '旧转写的引用定位可以补齐'}</b>
              <p>{resumeTranscription ? '仍在使用之前保存的转写与引用时间。重试会继续处理未完成部分，保留已完成分片。' : '当前转写只有较长的原片段时间。重新识别音频后，新回答会使用更短的语音片段定位；历史回答保留原有引用。'}</p>
              <button className="btn btn-sm" disabled={readOnly || busy !== '' || processing || checkingTranscription} onClick={() => setPendingAction({ kind: 'transcribe', force: !resumeTranscription, title: `${citationUpgradeLabel}？`, body: resumeTranscription ? resumeTranscriptionBody : '会重新识别这段视频的音频，并更新转写与检索索引，可能产生新的 ASR 和 Embedding 费用。完成后，新回答会使用更短的来源时间；历史回答和引用快照会保留。语音服务不返回句子时间时，将使用短音频片段时间。', confirmLabel: citationUpgradeLabel })}><Icon name="refresh" size="sm" />{citationUpgradeLabel}</button>
              {readOnly && <small>演示账号无法重新识别视频。</small>}
            </div>}
            {transcriptRows.map((a, i) => (
              <div
                key={a.id}
                ref={i === liveIndex ? liveRowRef : undefined}
                className={`t-row${a.time_range_status !== 'exact' ? ' coarse' : ''}${i === liveIndex ? ' live' : ''}`}
              >
                <button type="button" className="transcript-time" disabled={a.time_range_status === 'unknown'} aria-label={a.time_range_status === 'unknown' ? '来源时间未知' : `回放 ${formatTime(a.start_ms)}`} onClick={() => seek(a.start_ms)}>
                  <span className="ts">{a.time_range_status === 'unknown' ? '时间未知' : a.time_range_status === 'exact' ? formatTimeRange(a.start_ms, a.end_ms) : <>{formatTimeRange(a.start_ms, a.end_ms)}<small>{a.end_ms - a.start_ms <= 30000 ? '约定位' : '原片段'}</small></>}</span>
                  {a.time_range_status !== 'unknown' && <Icon name="play" size="sm" />}
                </button>
                <ClampRead className="tx transcript-paragraphs">{a.paragraphs.map((text, paragraph) => <p key={paragraph}>{text}</p>)}</ClampRead>
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
        <div className="workbench-visual-summary">
          <div><span className="workbench-eyebrow">画面分析</span><strong>{visualModeLabel(taskVisualMode(task))}</strong></div>
          <button className="btn btn-sm" onClick={openVisualSettings}><Icon name="settings" size="sm" />设置</button>
        </div>
        <p className="workbench-visual-hint">按内容选择 OCR 或画面描述。演讲、访谈通常无需启用；已有画面证据会保留。</p>
        {coverage ? <p style={{ fontSize: 13, color: 'var(--tx-3)', marginBottom: 10 }} role="status">
          已保存采样帧 {coverage.sampled_frames} 张，其中 {coverage.evidence_frames} 张生成了 OCR 或描述、{coverage.preview_frames} 张有预览。
          采样时间 {formatTime(coverage.first_ms)}–{formatTime(coverage.last_ms)}{videoDurationMs > 0 ? ` / 视频总长 ${formatTime(videoDurationMs)}` : '；视频总长待加载'}。
          {coverage.evidence_first_ms !== undefined && coverage.evidence_last_ms !== undefined ? ` 有文字证据的时间范围 ${formatTime(coverage.evidence_first_ms)}–${formatTime(coverage.evidence_last_ms)}。` : ' 尚无可用的 OCR 或描述文字。'}
          {tailUncovered ? ` 后段尚无采样帧，采样仅到 ${formatTime(coverage.last_ms)}。` : evidenceTailUncovered ? ' 后段采样帧尚未产出文字证据。' : ''}
        </p> : <p className="muted" style={{ fontSize: 13, marginBottom: 10 }}>尚无已保存的画面采样帧。</p>}
        {frames.length === 0 && <div className="empty"><Icon name="eye" size="lg" /><b>没有画面文字证据</b></div>}
        <div className="frames-list">
          {frames.map(f => (
            <button type="button" className="frame-row" key={f.key} onClick={() => seek(f.timeMs)}>
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
            </button>
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
          {index.next_retry_at && <div className="idx-row"><span className="k">下次重试</span><span className="v">{fmtDateTime(index.next_retry_at)}</span></div>}
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
    <div className="page-fill video-workbench">
      {artifactMode && <ArtifactCreateDialog source={{ id: task.id, title }} existing={artifactMode === 'reorganize' && readableArtifact ? { id: readableArtifact.id, title: readableArtifact.title, head_version: readableArtifact.head_version } : undefined} onClose={() => { setArtifactMode(null); void relatedArtifacts.refetch() }} />}
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
          {subError && (
            <div className="card card-pad" style={{ marginTop: 10, marginBottom: 0, display: 'flex', alignItems: 'center', gap: 10 }}>
              <span style={{ color: 'var(--bad)', display: 'flex' }}><Icon name="alert" /></span>
              <b style={{ flex: 1, fontSize: 13 }}>{subError}</b>
              <button className="btn btn-sm" onClick={() => setSubReloadTick(t => t + 1)}>
                <Icon name="refresh" size="sm" />重试
              </button>
            </div>
          )}
          <VideoPlayer
            key={`${taskId}-${searchParams?.t || '0'}`}
            initialTimeMs={searchParams?.t ? Number(searchParams.t) : undefined}
            ref={playerRef}
            src={playbackUrl}
            title={title}
            onPlayhead={(ms,playing) => { setPlayheadMs(ms); if (ms>0 && (playing || wasPlaying.current) && Date.now()-lastPositionWrite.current>5000) { lastPositionWrite.current=Date.now(); study.record({task_id:taskId,artifact_id:'',version_id:'',block_id:'',time_ms:Math.round(ms)}) } if (wasPlaying.current && !playing) void study.flush(); wasPlaying.current=playing }}
            onDuration={setVideoDurationMs}
            onNeedRefresh={refreshPlaybackUrl}
            fallbackText={failed ? '任务处理失败,暂无可用播放源' : '播放源暂不可用,文件可能仍在处理'}
          />
          <p className="workbench-capability" role="status">{task.has_transcription ? processing && transcriptionRelevant ? '正在更新转写与引用定位，完成前仍使用之前保存的内容。' : alignmentIncomplete ? '句子时间对齐未完成，之前保存的转写仍可查看。' : transcriptionIncomplete ? '本次转写尚未完成，当前仍使用之前保存的转写与引用定位。' : checkingTranscription ? '正在核对本次转写进度，已保存的内容仍可查看。' : index?.indexed ? '转写与检索已就绪，可提问并核对引用。' : '转写可阅读；快速问答可使用摘要或转写，检索引用尚未就绪。' : studyCapability.ready ? '画面内容可整理笔记；画面问答需建立检索索引。' : visualCapability.ready ? '视频已导入：讲解视频可开始转写，无声演示可直接分析画面。' : visualCapability.reason}</p>
          {!readOnly && !ai.ready && <div className="artifact-notice" role="status"><span>{ai.reason}</span>{ai.error ? <button className="btn btn-sm" onClick={() => void ai.refetch()}>重试</button> : <a className="btn btn-sm" href="/settings" target="_blank" rel="noopener noreferrer">配置 AI</a>}</div>}
          <section className="workbench-commands" aria-label="视频学习操作">
            <div className="workbench-primary">
              <button className="btn btn-primary workbench-chat-action" onClick={() => router.push(`/chat/v/${task.id}`)}><Icon name="message" size="sm" />进入问答<Icon name="chev-r" size="sm" /></button>
              {readableArtifact ? <Link className="btn" href={`/artifacts/${encodeURIComponent(readableArtifact.id)}`}><Icon name="file" size="sm" />阅读学习笔记</Link> : !task.has_transcription && !studyCapability.ready && !readOnly ? <button className="btn" disabled={readOnly || busy !== '' || processing} onClick={() => runAction('transcribe')}><Icon name="activity" size="sm" />{processing ? '转写处理中' : '开始转写'}</button> : pendingArtifact ? <Link className="btn" href={`/tasks?run=${encodeURIComponent(pendingArtifact.latest_run!.id)}`}><Icon name="clock" size="sm" />查看笔记进度</Link> : <button className="btn" disabled={readOnly || !studyCapability.ready || relatedArtifacts.isPending || !!relatedArtifacts.error} title={studyCapability.reason || undefined} onClick={() => setArtifactMode('new')}><Icon name="wand" size="sm" />新建学习笔记</button>}
              {task.has_summary ? (
                <button className="btn" onClick={() => setSummaryOpen(true)}><Icon name="eye" size="sm" />查看摘要</button>
              ) : (
                <button
                  className={`btn${busy === 'analyze' ? ' is-loading' : ''}`}
                  aria-busy={busy === 'analyze' || undefined}
                  disabled={readOnly || busy !== '' || !canGenerateSummary(task)}
                  title={!task.has_transcription ? '转写完成后才能生成摘要' : generatingSummary ? '摘要正在生成，请勿重复提交' : !canGenerateSummary(task) ? '等待本次转写完成或摘要重试结束' : undefined}
                  onClick={() => void runAction('analyze')}
                ><Icon name="file" size="sm" />{busy === 'analyze' ? '提交中…' : generatingSummary ? summaryStatusText(task) : '生成摘要'}</button>
              )}
            </div>
            <div className="workbench-secondary">
              <button type="button" onClick={event => { setDialogInstant(event.detail === 0); videoPreflight.request('生成视频推荐问题', () => setQuestionsOpen(true), 'summary') }}><Icon name="bulb" size="sm" />推荐问题</button>
              <button type="button" onClick={openVisualSettings}><Icon name="photo" size="sm" />画面分析<span className="workbench-mode">{visualModeLabel(taskVisualMode(task))}</span></button>
              <button type="button" className="workbench-more" onClick={event => { setDialogInstant(event.detail === 0); setMoreOpen(true) }}><Icon name="settings" size="sm" />更多操作<Icon name="chev-r" size="sm" /></button>
            </div>
          </section>
          <div className="ws-stage-scroll">
            {processing && (
              <div style={{ marginTop: 12, flex: 'none' }}>
                <ProcessStrip status={task.status} stage={task.stage} has_transcription={task.has_transcription} last_job_type={task.last_job_type} has_rag_index={task.has_rag_index} visual_status={task.visual_status} />
              </div>
            )}
            {transcriptionRelevant && <TranscriptionProgressPanel task={task} onProgress={setTranscriptionProgress} />}
            {!transcriptionProgress?.alignment_only && (visualProcessing || taskVisualMode(task) !== 'off' && (task.stage === 'transcribing' || task.stage === 'visual_indexing' || task.last_job_type === 'transcribe')) && <VisualProgressPanel task={task} />}
            {processing && canGenerateSummary(task) && <p className="muted" role="status">转写已保存，可以生成摘要；画面分析和检索索引会继续处理。</p>}
            {study.error && <div className="artifact-notice" role="status">{study.error}</div>}

            {relatedArtifacts.error && <div className="artifact-notice danger" role="alert">相关笔记读取失败：{artifactError(relatedArtifacts.error)}<button className="btn btn-sm" onClick={() => void relatedArtifacts.refetch()}>重试</button></div>}
            {((!task.has_summary && generatingSummary) || (!task.has_summary && task.summary_progress)) && (
              <div className="ws-action-status">
                {!task.has_summary && generatingSummary && (
                  <span className="muted" style={{ fontSize: 12 }} role="status">{summaryStatusText(task)}，完成后会显示在这里</span>
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

            {(failed || summaryFailure) && (
              <div className="card card-pad" style={{ marginTop: 14, flex: 'none', borderColor: 'color-mix(in srgb, var(--bad) 35%, transparent)' }}>
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
                    {urlJob && !summaryFailure ? (
                      <span className="chip chip-mute" style={{ marginTop: 10 }}>URL 任务,请删除后重新添加</span>
                    ) : !summaryFailure?.scheduled && (
                      <button
                        className="btn btn-sm"
                        style={{ marginTop: 10 }}
                        disabled={busy !== '' || readOnly}
                        onClick={() => task.last_job_type === 'visual' ? openVisualSettings() : setPendingAction({
                          kind: summaryFailure || task.last_job_type === 'analyze' ? 'analyze' : task.last_job_type === 'index' ? 'index' : transcriptionProgress?.alignment_only ? 'align' : 'transcribe',
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
      {questionsOpen && (
        <Modal title="从这些问题开始" className={`workbench-dialog workbench-questions-dialog${dialogInstant ? ' workbench-instant' : ''}`} width={640} onClose={() => setQuestionsOpen(false)}>
          <VideoQuestionsPanel taskId={task.id} revision={task.updated_at} />
        </Modal>
      )}
      {visualSettingsOpen && (
        <Modal
          title="选择画面分析方式"
          className={`workbench-dialog workbench-visual-dialog${dialogInstant ? ' workbench-instant' : ''}`}
          width={580}
          onClose={() => { if (!visualSettingBusy) setVisualSettingsOpen(false) }}
          footer={(
            <>
              <button className="btn" disabled={readOnly || processing || visualProcessing || visualSettingBusy} onClick={() => void saveVisualMode()}>{visualSettingBusy && !visualBuildBusy ? '保存中…' : '保存设置'}</button>
              {visualDraft !== 'off' && <button className="btn btn-primary" disabled={readOnly || !visualCapability.ready || visualSettingBusy} onClick={() => videoPreflight.request('生成画面证据', () => { void saveVisualMode(true) }, visualDraft === 'ocr' ? 'ocr' : 'caption')}><Icon name="photo" size="sm" />{visualSettingBusy ? '提交中…' : frames.length ? '保存并重建画面' : '保存并生成画面'}</button>}
            </>
          )}
        >
          <p className="workbench-dialog-intro">补充视频声音之外的信息。按画面内容选择，转写和摘要始终可以单独使用。</p>
          <fieldset className="workbench-visual-choices" disabled={readOnly || !visualCapability.ready || visualSettingBusy}>
            <legend className="workbench-sr-only">画面分析方式</legend>
            {visualChoices.map(choice => (
              <label key={choice.mode} className={`workbench-visual-choice${visualDraft === choice.mode ? ' selected' : ''}`}>
                <input type="radio" name="visual-mode" value={choice.mode} checked={visualDraft === choice.mode} onChange={() => setVisualDraft(choice.mode)} />
                <span className="workbench-choice-icon"><Icon name={choice.icon} /></span>
                <span className="workbench-choice-copy"><strong>{choice.title}{choice.mode === 'off' && <small>默认</small>}</strong><span>{choice.description}</span></span>
                <span className="workbench-choice-check" aria-hidden="true"><Icon name="check" size="sm" /></span>
              </label>
            ))}
          </fieldset>
          <div className="workbench-visual-note">
            <Icon name="bulb" size="sm" />
            <p>{processing || visualProcessing ? '当前视频正在处理，完成后可修改画面设置。' : readOnly ? '演示账号可以查看设置，无法修改。' : '保存设置会用于下次转写及问答。已有证据会保留；生成或重建画面会单独处理视频并更新索引，无需再次转写。'}{!visualCapability.ready ? visualCapability.reason : !task.has_transcription ? '无声演示也可以直接分析画面，无需先转写。' : ''}</p>
          </div>
        </Modal>
      )}
      {moreOpen && (
        <Modal title="更多视频操作" className={`workbench-dialog${dialogInstant ? ' workbench-instant' : ''}`} width={520} onClose={() => setMoreOpen(false)}>
          <section className="workbench-more-group" aria-label="学习笔记与成果">
            <h4>学习笔记与成果</h4>
            {readableArtifact && <button className="workbench-operation" disabled={readOnly || !studyCapability.ready} title={studyCapability.reason || undefined} onClick={() => { setMoreOpen(false); setArtifactMode('new') }}><Icon name="plus" /><span><strong>新建另一份学习笔记</strong><small>为这段视频整理不同主题的笔记</small></span><Icon name="chev-r" size="sm" /></button>}
            {readableArtifact && <button className="workbench-operation" disabled={readOnly || !studyCapability.ready} title={studyCapability.reason || undefined} onClick={() => { setMoreOpen(false); setArtifactMode('reorganize') }}><Icon name="sort" /><span><strong>重新整理这份笔记</strong><small>保留已有版本，生成新的整理结果</small></span><Icon name="chev-r" size="sm" /></button>}
            <Link className="workbench-operation" href={`/artifacts?source=${task.id}`}><Icon name="list" /><span><strong>相关成果</strong><small>查看来自这段视频的笔记与产出</small></span><Icon name="chev-r" size="sm" /></Link>
          </section>
          <section className="workbench-more-group" aria-label="内容处理">
            <h4>内容处理</h4>
            {task.has_transcription ? (
              <button className="workbench-operation" disabled={readOnly || busy !== '' || processing || checkingTranscription} onClick={() => { setMoreOpen(false); setPendingAction({ kind: 'transcribe', force: !resumeTranscription, title: resumeTranscription ? '继续转写？' : '重新转写？', body: resumeTranscription ? resumeTranscriptionBody : '会清除旧分片并再次调用语音识别，可能产生新的 ASR 费用。', confirmLabel: resumeTranscription ? '继续转写' : '重新转写' }) }}><Icon name="refresh" /><span><strong>{resumeTranscription ? '继续转写' : '重新转写'}</strong><small>{resumeTranscription ? '保留已完成分片，继续处理未完成部分' : '重新识别视频音频，更新转写内容'}</small></span><Icon name="chev-r" size="sm" /></button>
            ) : <button className="workbench-operation" disabled={readOnly || busy !== '' || processing} onClick={() => { setMoreOpen(false); void runAction('transcribe') }}><Icon name="activity" /><span><strong>{processing ? '转写处理中' : '开始转写'}</strong><small>将视频声音转换为可检索的文字</small></span><Icon name="chev-r" size="sm" /></button>}
            <button className="workbench-operation" disabled={busy !== '' || !index || index.status === 'indexing' || index.status === 'queued'} onClick={() => { if (index) { setMoreOpen(false); setPendingAction(indexConfirm(index)) } }}><Icon name="layers" /><span><strong>{indexActionLabel(index)}</strong><small>更新视频问答使用的内容检索索引</small></span><Icon name="chev-r" size="sm" /></button>
          </section>
          <section className="workbench-more-group" aria-label="导出与知识库">
            <h4>导出与知识库</h4>
            <button className="workbench-operation" disabled={busy !== ''} onClick={() => { setMoreOpen(false); void downloadMedia() }}><Icon name="download" /><span><strong>下载视频</strong><small>保存原始视频文件</small></span><Icon name="chev-r" size="sm" /></button>
            <button className="workbench-operation" disabled={readOnly} onClick={() => { setMoreOpen(false); setKbOpen(true) }}><Icon name="folder" /><span><strong>加入知识库</strong><small>和其他视频一起检索与问答</small></span><Icon name="chev-r" size="sm" /></button>
          </section>
        </Modal>
      )}
      {summaryOpen && task.summary && (
        <Modal title="AI 摘要" className="modal-read" onClose={() => setSummaryOpen(false)}>
          <SummaryRevisionPanel taskId={task.id} readOnly={readOnly} onChanged={async () => { setTask(await api.getTask(task.id)) }} />
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
      {videoPreflight.dialog}
    </div>
  )
}
