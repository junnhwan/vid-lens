'use client'

import { Suspense, useCallback, useEffect, useState } from 'react'
import Link from 'next/link'
import { useRouter, useSearchParams } from 'next/navigation'
import { Video, Library, ChevronRight, ArrowRight, Search } from 'lucide-react'
import AppShell, { PageHero } from '@/components/layout/AppShell'
import { api, ApiError } from '@/lib/api'
import { taskTitle } from '@/lib/format'
import { TaskStatusEnum } from '@/lib/types'
import type { KnowledgeBase, VideoTask } from '@/lib/types'

export default function QaHubPage() {
  return (
    <Suspense fallback={<div className="h-screen bg-paper-1" />}>
      <QaHubView />
    </Suspense>
  )
}

function QaHubView() {
  const router = useRouter()
  const sp = useSearchParams()
  const [q, setQ] = useState(sp.get('q') || '')
  const [tasks, setTasks] = useState<VideoTask[]>([])
  const [kbs, setKbs] = useState<KnowledgeBase[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [taskRes, kbList] = await Promise.all([
        api.listTasks(1, 100, ''),
        api.listKBs(),
      ])
      const qaTasks = (taskRes.list || []).filter(
        t => t.status === TaskStatusEnum.Completed && t.has_transcription,
      )
      setTasks(qaTasks)
      setKbs(kbList || [])
      setErr('')
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  useEffect(() => {
    const params = new URLSearchParams()
    if (q) params.set('q', q)
    const next = params.toString()
    router.replace(next ? `/qa?${next}` : '/qa', { scroll: false })
  }, [q, router])

  const needle = q.trim().toLowerCase()
  const videos = tasks.filter(t => {
    if (!needle) return true
    return taskTitle(t).toLowerCase().includes(needle) || String(t.id).includes(needle)
  })
  const kbFiltered = kbs.filter(k => {
    if (!needle) return true
    return k.name.toLowerCase().includes(needle) || String(k.id).includes(needle)
  })

  return (
    <AppShell>
      <div className="flex-1 overflow-y-auto scroll-thin">
        <PageHero
          kicker="问答"
          title="开始问答"
          desc="选择单视频或知识库进入聊天。单视频仅检索本卷转写；知识库可跨成员视频检索并标注来源。"
        />

        <div className="px-8 pb-10 max-w-3xl">
          <div className="relative mb-8 ui-fade-in">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-ink-4" />
            <input
              value={q}
              onChange={e => setQ(e.target.value)}
              placeholder="搜索视频或知识库…"
              className="w-full h-11 pl-10 pr-4 rounded-xl border border-ink-0/10 bg-paper-0 text-[14px] focus:outline-none focus:ring-2 focus:ring-sienna-500/20"
            />
          </div>

          {err && <p className="text-[13px] text-red-600 mb-4">{err}</p>}
          {loading && <p className="text-[13px] text-ink-4">加载中…</p>}

          {!loading && (
            <>
              <section className="mb-8 ui-fade-in">
                <h2 className="text-[12px] uppercase tracking-wider text-ink-4 mb-3 flex items-center gap-2">
                  <Video className="w-4 h-4" />
                  单视频问答
                  <span className="text-ink-5 font-normal">· 仅本视频转写</span>
                </h2>
                <div className="space-y-2">
                  {videos.length === 0 ? (
                    <p className="text-[13px] text-ink-4 py-4">暂无已完成转写的视频，请先在视频库上传并等待转写完成。</p>
                  ) : videos.map(v => (
                    <Link
                      key={v.id}
                      href={`/chat/${v.id}`}
                      className="flex items-center gap-4 p-4 rounded-xl border border-ink-0/8 bg-paper-0 ui-card-hover"
                    >
                      <div className="w-10 h-10 rounded-lg bg-sienna-500/15 text-sienna-700 flex items-center justify-center text-[11px] font-bold shrink-0">
                        1V
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="font-medium text-ink-0 truncate">{taskTitle(v)}</div>
                        <div className="text-[11px] text-ink-4">任务 #{v.id} · 严格 RAG · 引用可追溯</div>
                      </div>
                      <ChevronRight className="w-4 h-4 text-ink-4 shrink-0" />
                    </Link>
                  ))}
                </div>
              </section>

              <section className="ui-fade-in">
                <h2 className="text-[12px] uppercase tracking-wider text-ink-4 mb-3 flex items-center gap-2">
                  <Library className="w-4 h-4" />
                  知识库问答
                  <span className="text-ink-5 font-normal">· 跨视频检索</span>
                </h2>
                <div className="space-y-2">
                  {kbFiltered.length === 0 ? (
                    <p className="text-[13px] text-ink-4 py-4">暂无知识库，请先在知识库页创建并添加成员视频。</p>
                  ) : kbFiltered.map(k => (
                    <Link
                      key={k.id}
                      href={`/kb/${k.id}`}
                      className="flex items-center gap-4 p-4 rounded-xl border border-ink-0/8 bg-paper-0 ui-card-hover"
                    >
                      <div className="w-10 h-10 rounded-lg bg-ink-0/8 text-ink-2 flex items-center justify-center text-[11px] font-bold shrink-0">
                        KB
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="font-medium text-ink-0 truncate">{k.name}</div>
                        <div className="text-[11px] text-ink-4">
                          跨 {k.member_count ?? k.videos?.length ?? 0} 个视频 · 引用标注来源
                        </div>
                      </div>
                      <ArrowRight className="w-4 h-4 text-ink-4 shrink-0" />
                    </Link>
                  ))}
                </div>
              </section>
            </>
          )}
        </div>
      </div>
    </AppShell>
  )
}
