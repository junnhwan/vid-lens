'use client'

import { useState } from 'react'
import Link from 'next/link'
import { Plus, Trash2, Library, ArrowRight, Users, Video } from 'lucide-react'
import type { KnowledgeBase } from '@/lib/types'
import { ProtoShell, PageHero } from '@/components/prototype/c/Shell'

interface Props {
  kbs: KnowledgeBase[]
  loading: boolean
  error: string
  onRefresh: () => void
}

export default function KBListView({ kbs, loading, error, onRefresh }: Props) {
  const [hoverId, setHoverId] = useState<number | null>(null)

  return (
    <ProtoShell active="kb">
      <PageHero
        kicker="知识库"
        title="跨视频问答"
        desc="将多个视频归入同一知识库，基于全部转写内容做严格 RAG 检索，引用标注来源视频。"
        actions={
          <button className="h-9 px-4 rounded-lg bg-stone-900 text-white text-[12px] flex items-center gap-1.5 proto-btn-lift">
            <Plus className="w-3.5 h-3.5" />新建知识库
          </button>
        }
      />

      <main className="flex-1 overflow-y-auto px-8 pb-24">
        {error && (
          <div className="py-4 px-4 mb-4 text-[13px] text-amber-800 bg-amber-50 border border-amber-200 rounded-lg">{error}</div>
        )}

        {loading ? (
          <div className="space-y-3">
            {[0, 1, 2].map(i => (
              <div key={i} className="h-28 bg-white border border-stone-200 rounded-xl animate-pulse" />
            ))}
          </div>
        ) : kbs.length === 0 ? (
          <div className="py-20 text-center proto-fade-in">
            <Library className="w-10 h-10 text-stone-300 mx-auto mb-3" />
            <div className="text-[15px] text-stone-600">还没有知识库</div>
            <p className="text-[13px] text-stone-400 mt-1">创建知识库，把相关视频组织在一起做跨卷问答</p>
          </div>
        ) : (
          <div className="grid gap-4 md:grid-cols-2">
            {kbs.map((kb, i) => (
              <article
                key={kb.id}
                onMouseEnter={() => setHoverId(kb.id)}
                onMouseLeave={() => setHoverId(null)}
                className="bg-white border border-stone-200 rounded-xl p-5 proto-card-hover proto-fade-in"
                style={{ animationDelay: `${i * 60}ms` }}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <h3 className="text-[17px] font-semibold text-stone-900 proto-serif truncate">{kb.name}</h3>
                      <span className="text-[10px] text-stone-400 font-mono">KB-{pad(kb.id)}</span>
                    </div>
                    {kb.description && (
                      <p className="text-[13px] text-stone-500 mt-1 line-clamp-2 leading-relaxed">{kb.description}</p>
                    )}
                  </div>
                  <button className="w-8 h-8 rounded-lg border border-stone-200 flex items-center justify-center text-stone-400 hover:text-red-600 hover:border-red-200 transition-colors shrink-0">
                    <Trash2 className="w-3.5 h-3.5" />
                  </button>
                </div>

                <div className="flex items-center gap-4 mt-4 text-[11px] text-stone-400">
                  <span className="flex items-center gap-1"><Users className="w-3 h-3" />{kb.member_count} 个视频</span>
                  <span className="flex items-center gap-1"><Video className="w-3 h-3" />{kb.videos?.filter(v => v.retrievable).length ?? 0} 可检索</span>
                  <span>创建于 {fmt(kb.created_at)}</span>
                </div>

                {/* 成员预览条 */}
                {kb.videos && kb.videos.length > 0 && (
                  <div className="mt-3 flex gap-1.5 overflow-hidden">
                    {kb.videos.slice(0, 4).map((v, vi) => (
                      <span
                        key={v.task_id}
                        className={`text-[10px] px-2 py-1 rounded-md truncate max-w-[120px] transition-all duration-200 ${
                          v.retrievable ? 'bg-emerald-50 text-emerald-700 border border-emerald-200' : 'bg-stone-50 text-stone-500 border border-stone-200'
                        } ${hoverId === kb.id ? 'translate-y-0' : ''}`}
                        style={{ transitionDelay: `${vi * 30}ms` }}
                      >
                        {v.title}
                      </span>
                    ))}
                    {kb.videos.length > 4 && (
                      <span className="text-[10px] px-2 py-1 text-stone-400">+{kb.videos.length - 4}</span>
                    )}
                  </div>
                )}

                <div className="flex gap-2 mt-4 pt-4 border-t border-stone-100">
                  <button className="h-8 px-3 rounded-lg border border-stone-200 text-[11px] text-stone-600 hover:bg-stone-50 proto-btn-lift">
                    管理成员
                  </button>
                  <Link
                    href={`/prototype/kb/${kb.id}`}
                    className="h-8 px-4 rounded-lg bg-stone-900 text-white text-[11px] flex items-center gap-1.5 proto-btn-lift ml-auto"
                  >
                    去问答 <ArrowRight className="w-3 h-3" />
                  </Link>
                </div>
              </article>
            ))}
          </div>
        )}

        {!loading && kbs.length > 0 && (
          <button onClick={onRefresh} className="mt-6 text-[12px] text-stone-400 hover:text-stone-600">刷新列表</button>
        )}
      </main>
    </ProtoShell>
  )
}

function pad(n: number) { return n < 10 ? `0${n}` : `${n}` }
function fmt(iso: string) {
  const d = new Date(iso)
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}
