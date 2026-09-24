'use client'

import { useEffect, useState } from 'react'
import Link from 'next/link'
import { api } from '@/lib/api'
import type { ChatSession, KnowledgeBase, VideoTask } from '@/lib/types'
import { taskTitle } from '@/lib/format'
import { useCrumb } from '@/components/shell/AppShell'

export default function ChatEntryPage() {
  useCrumb([{ label: '问答' }])
  const [query, setQuery] = useState('')
  const [tasks, setTasks] = useState<VideoTask[]>([])
  const [kbs, setKbs] = useState<KnowledgeBase[]>([])
  const [sessions, setSessions] = useState<ChatSession[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  useEffect(() => {
    let active = true
    Promise.all([api.listKBs(), api.listSessions()]).then(([libraries, history]) => {
      if (active) { setKbs(libraries); setSessions(history) }
    }).catch(() => { if (active) setError('知识库或会话列表加载失败，请刷新重试。') })
    return () => { active = false }
  }, [])
  useEffect(() => {
    let active = true
    setLoading(true); setError('')
    const timer = window.setTimeout(() => {
      void api.listTasks(1, 30, query).then(page => { if (active) setTasks(page.list) }).catch(() => { if (active) setError('视频列表加载失败，请刷新重试。') }).finally(() => { if (active) setLoading(false) })
    }, query ? 250 : 0)
    return () => { active = false; window.clearTimeout(timer) }
  }, [query])
  return <div className="page page-wide chat-entry">
    <h1>问答</h1><p>先选择证据范围。每个范围的会话独立保存。</p>
    <div className="chat-entry-scopes">
      <Link href="/chat/library" className="card card-pad"><b>视频库问答</b><span>跨你的视频库中已建立索引的视频检索，不限于某个知识库。</span></Link>
      <div className="card card-pad"><b>知识库问答</b><span>只检索所选知识库的成员视频。</span><div>{kbs.map(kb => <Link key={kb.id} href={`/chat/kb/${kb.id}`}>{kb.name} · {kb.member_count} 个视频 →</Link>)}{!kbs.length && <small>还没有知识库。</small>}</div></div>
    </div>
    <section className="chat-entry-videos"><h2>单视频问答</h2><p>只使用所选视频的内容与证据。</p><input className="input" value={query} onChange={e => setQuery(e.target.value)} placeholder="搜索视频标题或文件名" aria-label="搜索问答视频" />
      {loading ? <p>加载中…</p> : error ? <p role="alert">{error}</p> : tasks.length ? <div className="chat-entry-video-list">{tasks.map(task => <Link key={task.id} href={`/chat/v/${task.id}`}><span>{taskTitle(task)}</span><small>{task.has_transcription ? '转写已就绪' : '等待转写'} · 仅此视频</small></Link>)}</div> : <p>没有匹配的视频。</p>}
    </section>
    {!!sessions.length && <section className="chat-entry-history"><h2>最近会话</h2>{sessions.slice(0, 8).map(item => <Link key={item.id} href={item.scope_type === 'video_library' ? `/chat/library?session=${item.id}` : item.scope_type === 'knowledge_base' ? `/chat/kb/${item.knowledge_base_id}?session=${item.id}` : `/chat/v/${item.task_id}?session=${item.id}`}><b>{item.title || '未命名会话'}</b><small>{item.scope_type === 'video_library' ? '视频库' : item.scope_type === 'knowledge_base' ? '知识库' : '单视频'}</small></Link>)}</section>}
  </div>
}
