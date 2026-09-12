'use client'

import { useEffect, useState } from 'react'
import { api } from '@/lib/api'
import type { AnswerFeedback as Feedback, AnswerFeedbackInput } from '@/lib/types'

export function AnswerFeedback({ sessionId, messageId }: { sessionId: number; messageId: number }) {
  const [saved, setSaved] = useState<Feedback | null>(null)
  const [editing, setEditing] = useState(false)
  const [category, setCategory] = useState<AnswerFeedbackInput['category']>('content')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(true)
  const [error, setError] = useState('')
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    let active = true
    api.getAnswerFeedback(sessionId, messageId).then(value => {
      if (!active) return
      setSaved(value)
      setCategory(value?.category || 'content')
      setNote(value?.note || '')
      setLoaded(true)
    }).catch(() => { if (active) setError('反馈读取失败，请刷新后重试') })
      .finally(() => { if (active) setBusy(false) })
    return () => { active = false }
  }, [sessionId, messageId])

  async function save(rating: 'helpful' | 'problem') {
    setBusy(true)
    setError('')
    try {
      setSaved(await api.saveAnswerFeedback(sessionId, messageId, { rating, category: rating === 'helpful' ? '' : category, note: rating === 'helpful' ? '' : note }))
      setEditing(false)
    } catch (e) { setError(e instanceof Error ? e.message : '反馈保存失败') }
    finally { setBusy(false) }
  }

  async function clear() {
    setBusy(true)
    setError('')
    try {
      await api.clearAnswerFeedback(sessionId, messageId)
      setSaved(null)
      setNote('')
      setEditing(false)
    } catch (e) { setError(e instanceof Error ? e.message : '反馈清除失败') }
    finally { setBusy(false) }
  }

  return <div style={{ marginTop: 12 }} aria-label="回答反馈">
    <div style={{ display: 'flex', gap: 14, alignItems: 'center' }}>
      <button className="meta-link" disabled={busy || !loaded} aria-pressed={saved?.rating === 'helpful'} onClick={() => void save('helpful')}>{saved?.rating === 'helpful' ? '已标记有帮助' : '有帮助'}</button>
      <button className="meta-link" disabled={busy || !loaded} aria-expanded={editing} onClick={() => setEditing(v => !v)}>{saved?.rating === 'problem' ? '修改问题反馈' : '有问题'}</button>
      {saved && <button className="meta-link" disabled={busy} onClick={() => void clear()}>清除反馈</button>}
    </div>
    {editing && <form onSubmit={e => { e.preventDefault(); void save('problem') }} style={{ display: 'grid', gap: 8, marginTop: 10, maxWidth: 520 }}>
      <label>问题类型 <select aria-label="问题类型" value={category} disabled={busy} onChange={e => setCategory(e.target.value as AnswerFeedbackInput['category'])}>
        <option value="content">内容有误</option><option value="citation">引用有误</option><option value="incomplete">未完成问题</option><option value="slow">回答太慢</option>
      </select></label>
      <label>补充说明（可选）<textarea aria-label="反馈补充说明" value={note} maxLength={2000} rows={3} disabled={busy} onChange={e => setNote(e.target.value)} style={{ width: '100%' }} /></label>
      <div style={{ display: 'flex', gap: 8 }}><button className="btn btn-sm" type="submit" disabled={busy}>{busy ? '保存中…' : '保存反馈'}</button><button className="meta-link" type="button" disabled={busy} onClick={() => setEditing(false)}>取消</button></div>
    </form>}
    {saved && !editing && <p role="status" style={{ fontSize: 12, color: 'var(--tx-3)', marginTop: 8 }}>反馈已保存，可随时修改或清除。</p>}
    {error && <p role="alert" style={{ color: 'var(--bad)', fontSize: 12 }}>{error}</p>}
  </div>
}
