'use client'

import { useState, useEffect } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import { Loader2 } from 'lucide-react'
import { api, setToken, getToken, ApiError } from '@/lib/api'

// 登录/注册页。401 未授权统一跳此；已登录访问此页自动跳视频库。
export default function LoginPage() {
  const router = useRouter()
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [nickname, setNickname] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  // 已登录 → 跳视频库
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

  return (
    <div className="min-h-screen flex items-center justify-center px-4">
      <div className="w-full max-w-sm">
        {/* 标题 */}
        <div className="mb-8 text-center">
          <Link href="/" className="inline-flex items-center gap-2.5">
            <span className="w-8 h-8 rounded-md bg-ink-0 text-paper-0 flex items-center justify-center text-[15px] font-semibold">V</span>
            <span className="text-[20px] font-semibold tracking-tight text-ink-0">VidLens · 映知</span>
          </Link>
          <p className="font-sans italic text-[13px] text-ink-3 mt-3">AI 长视频理解与可追溯问答</p>
        </div>

        {/* tab */}
        <div className="flex border-b border-ink-0/15 mb-6 font-mono text-[11px]">
          <button onClick={() => { setMode('login'); setErr('') }} className={`tab py-2 mr-6 ${mode === 'login' ? 'on' : ''}`}>登录</button>
          <button onClick={() => { setMode('register'); setErr('') }} className={`tab py-2 mr-6 ${mode === 'register' ? 'on' : ''}`}>注册</button>
        </div>

        <form onSubmit={submit} className="space-y-4">
          {mode === 'register' && (
            <div>
              <label className="block font-mono text-[10px] text-ink-3 mb-1.5">昵称（可选）</label>
              <div className="field px-3 h-10 flex items-center">
                <input value={nickname} onChange={(e) => setNickname(e.target.value)} className="w-full font-sans text-[13px] text-ink-1" placeholder="显示名" />
              </div>
            </div>
          )}
          <div>
            <label className="block font-mono text-[10px] text-ink-3 mb-1.5">用户名</label>
            <div className="field px-3 h-10 flex items-center">
              <input value={username} onChange={(e) => setUsername(e.target.value)} autoFocus className="w-full font-sans text-[13px] text-ink-1" placeholder="2–50 字符" />
            </div>
          </div>
          <div>
            <label className="block font-mono text-[10px] text-ink-3 mb-1.5">密码</label>
            <div className="field px-3 h-10 flex items-center">
              <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} className="w-full font-sans text-[13px] text-ink-1" placeholder="至少 6 位" />
            </div>
          </div>

          {err && <div className="text-[12px] text-rust">{err}</div>}

          <button type="submit" disabled={busy} className="btn-ink w-full h-10 font-sans text-[13px] font-medium flex items-center justify-center gap-1.5 disabled:opacity-50">
            {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : null}
            {mode === 'login' ? '登录' : '注册并登录'}
          </button>
        </form>

        <p className="font-sans text-[11px] text-ink-4 mt-6 text-center leading-relaxed">
          {mode === 'login' ? '还没有账号？' : '已有账号？'}
          <button
            onClick={() => { setMode(mode === 'login' ? 'register' : 'login'); setErr('') }}
            className="ml-1 text-sienna-700 hover:underline"
          >
            {mode === 'login' ? '去注册' : '去登录'}
          </button>
        </p>
      </div>
    </div>
  )
}
