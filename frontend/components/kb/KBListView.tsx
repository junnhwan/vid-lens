'use client'

import { useState } from 'react'
import Link from 'next/link'
import { Plus, Trash2, Library, ArrowRight, Users, Video } from 'lucide-react'
import type { KnowledgeBase } from '@/lib/types'
import AppShell, { PageHero } from '@/components/layout/AppShell'
import { fmtShortDate } from '@/components/chat/chatUtils'

interface Props {
  kbs: KnowledgeBase[]
  loading: boolean
  error: string
  isDemo: boolean
  onRefresh: () => void
  onCreate: () => void
  onManage: (kb: KnowledgeBase) => void
  onDelete: (id: number) => void
}

export default function KBListView({ kbs, loading, error, isDemo, onRefresh, onCreate, onManage, onDelete }: Props) {
  const [hoverId, setHoverId] = useState<number | null>(null)

  return (
    <AppShell>
      <PageHero
        kicker="知识库"
        title="跨视频问答"
        desc="将多个视频归入同一知识库，基于全部转写内容做严格 RAG 检索，引用标注来源视频。"
        actions={!isDemo ? (
          <button
            onClick={onCreate}
            className="h-9 px-4 rounded-lg bg-ink-0 text-paper-0 text-[12px] flex items-center gap-1.5 ui-btn-lift shrink-0"
          >
            <Plus className="w-3.5 h-3.5" />新建知识库
          </button>
        ) : undefined}
      />

      <main className="flex-1 overflow-y-auto px-8 pb-24">
        {error && (
          <div className="py-4 px-4 mb-4 text-[13px] text-sienna-700 bg-sienna-500/8 border border-sienna-500/20 rounded-lg">
            {error}
            <button onClick={onRefresh} className="ml-2 underline">重试</button>
          </div>
        )}

        {loading ? (
          <div className="space-y-3">
            {[0, 1, 2].map(i => (
              <div key={i} className="h-28 bg-paper-0 border border-ink-0/8 rounded-xl sk" />
            ))}
          </div>
        ) : kbs.length === 0 ? (
          <div className="py-20 text-center ui-fade-in">
            <Library className="w-10 h-10 text-ink-5 mx-auto mb-3" />
            <div className="text-[15px] text-ink-2">还没有知识库</div>
            <p className="text-[13px] text-ink-4 mt-1">创建知识库，把相关视频组织在一起做跨卷问答</p>
            {!isDemo && (
              <button
                onClick={onCreate}
                className="mt-4 h-9 px-4 rounded-lg bg-ink-0 text-paper-0 text-[12px] inline-flex items-center gap-1.5 ui-btn-lift"
              >
                <Plus className="w-3.5 h-3.5" />创建第一个
              </button>
            )}
          </div>
        ) : (
          <div className="grid gap-4 md:grid-cols-2">
            {kbs.map((kb, i) => (
              <article
                key={kb.id}
                onMouseEnter={() => setHoverId(kb.id)}
                onMouseLeave={() => setHoverId(null)}
                className="bg-paper-0 border border-ink-0/8 rounded-xl p-5 ui-card-hover ui-fade-in"
                style={{ animationDelay: `${i * 60}ms` }}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <h3 className="text-[17px] font-semibold text-ink-0 ui-serif truncate">{kb.name}</h3>
                      <span className="text-[10px] text-ink-4 font-mono">KB-{pad(kb.id)}</span>
                    </div>
                    {kb.description && (
                      <p className="text-[13px] text-ink-3 mt-1 line-clamp-2 leading-relaxed">{kb.description}</p>
                    )}
                  </div>
                  {!isDemo && (
                    <button
                      onClick={() => onDelete(kb.id)}
                      className="w-8 h-8 rounded-lg border border-ink-0/10 flex items-center justify-center text-ink-4 hover:text-rust hover:border-rust/30 transition-colors shrink-0"
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </button>
                  )}
                </div>

                <div className="flex items-center gap-4 mt-4 text-[11px] text-ink-4">
                  <span className="flex items-center gap-1"><Users className="w-3 h-3" />{kb.member_count} 个视频</span>
                  <span className="flex items-center gap-1">
                    <Video className="w-3 h-3" />{kb.videos?.filter(v => v.retrievable).length ?? 0} 可检索
                  </span>
                  <span>创建于 {fmtShortDate(kb.created_at)}</span>
                </div>

                {kb.videos && kb.videos.length > 0 && (
                  <div className="mt-3 flex gap-1.5 overflow-hidden">
                    {kb.videos.slice(0, 4).map((v, vi) => (
                      <span
                        key={v.task_id}
                        className={`text-[10px] px-2 py-1 rounded-md truncate max-w-[120px] transition-all duration-200 ${
                          v.retrievable
                            ? 'bg-moss/10 text-moss border border-moss/20'
                            : 'bg-paper-1 text-ink-4 border border-ink-0/8'
                        } ${hoverId === kb.id ? 'translate-y-0' : ''}`}
                        style={{ transitionDelay: `${vi * 30}ms` }}
                      >
                        {v.title}
                      </span>
                    ))}
                    {kb.videos.length > 4 && (
                      <span className="text-[10px] px-2 py-1 text-ink-4">+{kb.videos.length - 4}</span>
                    )}
                  </div>
                )}

                <div className="flex gap-2 mt-4 pt-4 border-t border-ink-0/6">
                  {!isDemo && (
                    <button
                      onClick={() => onManage(kb)}
                      className="h-8 px-3 rounded-lg border border-ink-0/10 text-[11px] text-ink-2 hover:bg-paper-1 ui-btn-lift"
                    >
                      管理成员
                    </button>
                  )}
                  <Link
                    href={`/kb/${kb.id}`}
                    className="h-8 px-4 rounded-lg bg-ink-0 text-paper-0 text-[11px] flex items-center gap-1.5 ui-btn-lift ml-auto"
                  >
                    去问答 <ArrowRight className="w-3 h-3" />
                  </Link>
                </div>
              </article>
            ))}
          </div>
        )}

        {!loading && kbs.length > 0 && (
          <button onClick={onRefresh} className="mt-6 text-[12px] text-ink-4 hover:text-ink-2">刷新列表</button>
        )}
      </main>
    </AppShell>
  )
}

function pad(n: number) { return n < 10 ? `0${n}` : `${n}` }
