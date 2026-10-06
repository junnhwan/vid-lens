import { useEffect, useMemo } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, getToken } from '@/lib/api'
import { artifactApi } from '@/lib/artifacts/api'
import type { VideoTask } from '@/lib/types'
import { summaryRunning } from '@/lib/summaryState'
import { summaryFailureView } from '@/lib/summaryFailure'

export function videoIsProcessing(task?: VideoTask | null) {
  return !!task && (task.status === 1 || task.status === 2 || summaryRunning(task) ||
    !!summaryFailureView(task)?.scheduled || ['queued', 'running'].includes(task.visual_status))
}
export function transcriptionRelevant(task?: VideoTask | null) {
  return !!task && (task.last_job_type === 'transcribe' || ['transcribing', 'aligning', 'visual_indexing'].includes(task.stage))
}
export function useVideoWorkbenchData(taskId: number, enabled: boolean) {
  const client = useQueryClient()
  const token = getToken()
  const scope = useMemo(() => ['video-workbench', token, taskId] as const, [token, taskId])
  const valid = enabled && Number.isSafeInteger(taskId) && taskId > 0
  const taskQuery = useQuery({ queryKey: [...scope, 'task'], queryFn: ({ signal }) => api.getTask(taskId, signal),
    enabled: valid, retry: 1, refetchInterval: query => videoIsProcessing(query.state.data) ? 5000 : false })
  const task = taskQuery.data ?? null
  const active = videoIsProcessing(task)
  const timelineQuery = useQuery({ queryKey: [...scope, 'timeline'], queryFn: ({ signal }) => api.getTimeline(taskId, signal), enabled: valid && !!task, retry: 1 })
  const indexQuery = useQuery({ queryKey: [...scope, 'index'], queryFn: ({ signal }) => api.getRagIndex(taskId, signal), enabled: valid && !!task, retry: 1,
    refetchInterval: query => active || ['queued', 'indexing'].includes(query.state.data?.status ?? '') ? 5000 : false })
  const playbackQuery = useQuery({ queryKey: [...scope, 'playback'], queryFn: ({ signal }) => api.playbackSrc(taskId, signal), enabled: valid && !!task, retry: 1 })
  const progressQuery = useQuery({ queryKey: [...scope, 'transcription-progress'], queryFn: ({ signal }) => api.getTranscriptionProgress(taskId, signal),
    enabled: valid && transcriptionRelevant(task), retry: false, refetchInterval: active ? 5000 : false })
  const visualQuery = useQuery({ queryKey: [...scope, 'visual-progress'], queryFn: ({ signal }) => api.getVisualProgress(taskId, signal),
    enabled: valid && !!task && (task.visual_status !== '' || task.visual_mode !== 'off' && !task.visual_disabled), retry: 1, refetchInterval: active ? 5000 : false })
  const relatedArtifacts = useQuery({ queryKey: [...scope, 'artifacts'], queryFn: async ({ signal }) => {
    const first = await artifactApi.list(1, taskId, signal)
    const list = [...first.list]
    for (let page = 2; list.length < first.total && !list.some(item => item.current_version_id); page++) {
      const next = await artifactApi.list(page, taskId, signal)
      if (!next.list.length) break
      list.push(...next.list)
    }
    return { ...first, list }
  }, enabled: valid && !!task, refetchInterval: query => query.state.data?.list.some(item => item.latest_run && ['queued', 'running'].includes(item.latest_run.status)) ? 15000 : false })
  // A publication or processing transition refreshes retained evidence once.
  // Query keys and consumed signals isolate requests across task/account changes.
  const revision = task ? `${task.updated_at}:${task.status}:${task.visual_status}:${task.has_transcription}` : ''
  useEffect(() => {
    if (revision) {
      void client.invalidateQueries({ queryKey: [...scope, 'timeline'] })
      void client.invalidateQueries({ queryKey: [...scope, 'index'] })
    }
  }, [client, scope, revision])
  const refreshTask = () => taskQuery.refetch()
  const refreshEvidence = () => Promise.all([timelineQuery.refetch(), indexQuery.refetch(), playbackQuery.refetch(), progressQuery.refetch()])
  const refreshAfterAction = async () => { await Promise.all(['task', 'timeline', 'index', 'transcription-progress', 'visual-progress'].map(key => client.invalidateQueries({ queryKey: [...scope, key] }))) }
  const setTask = (value: VideoTask) => client.setQueryData([...scope, 'task'], value)
  const setIndex = (value: NonNullable<typeof indexQuery.data>) => client.setQueryData([...scope, 'index'], value)
  const errors = [timelineQuery.error, indexQuery.error, playbackQuery.error].filter(Boolean)
  return { task, setTask, setIndex, timeline: timelineQuery.data ?? null, index: indexQuery.data ?? null,
    playbackUrl: playbackQuery.data ?? null, transcriptionProgress: progressQuery.data ?? null,
    progressQuery, visualQuery, relatedArtifacts, loading: taskQuery.isPending,
    loadError: taskQuery.error ? taskQuery.error instanceof ApiError ? taskQuery.error.message : '视频加载失败' : '',
    subError: errors.length ? '部分数据加载失败，请重试' : '', refreshTask, refreshEvidence, refreshAfterAction,
    refreshPlaybackUrl: async () => (await playbackQuery.refetch()).data ?? null }
}
