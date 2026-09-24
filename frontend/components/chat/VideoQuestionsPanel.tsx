'use client'

import { useCallback, useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'
import { api } from '@/lib/api'
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
    <div className="video-questions-head"><strong>可以问这段视频</strong><button type="button" onClick={() => void load()} disabled={loading}>{loading ? '读取中…' : '刷新推荐'}</button></div>
    <p>{result?.message || '正在读取已有转写、摘要和画面证据…'}</p>
    {!!result?.questions.length && <div className="video-questions-list">{result.questions.map(item => <button key={item.question} type="button" onClick={() => router.push(`/chat/v/${taskId}?ask=${encodeURIComponent(item.question)}`)}><span>{item.question}</span><small>{item.source}{item.time_ms != null ? ` · ${Math.floor(item.time_ms / 60000)}:${String(Math.floor(item.time_ms / 1000) % 60).padStart(2, '0')}` : ''}</small></button>)}</div>}
  </section>
}
