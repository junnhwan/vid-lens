'use client'

import Link from 'next/link'
import { useState } from 'react'
import { Video, Library, MessageCircle, ArrowRight, Search, ChevronRight } from 'lucide-react'
import { DEMO_KBS, DEMO_VIDEOS } from '@/components/prototype/agent/types'

const NAV_MAIN = [
  { key: 'library', href: '/prototype/dashboard', icon: Video, label: '我的视频', active: false },
  { key: 'qa', href: '/prototype/qa-navigation?variant=AB', icon: MessageCircle, label: '问答', active: true },
  { key: 'kb-manage', href: '/prototype/kb', icon: Library, label: '管理知识库', active: false },
] as const

/** A+B 融合（推荐）：侧栏快捷入口 + 主区问答案例台 */
export function NavVariantAB() {
  const [q, setQ] = useState('')

  const videos = DEMO_VIDEOS.filter(v => !q || v.title.includes(q) || q.length < 2)
  const kbs = DEMO_KBS.filter(k => !q || k.name.includes(q) || q.length < 2)

  return (
    <div className="h-screen flex proto-root bg-[#f7f4ef]">
      <aside className="w-[260px] shrink-0 bg-[#faf8f5] border-r border-stone-200 flex flex-col">
        <div className="p-6 border-b border-stone-200">
          <div className="text-[22px] font-semibold proto-serif text-stone-900">映知</div>
          <p className="text-[11px] text-stone-500 mt-1 italic">观之以映，释之以知</p>
        </div>

        <nav className="p-3 space-y-1">
          {NAV_MAIN.map(({ key, href, icon: Icon, label, active }) => (
            <Link
              key={key}
              href={href}
              className={`flex items-center gap-3 px-3 py-2.5 rounded-lg text-[13px] transition-colors ${
                active ? 'bg-amber-50/80 text-stone-900 font-medium' : 'text-stone-600 hover:bg-stone-100'
              }`}
            >
              <div className={`w-8 h-8 rounded-lg flex items-center justify-center ${active ? 'bg-amber-100 text-amber-800' : 'bg-stone-100 text-stone-500'}`}>
                <Icon className="w-4 h-4" />
              </div>
              {label}
            </Link>
          ))}
        </nav>

        <div className="px-4 pt-2 flex-1 overflow-y-auto">
          <div className="text-[10px] uppercase tracking-wider text-stone-400 mb-2">最近可问答</div>
          <div className="text-[10px] text-amber-800/80 px-2 mb-1">单视频</div>
          {DEMO_VIDEOS.map(v => (
            <Link key={v.id} href={`/prototype/chat/${v.id}`} className="flex items-center gap-2 px-2 py-1.5 rounded-lg text-[12px] text-stone-700 hover:bg-amber-50 proto-row-hover">
              <MessageCircle className="w-3 h-3 text-amber-700 shrink-0" />
              <span className="truncate">{v.title}</span>
            </Link>
          ))}
          <div className="text-[10px] text-violet-700/80 px-2 mt-3 mb-1">知识库</div>
          {DEMO_KBS.map(k => (
            <Link key={k.id} href={`/prototype/kb/${k.id}`} className="flex items-center gap-2 px-2 py-1.5 rounded-lg text-[12px] text-stone-700 hover:bg-violet-50 proto-row-hover">
              <Library className="w-3 h-3 text-violet-600 shrink-0" />
              <span className="truncate">{k.name}</span>
            </Link>
          ))}
        </div>
      </aside>

      <main className="flex-1 overflow-y-auto px-8 py-10">
        <div className="max-w-3xl mx-auto proto-fade-in">
          <div className="text-[11px] text-emerald-700 font-medium mb-2">推荐方案 · 侧栏快捷入口 + 问答案例台</div>
          <h1 className="text-[32px] font-semibold proto-serif text-stone-900">开始问答</h1>
          <p className="text-[14px] text-stone-500 mt-2">
            左侧随时跳进最近视频/知识库；主区按「单视频 / 知识库」分类浏览全部可问答对象。
          </p>

          <div className="relative mt-8 mb-6">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-stone-400" />
            <input
              value={q}
              onChange={e => setQ(e.target.value)}
              placeholder="搜索视频或知识库…"
              className="w-full h-11 pl-10 pr-4 rounded-xl border border-stone-200 bg-white text-[14px] focus:outline-none focus:ring-2 focus:ring-amber-600/20"
            />
          </div>

          <section className="mb-8">
            <h2 className="text-[12px] uppercase tracking-wider text-stone-400 mb-3 flex items-center gap-2">
              <Video className="w-4 h-4" />单视频问答 <span className="text-stone-300 font-normal">· 仅本视频转写</span>
            </h2>
            <div className="space-y-2">
              {videos.map(v => (
                <Link key={v.id} href={`/prototype/chat/${v.id}`} className="flex items-center gap-4 p-4 rounded-xl border border-stone-200 bg-white proto-card-hover">
                  <div className="w-10 h-10 rounded-lg bg-gradient-to-br from-amber-200 to-orange-300 flex items-center justify-center text-white text-[11px] font-bold shrink-0">1V</div>
                  <div className="flex-1 min-w-0">
                    <div className="font-medium text-stone-900 truncate">{v.title}</div>
                    <div className="text-[11px] text-stone-400">严格 RAG · 引用可追溯</div>
                  </div>
                  <ChevronRight className="w-4 h-4 text-stone-400 shrink-0" />
                </Link>
              ))}
            </div>
          </section>

          <section>
            <h2 className="text-[12px] uppercase tracking-wider text-stone-400 mb-3 flex items-center gap-2">
              <Library className="w-4 h-4" />知识库问答 <span className="text-stone-300 font-normal">· 跨视频检索</span>
            </h2>
            <div className="space-y-2">
              {kbs.map(k => (
                <Link key={k.id} href={`/prototype/kb/${k.id}`} className="flex items-center gap-4 p-4 rounded-xl border border-violet-200 bg-violet-50/30 proto-card-hover">
                  <div className="w-10 h-10 rounded-lg bg-gradient-to-br from-violet-300 to-indigo-400 flex items-center justify-center text-white text-[11px] font-bold shrink-0">KB</div>
                  <div className="flex-1 min-w-0">
                    <div className="font-medium text-stone-900 truncate">{k.name}</div>
                    <div className="text-[11px] text-stone-400">跨 {k.videos} 个视频 · 引用标注来源</div>
                  </div>
                  <ArrowRight className="w-4 h-4 text-violet-500 shrink-0" />
                </Link>
              ))}
            </div>
          </section>

          <div className="mt-8 p-4 rounded-xl border border-dashed border-stone-300 text-[12px] text-stone-500">
            进入聊天后，可接 <Link href="/prototype/agent-chat?variant=D" className="text-amber-800 underline">Agent 融合 UI</Link> 展示思考/检索/工具过程。
          </div>
        </div>
      </main>
    </div>
  )
}

// --- 保留 A、B 单独对比（不含 C）---

export function NavVariantA() {
  return (
    <div className="h-screen flex proto-root">
      <aside className="w-[260px] shrink-0 bg-[#faf8f5] border-r border-stone-200 flex flex-col p-4">
        <div className="text-[22px] font-semibold proto-serif px-2 mb-6">映知</div>
        <nav className="space-y-1 text-[13px]">
          <Link href="/prototype/dashboard" className="flex items-center gap-2 px-3 py-2 rounded-lg text-stone-600 hover:bg-stone-100">
            <Video className="w-4 h-4" />我的视频
          </Link>
          <div className="pt-3 pb-1 px-3 text-[10px] uppercase tracking-wider text-amber-700 font-medium">问答</div>
          <div className="ml-2 space-y-0.5 border-l-2 border-amber-200 pl-3">
            <div className="text-[10px] text-stone-400 px-2 py-1">单视频</div>
            {DEMO_VIDEOS.map(v => (
              <Link key={v.id} href={`/prototype/chat/${v.id}`} className="flex items-center gap-2 px-2 py-1.5 rounded-lg text-[12px] text-stone-700 hover:bg-amber-50">
                <MessageCircle className="w-3.5 h-3.5 text-amber-700 shrink-0" />
                <span className="truncate">{v.title}</span>
              </Link>
            ))}
            <div className="text-[10px] text-stone-400 px-2 py-1 mt-2">知识库</div>
            {DEMO_KBS.map(k => (
              <Link key={k.id} href={`/prototype/kb/${k.id}`} className="flex items-center gap-2 px-2 py-1.5 rounded-lg text-[12px] text-stone-700 hover:bg-violet-50">
                <Library className="w-3.5 h-3.5 text-violet-600 shrink-0" />
                <span className="truncate">{k.name}</span>
              </Link>
            ))}
          </div>
        </nav>
      </aside>
      <main className="flex-1 p-8 flex items-center justify-center">
        <div className="max-w-md text-center">
          <div className="text-[11px] text-amber-700 font-medium mb-2">方案 A · 仅侧栏</div>
          <p className="text-[14px] text-stone-500">问答入口集中在侧栏，主区仍是视频库等内容页。</p>
        </div>
      </main>
    </div>
  )
}

export function NavVariantB() {
  return (
    <div className="h-screen proto-root overflow-y-auto px-8 py-10 bg-[#f7f4ef]">
      <div className="max-w-3xl mx-auto">
        <div className="text-[11px] text-amber-700 font-medium mb-2">方案 B · 仅问答案例台</div>
        <h1 className="text-[32px] font-semibold proto-serif">开始问答</h1>
        <p className="text-[14px] text-stone-500 mt-2 mb-8">独立中枢页，单视频与知识库分块展示（无侧栏快捷入口）。</p>
        <div className="space-y-2">
          {DEMO_VIDEOS.map(v => (
            <Link key={v.id} href={`/prototype/chat/${v.id}`} className="block p-4 rounded-xl border bg-white proto-card-hover">{v.title}</Link>
          ))}
          {DEMO_KBS.map(k => (
            <Link key={k.id} href={`/prototype/kb/${k.id}`} className="block p-4 rounded-xl border border-violet-200 bg-violet-50/30 proto-card-hover">{k.name}</Link>
          ))}
        </div>
      </div>
    </div>
  )
}
