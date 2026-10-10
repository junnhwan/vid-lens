import { useEffect, useRef, useState } from 'react'
import { ApiError } from '@/lib/api'
import { summaryExperienceApi, type SummaryGeneration, type SummaryVisualRetryRequest } from '@/lib/summaryExperience'

const messages: Record<string, string> = {
  visual_disabled: '画面处理已关闭，现有文字仍可阅读。',
  summary_generation_disabled: '暂时无法启动新的摘要处理，现有内容仍可阅读。',
  generation_conflict: '原稿已更新，请刷新后再补图。',
  source_changed: '视频来源已改变，请先为当前来源生成摘要。',
  summary_version_conflict: '原稿已更新，请刷新后再补图。',
  generation_changed: '原稿已更新，请刷新后再补图。',
  version_conflict: '原稿已更新，请刷新后再补图。',
  generation_stale: '摘要与当前来源不一致，请先生成当前来源的摘要。',
  generation_source_changed: '视频来源已改变，请先为当前来源生成摘要。',
  summary_generation_active: '已有摘要处理正在进行，可继续阅读并等待结果。',
  frozen_profile_changed: '画面处理配置已改变，请刷新后重试。',
  frozen_profile_unavailable: '画面处理配置暂不可用，现有文字仍可阅读。',
  profile_required: '请先选择可用的 AI 配置，再尝试补图。',
  vision_unavailable: '请先配置画面理解能力，现有文字仍可阅读。',
  visual_retry_requires_text_result: '这份原稿当前无需重试配图。',
}

export function SummaryVisualRetry({ taskId, generation, readOnly, onAccepted }: { taskId: number; generation: SummaryGeneration | null; readOnly: boolean; onAccepted: () => void }) {
  const [state, setState] = useState<'idle' | 'sending' | 'accepted'>('idle'), [error, setError] = useState('')
  const intent = useRef<{ key: string; body: SummaryVisualRetryRequest }>()
  const active = useRef<AbortController>()
  const basis = JSON.stringify([taskId, generation?.generation_id, generation?.result_generation_id, generation?.generated_version, generation?.generated_content_digest, generation?.generated_source_id, generation?.generated_source_digest])
  useEffect(() => {
    active.current?.abort(); intent.current = undefined; setState('idle'); setError('')
    return () => active.current?.abort()
  }, [basis])
  if (readOnly || !generation?.visual_retry_available || !generation.result_generation_id || !generation.generated_content_digest || !generation.generated_source_id || !generation.generated_source_digest) return null
  const start = async () => {
    if (active.current && !active.current.signal.aborted) return
    intent.current ??= { key: `visual-retry-${crypto.randomUUID()}`, body: {
      expected_generation_id: generation.result_generation_id!, expected_generated_version: generation.generated_version,
      expected_content_digest: generation.generated_content_digest!, expected_source_id: generation.generated_source_id!, expected_source_digest: generation.generated_source_digest!, authorize_new_visual_budget: true,
    } }
    const controller = new AbortController(); active.current = controller
    setState('sending'); setError('')
    try {
      await summaryExperienceApi.visualRetry(taskId, intent.current.body, intent.current.key, controller.signal)
      if (controller.signal.aborted) return
      setState('accepted'); onAccepted()
    } catch (reason) {
      if (controller.signal.aborted) return
      setState('idle')
      setError(reason instanceof ApiError && reason.code && messages[reason.code] || '补图请求暂未确认。现有正文保留，可重试同一次请求。')
    } finally {
      if (active.current === controller) active.current = undefined
    }
  }
  return <section className="summary-visual-retry" aria-label="补充摘要画面">
    <p>复用已保存正文，开始一次有预算限制的配图尝试；保留你的修订。</p>
    <button className="btn btn-sm" disabled={state !== 'idle'} onClick={() => void start()}>{state === 'sending' ? '正在提交补图请求…' : state === 'accepted' ? '补图请求已受理' : '只重试配图'}</button>
    {error && <p role="status">{error}</p>}
  </section>
}
