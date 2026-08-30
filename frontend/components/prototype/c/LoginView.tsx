'use client'

import { useState } from 'react'
import Link from 'next/link'
import { Loader2, ArrowRight } from 'lucide-react'

export default function LoginView() {
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [busy, setBusy] = useState(false)

  return (
    <div className="min-h-[100dvh] flex proto-root">
      <div className="hidden lg:flex w-[42%] bg-ink-0 text-paper-0 flex-col justify-between p-12 relative overflow-hidden">
        <div className="absolute -bottom-24 -right-16 w-72 h-72 rounded-full bg-sienna-600/15 blur-3xl" />
        <div className="relative proto-fade-in">
          <div className="flex items-center gap-2.5">
            <span className="w-8 h-8 rounded-[8px] bg-paper-0 text-ink-0 text-[12px] font-semibold flex items-center justify-center">映</span>
            <span className="text-[28px] font-semibold tracking-tight">映知</span>
          </div>
          <p className="text-[15px] text-paper-0/45 mt-4 italic leading-relaxed max-w-sm">
            观之以映，释之以知
          </p>
        </div>
        <div className="relative space-y-7 proto-fade-in" style={{ animationDelay: '100ms' }}>
          <Feature title="视频转写" desc="长视频自动 ASR，分片处理，失败可重试" />
          <Feature title="引用式问答" desc="每个回答带 [C1] 引用片段，可回溯原文" />
          <Feature title="跨视频检索" desc="知识库内多视频联合 RAG，标注来源" />
        </div>
        <div className="relative text-[12px] text-paper-0/40">
          <Link href="/prototype/dashboard" className="hover:text-paper-0/70 transition-colors">返回原型工作台</Link>
        </div>
      </div>

      <div className="flex-1 flex items-center justify-center px-6 py-12 bg-paper-1">
        <div className="w-full max-w-sm proto-fade-in">
          <div className="lg:hidden mb-8">
            <div className="text-[28px] font-semibold text-ink-0 tracking-tight">映知</div>
            <p className="text-[13px] text-ink-3 mt-1">AI 长视频理解与可追溯问答</p>
          </div>

          <div className="flex gap-6 border-b border-ink-0/8 mb-6">
            {(['login', 'register'] as const).map(m => (
              <button
                key={m}
                onClick={() => setMode(m)}
                className={`pb-2.5 text-[13px] border-b-2 -mb-px transition-colors duration-200 ${
                  mode === m ? 'border-sienna-500 text-ink-0 font-medium' : 'border-transparent text-ink-4'
                }`}
              >
                {m === 'login' ? '登录' : '注册'}
              </button>
            ))}
          </div>

          <form className="space-y-4" onSubmit={e => { e.preventDefault(); setBusy(true); setTimeout(() => setBusy(false), 800) }}>
            {mode === 'register' && (
              <Input label="昵称（可选）" placeholder="显示名" />
            )}
            <Input label="用户名" placeholder="2-50 字符" />
            <Input label="密码" type="password" placeholder="至少 6 位" />

            <button
              type="submit"
              disabled={busy}
              className="w-full h-11 rounded-lg bg-ink-0 text-paper-0 text-[14px] font-medium flex items-center justify-center gap-2 proto-btn-lift disabled:opacity-50"
            >
              {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : null}
              {mode === 'login' ? '登录' : '注册并登录'}
              {!busy && <ArrowRight className="w-4 h-4" />}
            </button>
          </form>

          <div className="my-6 h-px bg-ink-0/8" />

          <Link
            href="/prototype/dashboard"
            className="w-full h-11 rounded-lg text-[14px] font-medium flex items-center justify-center text-ink-2 hover:text-ink-0 transition-colors"
          >
            进入原型演示
          </Link>
          <p className="text-[11px] text-ink-4 mt-3 text-center">
            演示账号 <span className="font-mono text-ink-3">test</span> / <span className="font-mono text-ink-3">test0236</span>
          </p>
        </div>
      </div>
    </div>
  )
}

function Feature({ title, desc }: { title: string; desc: string }) {
  return (
    <div>
      <div className="text-[14px] font-medium">{title}</div>
      <div className="text-[12px] text-paper-0/40 mt-1 leading-relaxed max-w-[32ch]">{desc}</div>
    </div>
  )
}

function Input({ label, type = 'text', placeholder }: { label: string; type?: string; placeholder?: string }) {
  return (
    <div>
      <label className="block text-[12px] text-ink-4 mb-1.5">{label}</label>
      <input
        type={type}
        placeholder={placeholder}
        className="w-full h-10 px-3 rounded-lg border border-ink-0/10 bg-paper-0 text-[13px] text-ink-1 placeholder:text-ink-5 focus:outline-none focus:border-sienna-500/50 transition-colors"
      />
    </div>
  )
}
