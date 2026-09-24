'use client'

import { useEffect, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import type { PromptPreferenceView } from '@/lib/types'
import { useShell } from '@/components/shell/AppShell'

export function PromptPreferencesSection({ readOnly }: { readOnly: boolean }) {
  const { registerLeaveGuard } = useShell()
  const [views, setViews] = useState<PromptPreferenceView[]>([])
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [error, setError] = useState('')
  const [busy, setBusy] = useState('')
  useEffect(() => { api.promptPreferences().then(rows => { setViews(rows); setDrafts(Object.fromEntries(rows.map(row => [row.function, row.user_instruction]))) }).catch(() => setError('提示词配置加载失败')) }, [])
  const dirty = views.some(row => (drafts[row.function] ?? '') !== row.user_instruction)
  useEffect(() => {
    if (!dirty) { registerLeaveGuard(null); return }
    registerLeaveGuard(() => window.confirm('提示词偏好有未保存修改。放弃并离开吗？'))
    return () => registerLeaveGuard(null)
  }, [dirty, registerLeaveGuard])

  const save = async (row: PromptPreferenceView) => {
    setBusy(row.function); setError('')
    try {
      await api.setPromptPreference(row.function, drafts[row.function] || '')
      const refreshed = await api.promptPreferences()
      setViews(refreshed)
      setDrafts(previous => ({ ...previous, [row.function]: refreshed.find(item => item.function === row.function)?.user_instruction || '' }))
    } catch (e) { setError(e instanceof ApiError ? e.message : '保存提示词失败') }
    finally { setBusy('') }
  }

  return <section>
    <h3 style={{ fontSize: 16, fontWeight: 600 }}>AI 提示词与偏好</h3>
    <p style={{ color: 'var(--tx-3)', fontSize: 13 }}>以下偏好按当前用户、按功能保存，跨配置档和视频生效。普通问答会按检索或视频概览路径选用对应指令。保存后只影响新的模型请求；已完成会话、摘要和画面证据不会被改写。产品证据、引用、输出结构与安全约束始终优先。运行时还会加入当前问题、视频证据和授权范围。</p>
    {error && <p role="alert">{error}</p>}
    {views.map(row => {
      const draft = drafts[row.function] ?? ''
      return <div key={row.function} className="card" style={{ padding: 18, marginTop: 14 }}>
        <h4>{row.label}</h4>
        <p style={{ fontSize: 13, color: 'var(--tx-3)' }}>{row.scope}</p>
        <details><summary>查看产品必需指令{row.editable ? '' : '（固定）'}</summary><pre style={{ whiteSpace: 'pre-wrap', fontSize: 12, marginTop: 8 }}>{row.product_instruction}</pre></details>
        {row.editable && <>
        <label className="field-label" htmlFor={`prompt-${row.function}`}>我的偏好</label>
        <textarea id={`prompt-${row.function}`} className="input" rows={4} maxLength={row.function === 'summary' ? 500 : 2000} style={{ width: '100%', resize: 'vertical' }} value={draft} disabled={readOnly || busy !== ''} placeholder="例如：用简洁中文回答，并优先给出时间点" onChange={e => setDrafts(previous => ({ ...previous, [row.function]: e.target.value }))} />
        <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
          <button className="btn btn-sm btn-primary" disabled={readOnly || busy !== '' || draft === row.user_instruction} onClick={() => void save(row)}>{busy === row.function ? '保存中…' : '保存偏好'}</button>
          <button className="btn btn-sm" disabled={readOnly || busy !== ''} onClick={() => setDrafts(previous => ({ ...previous, [row.function]: '' }))}>恢复默认</button>
        </div>
        <p style={{ fontSize: 12, color: 'var(--tx-3)' }}>恢复默认后点击“保存偏好”生效。</p>
        </>}
        <details style={{ marginTop: 12 }}><summary>预览生效配置</summary><pre style={{ whiteSpace: 'pre-wrap', fontSize: 12, marginTop: 8 }}>{row.editable ? preview(row, draft) : row.effective_preview}</pre></details>
      </div>
    })}
  </section>
}

function preview(row: PromptPreferenceView, draft: string): string {
  if (!draft.trim()) return row.product_instruction
  if (row.function === 'vision') return `${row.product_instruction}\n用户画面描述偏好（不得编造不可见内容）：\n${draft.trim()}\n如有冲突，遵守前面的事实与格式要求。`
  if (row.function === 'summary') return `${row.product_instruction}\n\n用户摘要偏好（不覆盖报告结构与事实约束）：\n${draft.trim()}\n若用户偏好与报告必需栏目或事实要求冲突，遵守产品指令。`
  return `${row.product_instruction}\n\n用户回答偏好（不能覆盖产品证据和引用约束）：\n${draft.trim()}\n若用户偏好与 VidLens 的证据范围、事实核查或引用格式冲突，遵守产品指令。`
}
