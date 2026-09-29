import { useCallback, useEffect, useState } from 'react'
import { formatClock } from '@/lib/format'
import { useRouter } from '@/lib/router'
import { api } from '@/lib/api'
import { Icon } from '@/components/ui/Icon'
import type { VideoQuestionResult } from '@/lib/types'

export function VideoQuestionsPanel({ taskId, revision }: { taskId: number; revision: string }) {
  const router = useRouter()
  const [result, setResult] = useState<VideoQuestionResult | null>(null)
  const [loading, setLoading] = useState(false)
  const load = useCallback(async () => {
    setLoading(true)
    try { setResult(await api.getVideoQuestions(taskId)) }
    catch { setResult({ status: 'no_evidence', message: '推荐问题暂时不可用，可以直接进入问答。', questions: [] }) }
    finally { setLoading(false) }
  }, [taskId])
  useEffect(() => { void load() }, [load, revision])
  return <section className="video-questions-panel" aria-label="基于视频内容的推荐问题">
    <div className="video-questions-head">
      <strong className="vq-title"><Icon name="bulb" size="sm" />可以问这段视频{!!result?.questions.length && <em className="vq-count">{result.questions.length}</em>}</strong>
      <button type="button" className="btn btn-sm btn-ghost" onClick={() => void load()} disabled={loading}><Icon name="refresh" size="sm" />{loading ? '读取中…' : '刷新推荐'}</button>
    </div>
    <p>{result?.message || '正在读取已有转写、摘要和画面证据…'}</p>
    {!!result?.questions.length && <div className="video-questions-list">{result.questions.map((item, i) => (
      <button
        key={item.question}
        type="button"
        className="vq-row"
        style={{ animationDelay: `${Math.min(i, 6) * 45}ms` }}
        onClick={() => router.push(`/chat/v/${taskId}?ask=${encodeURIComponent(item.question)}`)}
      >
        <span className="vq-ico" aria-hidden="true"><Icon name="message" size="sm" /></span>
        <span className="vq-q">{item.question}</span>
        <small className="vq-src">{item.source}{item.time_ms != null ? ` · ${formatClock(item.time_ms)}` : ''}</small>
        <Icon name="chev-r" size="sm" className="vq-go" />
      </button>
    ))}</div>}
  </section>
}
