'use client'

import { useState, useEffect } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import { api, setToken, getToken, ApiError } from '@/lib/api'
import { Icon } from '@/components/ui/Icon'
import { ProjectorStage } from '@/components/login/ProjectorStage'

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
    <div className="login-wrap">
      <div className="login-stage-col">
        <ProjectorStage />
        <Link href="/" className="login-brand-float">
          <div className="brand-mark" style={{ width: 36, height: 36 }} />
          <div>
            <div className="brand-name" style={{ fontSize: 22 }}>映知</div>
            <div className="brand-sub">VIDLENS</div>
          </div>
        </Link>
      </div>

      <div className="login-main">
        <div className="login-card">
          <div className="seg" style={{ marginBottom: 18 }}>
            <button type="button" className={mode === 'login' ? 'on' : ''} onClick={() => { setMode('login'); setErr('') }}>登录</button>
            <button type="button" className={mode === 'register' ? 'on' : ''} onClick={() => { setMode('register'); setErr('') }}>注册</button>
          </div>

          <form onSubmit={submit} className="login-form">
            {mode === 'register' && (
              <Field label="昵称(可选)" value={nickname} onChange={setNickname} placeholder="显示名" />
            )}
            <Field label="用户名" value={username} onChange={setUsername} placeholder="2–50 字符" autoFocus={mode === 'login'} />
            <Field label="密码" type="password" value={password} onChange={setPassword} placeholder="至少 6 位" />
            {err && <div className="login-err">{err}</div>}
            <button type="submit" className="btn btn-primary" style={{ width: '100%', height: 42 }} disabled={busy}>
              {busy ? '请稍候…' : mode === 'login' ? '登录' : '注册并登录'}
            </button>
          </form>

          <div className="login-divider" />

          <button type="button" className="btn" style={{ width: '100%', height: 42 }} onClick={demoLogin} disabled={busy}>
            <Icon name="bulb" size="sm" />
            演示账号
          </button>
        </div>
      </div>
    </div>
  )
}

function Field({ label, type = 'text', value, onChange, placeholder, autoFocus }: {
  label: string; type?: string; value: string; onChange: (v: string) => void; placeholder?: string; autoFocus?: boolean
}) {
  return (
    <div style={{ marginBottom: 14 }}>
      <label className="field-label">{label}</label>
      <input className="input" type={type} value={value} onChange={e => onChange(e.target.value)} placeholder={placeholder} autoFocus={autoFocus} />
    </div>
  )
}
