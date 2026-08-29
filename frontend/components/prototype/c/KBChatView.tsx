'use client'

import { useState } from 'react'
import Link from 'next/link'
import {
  ArrowLeft, BookOpen, MessageCircle, Plus, Send, Square, Settings2,
} from 'lucide-react'
import type { KnowledgeBase } from '@/lib/types'

const DOT_COLORS = ['#b45309', '#047857', '#1d4ed8', '#7c3aed', '#be123c']

interface MockMsg {
  role: 'user' | 'assistant'
  content: string
  cites?: { id: string; content: string; videoTitle: string; color: string; score: number }[]
  openCites?: string[]
}

const DEMO_MSGS: MockMsg[] = [
  { role: 'user', content: '对比这几场发布会的定价策略有什么不同？' },
  {
    role: 'assistant',
    content: '根据知识库内多个视频的转写，定价策略呈现三个差异点 [C1]。创始人访谈中强调了订阅制转型 [C2]，而技术分享则提到按量计费 [C3]。',
    cites: [
      { id: 'C1', content: '…我们调整了定价策略，从一次性买断转向年度订阅…', videoTitle: '产品发布会全程实录', color: DOT_COLORS[0], score: 0.91 },
      { id: 'C2', content: '…订阅制是我们今年最重要的战略转向…', videoTitle: '创始人访谈：从 0 到 1', color: DOT_COLORS[1], score: 0.85 },
      { id: 'C3', content: '…对于开发者我们提供按 API 调用量计费…', videoTitle: '深度学习入门讲座第 3 讲', color: DOT_COLORS[2], score: 0.78 },
    ],
    openCites: ['C1'],
  },
]

interface Props {
  kb: KnowledgeBase | null
  kbId: number
}

export default function KBChatView({ kb, kbId }: Props) {
  const [messages, setMessages] = useState<MockMsg[]>(DEMO_MSGS)
  const [input, setInput] = useState('')
  const [streaming, setStreaming] = useState(false)

  const toggleCite = (msgIdx: number, id: string) => {
    setMessages(prev => {
      const next = [...prev]
      const m = next[msgIdx]
      if (!m) return prev
      const open = m.openCites || []
      next[msgIdx] = { ...m, openCites: open.includes(id) ? open.filter(x => x !== id) : [...open, id] }
      return next
    })
  }

  return (
    <div className="h-screen flex flex-col bg-[#f7f4ef] proto-root">
      <header className="shrink-0 bg-[#faf8f5] border-b border-stone-200 px-6 h-14 flex items-center gap-4">
        <Link href="/prototype/kb" className="flex items-center gap-2 text-stone-500 hover:text-stone-800 text-[12px] transition-colors">
          <ArrowLeft className="w-4 h-4" />返回知识库
        </Link>
        <div className="h-5 w-px bg-stone-200" />
        <div className="min-w-0 flex-1">
          <div className="text-[10px] text-stone-400">知识库 KB-{String(kbId).padStart(2, '0')} · 跨视频严格 RAG</div>
          <div className="text-[15px] font-medium text-stone-900 truncate proto-serif">{kb?.name || '加载中…'}</div>
        </div>
        <button className="h-8 px-3 rounded-lg border border-stone-200 text-[11px] flex items-center gap-1.5 proto-btn-lift">
          <Settings2 className="w-3.5 h-3.5" />管理成员
        </button>
      </header>

      <div className="flex-1 flex min-h-0">
        <aside className="w-60 shrink-0 border-r border-stone-200 bg-[#faf8f5] p-5 hidden lg:block overflow-y-auto">
          <div className="flex items-center justify-between mb-3">
            <span className="text-[10px] uppercase tracking-wider text-stone-400">会话</span>
            <button className="text-[10px] text-amber-800 flex items-center gap-0.5"><Plus className="w-3 h-3" />新建</button>
          </div>
          <button className="w-full text-left px-3 py-2 rounded-lg bg-amber-50 text-amber-900 text-[12px] font-medium mb-4">
            <MessageCircle className="w-3 h-3 inline mr-1.5" />当前会话
          </button>

          <div className="text-[10px] uppercase tracking-wider text-stone-400 mb-2">成员视频</div>
          <ul className="space-y-1.5 mb-5">
            {(kb?.videos || []).map((v, i) => (
              <li key={v.task_id} className="flex items-center gap-2 py-1.5 px-2 rounded-lg hover:bg-stone-100/80 transition-colors proto-row-hover">
                <span className="w-2 h-2 rounded-sm shrink-0" style={{ background: DOT_COLORS[i % DOT_COLORS.length] }} />
                <span className="text-[12px] text-stone-700 truncate flex-1">{v.title}</span>
                <span className={`text-[10px] ${v.retrievable ? 'text-emerald-600' : 'text-stone-400'}`}>{v.retrievable ? '✓' : '—'}</span>
              </li>
            ))}
          </ul>

          <div className="text-[10px] uppercase tracking-wider text-stone-400 mb-2">检索信息</div>
          <dl className="text-[11px] space-y-1">
            <div className="flex justify-between"><dt className="text-stone-400">命中</dt><dd>3 条</dd></div>
            <div className="flex justify-between"><dt className="text-stone-400">跨卷</dt><dd>3 卷</dd></div>
            <div className="flex justify-between"><dt className="text-stone-400">TopK</dt><dd>8</dd></div>
          </dl>
        </aside>

        <main className="flex-1 overflow-y-auto px-6 py-6">
          <div className="max-w-2xl mx-auto space-y-6">
            <div className="proto-fade-in">
              <h2 className="text-[24px] font-semibold text-stone-900 proto-serif">跨视频问答</h2>
              <p className="text-[13px] text-stone-500 mt-1 italic">本会话检索知识库全部视频，引用标注来源视频。</p>
            </div>

            {messages.map((m, i) => (
              <div key={i} className="proto-fade-in">
                {m.role === 'user' ? (
                  <div className="flex justify-end">
                    <div className="bg-stone-900 text-white text-[14px] px-4 py-2.5 rounded-2xl rounded-br-md max-w-[85%]">{m.content}</div>
                  </div>
                ) : (
                  <div className="space-y-2">
                    <div className="text-[10px] text-stone-400 flex items-center gap-2">
                      <BookOpen className="w-3 h-3" />映知 · 跨视频严格 RAG
                    </div>
                    <div className="text-[15px] leading-[1.8] text-stone-900">
                      {renderCites(m.content, m.cites || [], id => toggleCite(i, id), m.openCites || [])}
                    </div>
                    {m.cites && m.cites.filter(c => (m.openCites || []).includes(c.id)).map(c => (
                      <div key={c.id} className="p-3 rounded-lg bg-white border border-stone-200 text-[12px] proto-expand">
                        <div className="flex items-center gap-2 mb-1.5">
                          <span className="w-2 h-2 rounded-sm" style={{ background: c.color }} />
                          <span className="text-[10px] font-mono text-amber-700">[{c.id}]</span>
                          <span className="text-[10px] text-stone-500">{c.videoTitle}</span>
                          <span className="text-[10px] text-stone-400 ml-auto">{c.score.toFixed(2)}</span>
                        </div>
                        <p className="text-stone-600 leading-relaxed">{c.content}</p>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            ))}
          </div>
        </main>
      </div>

      <footer className="shrink-0 bg-[#faf8f5] border-t border-stone-200 px-6 py-4">
        <div className="max-w-2xl mx-auto flex gap-2">
          <input
            value={input}
            onChange={e => setInput(e.target.value)}
            placeholder="就知识库全部视频提问…"
            className="flex-1 h-11 px-4 rounded-xl border border-stone-200 bg-white text-[14px] focus:outline-none focus:ring-2 focus:ring-amber-600/20 focus:border-amber-400"
          />
          <button className="h-11 w-11 rounded-xl bg-stone-900 text-white flex items-center justify-center proto-btn-lift">
            {streaming ? <Square className="w-4 h-4" /> : <Send className="w-4 h-4" />}
          </button>
        </div>
        <div className="max-w-2xl mx-auto mt-2 text-[10px] text-stone-400 text-center">
          跨 {kb?.member_count ?? 0} 个视频检索 · 引用标注来源 · 原型演示
        </div>
      </footer>
    </div>
  )
}

function renderCites(text: string, cites: { id: string }[], onToggle: (id: string) => void, open: string[]) {
  return text.split(/(\[C\d+\])/g).map((part, i) => {
    const m = part.match(/^\[(C\d+)\]$/)
    if (m) {
      const id = m[1]
      return (
        <button key={i} onClick={() => onToggle(id)} className={`font-mono text-[10px] align-super px-1 py-0.5 mx-0.5 rounded border transition-all proto-btn-lift ${
          open.includes(id) ? 'bg-amber-600 text-white border-amber-600' : 'bg-amber-50 text-amber-800 border-amber-300'
        }`}>{id}</button>
      )
    }
    return <span key={i}>{part}</span>
  })
}
