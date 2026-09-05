'use client'

import { useState, useEffect } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import { api, setToken, getToken, ApiError } from '@/lib/api'
import { Icon } from '@/components/ui/Icon'

// 登录/注册:视觉对齐深色放映厅设计系统,认证逻辑与后端契约不变。

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
      <div className="login-side">
        <div className="login-brand">
          <div className="brand-mark" style={{ width: 38, height: 38 }} />
          <div>
            <div className="brand-name" style={{ fontSize: 24 }}>映知</div>
            <div className="brand-sub">VIDLENS</div>
          </div>
        </div>
        <p className="login-claim">观之以映,释之以知。</p>
        <div className="login-features">
          <Feature icon="activity" title="视频转写" desc="长视频自动 ASR,分片处理,失败可重试" />
          <Feature icon="target" title="引用式问答" desc="每个回答带时间点引用,可回放核对到画面" />
          <Feature icon="shield-check" title="Agent 检证" desc="回答保存后经独立证据核验,不确定就明说" />
        </div>
        <div className="login-foot">
          <Link href="/">← 返回工作台</Link>
        </div>
      </div>

      <div className="login-main">
        <div className="login-card">
          <div className="seg" style={{ marginBottom: 18 }}>
            <button className={mode === 'login' ? 'on' : ''} onClick={() => { setMode('login'); setErr('') }}>登录</button>
            <button className={mode === 'register' ? 'on' : ''} onClick={() => { setMode('register'); setErr('') }}>注册</button>
          </div>

          <form onSubmit={submit} className="login-form">
            {mode === 'register' && (
              <Field label="昵称(可选)" value={nickname} onChange={setNickname} placeholder="显示名" />
            )}
            <Field label="用户名" value={username} onChange={setUsername} placeholder="2–50 字符" autoFocus={mode === 'login'} />
            <Field label="密码" type="password" value={password} onChange={setPassword} placeholder="至少 6 位" />

            {err && <div className="login-err">{err}</div>}

            <button type="submit" className="btn btn-primary" style={{ width: '100%', height: 40 }} disabled={busy}>
              {busy ? '请稍候…' : mode === 'login' ? '登录' : '注册并登录'}
            </button>
          </form>

          <div className="login-divider" />

          <button type="button" className="btn" style={{ width: '100%', height: 40 }} onClick={demoLogin} disabled={busy}>
            <Icon name="bulb" size="sm" />
            一键体验演示账号
          </button>
          <p className="login-demo-hint">
            演示账号 <span className="mono">{DEMO_USERNAME}</span> / <span className="mono">{DEMO_PASSWORD}</span>
            · 只读,可浏览转写与摘要并问答
          </p>
        </div>
      </div>
    </div>
  )
}

function Feature({ icon, title, desc }: { icon: 'activity' | 'target' | 'shield-check'; title: string; desc: string }) {
  return (
    <div className="login-feature">
      <span className="agent-mark"><Icon name={icon} /></span>
      <div>
        <b>{title}</b>
        <p>{desc}</p>
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
