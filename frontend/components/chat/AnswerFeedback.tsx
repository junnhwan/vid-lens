import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, getToken } from '@/lib/api'
import type { AnswerFeedbackInput } from '@/lib/types'

export function AnswerFeedback({ sessionId, messageId }: { sessionId: number; messageId: number }) {
  const identity = getToken()
  return <FeedbackControl key={`${identity}/${sessionId}/${messageId}`} sessionId={sessionId} messageId={messageId} identity={identity} />
}

function FeedbackControl({ sessionId, messageId, identity }: { sessionId: number; messageId: number; identity: string | null }) {
  const client = useQueryClient()
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState(false)
  const [category, setCategory] = useState<AnswerFeedbackInput['category']>('content')
  const [note, setNote] = useState('')
  const queryKey = ['answer-feedback', identity, sessionId, messageId]
  const read = useQuery({ queryKey, queryFn: ({ signal }) => api.getAnswerFeedback(sessionId, messageId, signal), enabled: open, staleTime: 30_000, retry: false })
  const saved = read.data
  const write = useMutation({
    mutationFn: async (input: AnswerFeedbackInput | null) => {
      await client.cancelQueries({ queryKey })
      if (input) return api.saveAnswerFeedback(sessionId, messageId, input)
      await api.clearAnswerFeedback(sessionId, messageId)
      return null
    },
    onSuccess: value => { client.setQueryData(queryKey, value); setEditing(false); setNote(value?.note || '') },
  })
  const busy = write.isPending || read.isFetching
  const loaded = read.isSuccess && !read.error
  const save = (rating: 'helpful' | 'problem') => write.mutate({ rating, category: rating === 'helpful' ? '' : category, note: rating === 'helpful' ? '' : note })
  const edit = () => {
    write.reset()
    if (!editing) { setCategory(saved?.category || 'content'); setNote(saved?.note || '') }
    setEditing(v => !v)
  }

  return <details style={{ marginTop: 12 }} aria-label="回答反馈" open={open} onToggle={event => setOpen(event.currentTarget.open)}>
    <summary>评价回答</summary>
    {open && <div style={{ marginTop: 10 }}>
      <div style={{ display: 'flex', gap: 14, alignItems: 'center' }}>
        <button className="meta-link" disabled={busy || !loaded} aria-pressed={saved?.rating === 'helpful'} onClick={() => save('helpful')}>{saved?.rating === 'helpful' ? '已标记有帮助' : '有帮助'}</button>
        <button className="meta-link" disabled={busy || !loaded} aria-expanded={editing} onClick={edit}>{saved?.rating === 'problem' ? '修改问题反馈' : '有问题'}</button>
        {saved && <button className="meta-link" disabled={busy || !loaded} onClick={() => write.mutate(null)}>清除反馈</button>}
      </div>
      {editing && <form onSubmit={e => { e.preventDefault(); save('problem') }} style={{ display: 'grid', gap: 8, marginTop: 10, maxWidth: 520 }}>
        <label>问题类型 <select aria-label="问题类型" value={category} disabled={busy} onChange={e => setCategory(e.target.value as AnswerFeedbackInput['category'])}>
          <option value="content">内容有误</option><option value="citation">引用有误</option><option value="incomplete">未完成问题</option><option value="slow">回答太慢</option>
        </select></label>
        <label>补充说明（可选）<textarea aria-label="反馈补充说明" value={note} maxLength={2000} rows={3} disabled={busy} onChange={e => setNote(e.target.value)} style={{ width: '100%' }} /></label>
        <div style={{ display: 'flex', gap: 8 }}><button className="btn btn-sm" type="submit" disabled={busy || !loaded}>{write.isPending ? '保存中…' : '保存反馈'}</button><button className="meta-link" type="button" disabled={busy} onClick={() => setEditing(false)}>取消</button></div>
      </form>}
      {saved && !editing && <p role="status" style={{ fontSize: 12, color: 'var(--tx-3)', marginTop: 8 }}>反馈已保存，可随时修改或清除。</p>}
      {read.error && <p role="alert" style={{ color: 'var(--bad)', fontSize: 12 }}>反馈读取失败。<button className="meta-link" disabled={busy} onClick={() => void read.refetch()}>重试读取</button></p>}
      {write.error && <p role="alert" style={{ color: 'var(--bad)', fontSize: 12 }}>{write.error instanceof Error ? write.error.message : '反馈保存失败'}。可重试当前操作。</p>}
    </div>}
  </details>
}
