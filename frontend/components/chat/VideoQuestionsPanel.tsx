import { useCallback, useEffect, useState } from 'react'
import { formatClock } from '@/lib/format'
import { useRouter } from '@/lib/router'
import { api } from '@/lib/api'
import { Icon } from '@/components/ui/Icon'
import type { VideoQuestionResult } from '@/lib/types'
import { QuestionSuggestionsLoading } from './QuestionSuggestionsLoading'

export function VideoQuestionsPanel({ taskId }: { taskId: number; revision: string }) {
  const router = useRouter()
  const [result, setResult] = useState<VideoQuestionResult | null>(null)
  const [loading, setLoading] = useState(true)
  const load = useCallback(async () => {
    setLoading(true)
    try { setResult(await api.generateVideoQuestions(taskId)) }
    catch { setResult({ status: 'no_evidence', message: '推荐问题暂时不可用，可以直接进入问答。', questions: [] }) }
    finally { setLoading(false) }
  }, [taskId])
  // A panel opens on demand. Task progress updates must not trigger model calls.
  useEffect(() => { void load() }, [load])
  return <section className="video-questions-panel" aria-label="基于视频内容的推荐问题" aria-busy={loading}>
    <div className="video-questions-head">
      <strong className="vq-title"><Icon name="bulb" size="sm" />可以问这段视频{!!result?.questions.length && <em className="vq-count">{result.questions.length}</em>}</strong>
    </div>
    {loading ? <QuestionSuggestionsLoading /> : <p>{result?.message}</p>}
    {!loading && !!result?.questions.length && <div className="video-questions-list">{result.questions.map((item, i) => (
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
