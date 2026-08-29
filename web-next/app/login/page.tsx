'use client'

import { useState, useEffect } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import { Loader2, Sparkles, ArrowRight } from 'lucide-react'
import { api, setToken, getToken, ApiError } from '@/lib/api'

const DEMO_USERNAME = 'test'
const DEMO_PASSWORD = 'test0236'

export default function LoginPage() {
  const router = useRouter()
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [nickname, setNickname] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  useEffect(() => {
    if (getToken()) router.replace('/')
  }, [router])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!username.trim() || !password.trim()) { setErr('请输入用户名和密码'); return }
    if (password.length < 6) { setErr('密码至少 6 位'); return }
    setBusy(true); setErr('')
    try {
      const r = mode === 'login'
        ? await api.login(username.trim(), password)
        : await api.register(username.trim(), password, nickname.trim() || undefined)
      setToken(r.token)
      router.replace('/')
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '请求失败')
    } finally { setBusy(false) }
  }

  const demoLogin = async () => {
    setBusy(true); setErr('')
    try {
      const r = await api.login(DEMO_USERNAME, DEMO_PASSWORD)
      setToken(r.token)
      router.replace('/')
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '请求失败')
    } finally { setBusy(false) }
  }

  return (
    <div className="min-h-screen flex ui-root">
      <div className="hidden lg:flex w-[45%] bg-stone-900 text-[#faf8f5] flex-col justify-between p-12 relative overflow-hidden">
        <div className="absolute inset-0 bg-gradient-to-br from-amber-900/30 via-stone-900 to-stone-950" />
        <div className="absolute -bottom-20 -right-20 w-80 h-80 rounded-full bg-amber-600/10 blur-3xl ui-pulse" />
        <div className="relative ui-fade-in">
          <div className="text-[36px] font-semibold ui-serif">映知</div>
          <p className="text-[15px] text-stone-400 mt-3 italic leading-relaxed max-w-sm">
            观之以映，释之以知
          </p>
        </div>
        <div className="relative space-y-6 ui-fade-in" style={{ animationDelay: '100ms' }}>
          <Feature num="01" title="视频转写" desc="长视频自动 ASR，分片处理，失败可重试" />
          <Feature num="02" title="引用式问答" desc="每个回答带 [C1] 引用片段，可回溯原文" />
          <Feature num="03" title="跨视频检索" desc="知识库内多视频联合 RAG，标注来源" />
        </div>
        <div className="relative text-[11px] text-stone-600">
          <Link href="/" className="hover:text-stone-400 transition-colors">← 返回视频库</Link>
        </div>
      </div>

      <div className="flex-1 flex items-center justify-center px-6 py-12 bg-[#f7f4ef]">
        <div className="w-full max-w-sm ui-fade-in">
          <div className="lg:hidden mb-8 text-center">
            <div className="text-[28px] font-semibold text-stone-900 ui-serif">映知</div>
            <p className="text-[13px] text-stone-500 mt-1 italic">AI 长视频理解与可追溯问答</p>
          </div>

          <div className="flex gap-6 border-b border-stone-200 mb-6">
            {(['login', 'register'] as const).map(m => (
              <button
                key={m}
                onClick={() => { setMode(m); setErr('') }}
                className={`pb-2.5 text-[13px] border-b-2 -mb-px transition-colors duration-200 ${
                  mode === m ? 'border-amber-600 text-stone-900 font-medium' : 'border-transparent text-stone-400'
                }`}
              >
                {m === 'login' ? '登录' : '注册'}
              </button>
            ))}
          </div>

          <form onSubmit={submit} className="space-y-4">
            {mode === 'register' && (
              <Field label="昵称（可选）" value={nickname} onChange={setNickname} placeholder="显示名" />
            )}
            <Field label="用户名" value={username} onChange={setUsername} placeholder="2–50 字符" autoFocus={mode === 'login'} />
            <Field label="密码" type="password" value={password} onChange={setPassword} placeholder="至少 6 位" />

            {err && <div className="text-[12px] text-red-600">{err}</div>}

            <button
              type="submit"
              disabled={busy}
              className="w-full h-11 rounded-lg bg-stone-900 text-white text-[14px] font-medium flex items-center justify-center gap-2 ui-btn-lift disabled:opacity-50"
            >
              {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : null}
              {mode === 'login' ? '登录' : '注册并登录'}
              {!busy && <ArrowRight className="w-4 h-4" />}
            </button>
          </form>

          <div className="my-6 flex items-center gap-3">
            <div className="h-px flex-1 bg-stone-200" />
            <span className="text-[11px] text-stone-400">或</span>
            <div className="h-px flex-1 bg-stone-200" />
          </div>

          <button
            type="button"
            onClick={demoLogin}
            disabled={busy}
            className="w-full h-11 rounded-lg border border-stone-300 bg-white text-[14px] font-medium flex items-center justify-center gap-2 ui-btn-lift hover:border-amber-400 hover:text-amber-900 transition-colors disabled:opacity-50"
          >
            {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : <Sparkles className="w-4 h-4" />}
            一键体验演示账号
          </button>
          <p className="text-[11px] text-stone-400 mt-3 text-center">
            演示账号 <span className="font-mono text-stone-500">test</span> / <span className="font-mono text-stone-500">test0236</span>
            · 只读，可浏览视频转写与摘要并问答
          </p>
        </div>
      </div>
    </div>
  )
}

function Feature({ num, title, desc }: { num: string; title: string; desc: string }) {
  return (
    <div className="flex gap-4">
      <span className="text-[11px] font-mono text-amber-600/70 mt-0.5">{num}</span>
      <div>
        <div className="text-[14px] font-medium">{title}</div>
        <div className="text-[12px] text-stone-500 mt-0.5">{desc}</div>
      </div>
    </div>
  )
}

function Field({ label, type = 'text', value, onChange, placeholder, autoFocus }: {
  label: string; type?: string; value: string; onChange: (v: string) => void; placeholder?: string; autoFocus?: boolean
}) {
  return (
    <div>
      <label className="block text-[10px] uppercase tracking-wider text-stone-400 mb-1.5">{label}</label>
      <input
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        autoFocus={autoFocus}
        className="w-full h-10 px-3 rounded-lg border border-stone-200 bg-white text-[13px] focus:outline-none focus:ring-2 focus:ring-amber-600/20 focus:border-amber-400 transition-shadow"
      />
    </div>
  )
}
