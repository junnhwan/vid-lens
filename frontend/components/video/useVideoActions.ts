import { useRef, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import { useAIAvailability } from '@/components/settings/useAIAvailability'
import { useVideoAIPreflight } from '@/components/settings/VideoAIPreflight'
import { useToast } from '@/components/Toast'
import type { VisualMode } from '@/lib/types'
import { visualAvailability } from '@/lib/taskCapabilities'
import type { useVideoWorkbenchData } from './useVideoWorkbenchData'
import { visualModeLabel } from './videoView'
export type ActionKind = 'transcribe' | 'align' | 'analyze' | 'index' | 'download'
export type ConfirmAction = { kind: Exclude<ActionKind, 'download'>; force?: boolean; title: string; body: string; confirmLabel: string }
export function useVideoActions(data: ReturnType<typeof useVideoWorkbenchData>, readOnly: boolean) {
 const { task, setTask } = data
 const toast = useToast()
 const videoPreflight = useVideoAIPreflight()
 const ai = useAIAvailability(readOnly, 'summary')
 const asrAI = useAIAvailability(readOnly, 'transcribe')
 const alignAI = useAIAvailability(readOnly, 'align')
 const indexAI = useAIAvailability(readOnly, 'index')
 const [busy, setBusy] = useState<ActionKind | ''>('')
 const inFlight = useRef(false)
 const [titleBusy,setTitleBusy] = useState(false)
 const [titleDraft,setTitleDraft] = useState('')
 const [editingTitle,setEditingTitle] = useState(false)
 const [visualSettingBusy,setVisualSettingBusy] = useState(false)
 const [visualBuildBusy,setVisualBuildBusy] = useState(false)
 const [visualDraft,setVisualDraft] = useState<VisualMode>('off')
 const [visualSettingsOpen,setVisualSettingsOpen] = useState(false)
 const [pendingAction,setPendingAction] = useState<ConfirmAction | null>(null)
 const visualAI = useAIAvailability(readOnly, visualDraft === 'ocr' ? 'ocr' : 'caption')
 const ocrAI = useAIAvailability(readOnly, 'ocr')
 const visualCapability = task ? visualAvailability(task) : { ready:false, reason:'正在加载视频' }
  const performAction = async (kind: Exclude<ActionKind, 'download'>, force = false) => {
    if (!task || inFlight.current) return
    const admission = kind === 'transcribe' ? asrAI : kind === 'align' ? alignAI : kind === 'index' ? indexAI : ai
    if (!admission.ready) { toast.info(admission.reason); return }
    inFlight.current = true
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
        data.setIndex(r)
        if (r.status === 'indexed') toast.success('索引构建完成')
        else toast.info(r.status === 'queued' ? '索引任务正在排队' : '索引正在构建中')
      }
      await data.refreshAfterAction()
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '操作失败')
    } finally {
      inFlight.current = false
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
    if (!task || inFlight.current) return
    inFlight.current = true
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
      inFlight.current = false
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

  const saveVisualMode = async (build = false) => {
    if (!task || inFlight.current) return
    if (readOnly || (build && (!visualAI.ready || visualDraft === 'both' && !ocrAI.ready || !visualCapability.ready))) { toast.info(readOnly ? '演示模式不可处理视频' : !visualAI.ready ? visualAI.reason : visualDraft === 'both' && !ocrAI.ready ? ocrAI.reason : visualCapability.reason); return }
    inFlight.current = true
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
      inFlight.current = false
      setVisualSettingBusy(false)
    }
  }

 return { ai, videoPreflight, busy, titleBusy, titleDraft,setTitleDraft,editingTitle,setEditingTitle,visualSettingBusy,visualBuildBusy,visualDraft,setVisualDraft,visualSettingsOpen,setVisualSettingsOpen,pendingAction,setPendingAction,runAction,downloadMedia,saveTitle,saveVisualMode,confirmAndRun }
}
