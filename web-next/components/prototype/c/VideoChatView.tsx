'use client'

import { useState } from 'react'
import Link from 'next/link'
import {
  ArrowLeft, BookOpen, Crosshair, MessageCircle, Plus, Copy, RefreshCw,
  Database, AlertTriangle, Send, Square,
} from 'lucide-react'
import type { VideoTask } from '@/lib/types'
import { taskTitle } from '@/components/prototype/c/mocks'

interface MockMsg {
  role: 'user' | 'assistant'
  content: string
  cites?: { id: string; content: string; score: number }[]
  openCites?: string[]
  streaming?: boolean
}

const DEMO_MSGS: MockMsg[] = [
  { role: 'user', content: '这场发布会提到了哪些新产品？' },
  {
    role: 'assistant',
    content: '根据转写内容，发布会重点介绍了三款产品：AI 助手、知识库平台和开发者工具链 [C1]。其中 AI 助手支持多模态理解 [C2]。',
    cites: [
      { id: 'C1', content: '…我们今天要介绍三款重磅产品，涵盖 AI 助手、知识库和开发者工具链…', score: 0.89 },
      { id: 'C2', content: '…AI 助手支持文本、图像和视频的多模态理解能力…', score: 0.82 },
    ],
    openCites: [],
  },
]

interface Props {
  task: VideoTask | null
  taskId: number
}

export default function VideoChatView({ task, taskId }: Props) {
  const [mode, setMode] = useState<'strict_rag' | 'video_assistant'>('strict_rag')
  const [messages, setMessages] = useState<MockMsg[]>(DEMO_MSGS)
  const [input, setInput] = useState('')
  const [streaming, setStreaming] = useState(false)
  const indexed = task?.has_transcription ?? true

  const send = () => {
    if (!input.trim() || streaming) return
    setMessages(m => [...m, { role: 'user', content: input }])
    setInput('')
    setStreaming(true)
    setTimeout(() => {
      setMessages(m => [...m, {
        role: 'assistant',
        content: '（原型演示）正在基于转写内容生成回答…',
        cites: [{ id: 'C1', content: '引用片段示例…', score: 0.75 }],
        openCites: [],
      }])
      setStreaming(false)
    }, 1200)
  }

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
      {/* 顶栏 */}
      <header className="shrink-0 bg-[#faf8f5] border-b border-stone-200 px-6 h-14 flex items-center gap-4">
        <Link href="/prototype/dashboard" className="flex items-center gap-2 text-stone-500 hover:text-stone-800 transition-colors text-[12px]">
          <ArrowLeft className="w-4 h-4" />返回视频库
        </Link>
        <div className="h-5 w-px bg-stone-200" />
        <div className="min-w-0 flex-1">
          <div className="text-[10px] text-stone-400">任务 #{taskId} · 单视频问答</div>
          <div className="text-[15px] font-medium text-stone-900 truncate proto-serif">{task ? taskTitle(task) : '加载中…'}</div>
        </div>
        <div className="flex rounded-lg border border-stone-200 overflow-hidden text-[11px]">
          <button onClick={() => setMode('video_assistant')} className={`px-3 py-1.5 transition-colors ${mode === 'video_assistant' ? 'bg-stone-900 text-white' : 'text-stone-500 hover:bg-stone-50'}`}>普通</button>
          <button onClick={() => setMode('strict_rag')} className={`px-3 py-1.5 flex items-center gap-1 transition-colors ${mode === 'strict_rag' ? 'bg-stone-900 text-white' : 'text-stone-500 hover:bg-stone-50'}`}>
            <Crosshair className="w-3 h-3" />严格 RAG
          </button>
        </div>
      </header>

      <div className="flex-1 flex min-h-0">
        {/* 左：元信息 */}
        <aside className="w-56 shrink-0 border-r border-stone-200 bg-[#faf8f5] p-5 hidden md:block">
          <div className="text-[10px] uppercase tracking-wider text-stone-400 mb-3">会话</div>
          <button className="w-full flex items-center gap-2 px-3 py-2 rounded-lg bg-amber-50 text-amber-900 text-[12px] font-medium mb-1">
            <MessageCircle className="w-3.5 h-3.5" />当前会话
          </button>
          <button className="w-full flex items-center gap-1 px-3 py-1.5 text-[11px] text-stone-400 hover:text-stone-600 mt-2">
            <Plus className="w-3 h-3" />新建会话
          </button>

          <div className="h-px bg-stone-200 my-5" />
          <div className="text-[10px] uppercase tracking-wider text-stone-400 mb-2">本视频</div>
          <dl className="text-[11px] space-y-1.5">
            <div className="flex justify-between"><dt className="text-stone-400">索引</dt><dd className={indexed ? 'text-emerald-700' : 'text-stone-400'}>{indexed ? '已建立' : '未索引'}</dd></div>
            <div className="flex justify-between"><dt className="text-stone-400">转写</dt><dd>{task?.transcription?.words ?? '—'} 词</dd></div>
            <div className="flex justify-between"><dt className="text-stone-400">模式</dt><dd>{mode === 'strict_rag' ? '严格 RAG' : '普通'}</dd></div>
          </dl>
        </aside>

        {/* 中：消息流 */}
        <main className="flex-1 overflow-y-auto px-6 py-6">
          <div className="max-w-2xl mx-auto space-y-6">
            <div className="text-center pb-2 proto-fade-in">
              <p className="text-[13px] text-stone-500 italic">基于本视频转写内容的问答。引用以 [C1] 标注，点击展开原文片段。</p>
            </div>

            {!indexed && mode === 'strict_rag' && (
              <div className="flex gap-3 p-4 rounded-xl bg-red-50 border border-red-200 proto-fade-in">
                <AlertTriangle className="w-5 h-5 text-red-600 shrink-0" />
                <div>
                  <div className="text-[14px] font-medium text-red-800">该视频尚未建立索引</div>
                  <p className="text-[12px] text-red-700/80 mt-1">strict_rag 模式强制走检索，无索引时无法回答。</p>
                  <button className="mt-2 h-8 px-3 rounded-lg border border-red-300 text-[11px] text-red-700 flex items-center gap-1 proto-btn-lift">
                    <Database className="w-3 h-3" />触发 RAG 索引
                  </button>
                </div>
              </div>
            )}

            {messages.map((m, i) => (
              <div key={i} className="proto-fade-in" style={{ animationDelay: `${i * 30}ms` }}>
                {m.role === 'user' ? (
                  <div className="flex justify-end">
                    <div className="bg-stone-900 text-white text-[14px] leading-relaxed px-4 py-2.5 rounded-2xl rounded-br-md max-w-[85%]">
                      {m.content}
                    </div>
                  </div>
                ) : (
                  <div className="space-y-2">
                    <div className="flex items-center gap-2 text-[10px] text-stone-400">
                      <BookOpen className="w-3 h-3" />映知 · {mode === 'strict_rag' ? '严格 RAG' : '问答'}
                      {m.streaming && <span className="text-amber-700 flex items-center gap-1"><span className="w-1.5 h-1.5 rounded-full bg-amber-500 proto-pulse" />生成中</span>}
                    </div>
                    <div className="text-[15px] leading-[1.8] text-stone-900">
                      {renderWithCites(m.content, m.cites || [], id => toggleCite(i, id), m.openCites || [])}
                    </div>
                    <div className="flex gap-3 text-[10px] text-stone-400">
                      <button className="hover:text-stone-700 flex items-center gap-1"><Copy className="w-3 h-3" />复制</button>
                      <button className="hover:text-stone-700 flex items-center gap-1"><RefreshCw className="w-3 h-3" />重试</button>
                    </div>
                    {m.cites && m.cites.length > 0 && (
                      <div className="space-y-2 mt-3">
                        {m.cites.filter(c => (m.openCites || []).includes(c.id)).map(c => (
                          <div key={c.id} className="p-3 rounded-lg bg-white border border-amber-200/60 text-[12px] text-stone-600 leading-relaxed proto-expand">
                            <div className="text-[10px] text-amber-700 font-mono mb-1">[{c.id}] score {c.score.toFixed(2)}</div>
                            {c.content}
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                )}
              </div>
            ))}
          </div>
        </main>
      </div>

      {/* 底部输入 */}
      <footer className="shrink-0 bg-[#faf8f5] border-t border-stone-200 px-6 py-4">
        <div className="max-w-2xl mx-auto flex gap-2">
          <input
            value={input}
            onChange={e => setInput(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send() } }}
            placeholder="就转写内容提问…"
            className="flex-1 h-11 px-4 rounded-xl border border-stone-200 bg-white text-[14px] focus:outline-none focus:ring-2 focus:ring-amber-600/20 focus:border-amber-400 transition-shadow"
          />
          <button
            onClick={streaming ? undefined : send}
            className="h-11 w-11 rounded-xl bg-stone-900 text-white flex items-center justify-center proto-btn-lift hover:bg-stone-800"
          >
            {streaming ? <Square className="w-4 h-4" /> : <Send className="w-4 h-4" />}
          </button>
        </div>
        <div className="max-w-2xl mx-auto mt-2 text-[10px] text-stone-400 text-center">基于本卷转写 · 引用可追溯 · 原型演示（不发真实请求）</div>
      </footer>
    </div>
  )
}

function renderWithCites(text: string, cites: { id: string }[], onToggle: (id: string) => void, open: string[]) {
  const parts = text.split(/(\[C\d+\])/g)
  return parts.map((part, i) => {
    const m = part.match(/^\[(C\d+)\]$/)
    if (m) {
      const id = m[1]
      const isOpen = open.includes(id)
      return (
        <button
          key={i}
          onClick={() => onToggle(id)}
          className={`font-mono text-[10px] font-medium align-super px-1 py-0.5 mx-0.5 rounded border transition-all duration-150 proto-btn-lift ${
            isOpen ? 'bg-amber-600 text-white border-amber-600' : 'bg-amber-50 text-amber-800 border-amber-300 hover:bg-amber-100'
          }`}
        >
          {id}
        </button>
      )
    }
    return <span key={i}>{part}</span>
  })
}
