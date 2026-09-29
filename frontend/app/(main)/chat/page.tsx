import { useEffect, useState } from 'react'
import Link from '@/lib/router'
import { api } from '@/lib/api'
import type { ChatSession, KnowledgeBase, VideoTask } from '@/lib/types'
import { fmtRelTime, taskTitle } from '@/lib/format'
import { useCrumb } from '@/components/shell/AppShell'
import { PageHeading } from '@/components/product/PageHeading'
import { Icon } from '@/components/ui/Icon'
import { LoadingBlock } from '@/components/ui/AsyncState'

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
    <PageHeading eyebrow="ASK, WITH EVIDENCE" title="每个问题，都有出处。"
      description="先选择证据范围，再开始问答。每个范围的会话独立保存，回答都会带上可回看的来源。" />
    <div className="chat-entry-scopes">
      <Link href="/chat/library" className="scope-card">
        <span className="scope-head">
          <span className="scope-icon" aria-hidden="true"><Icon name="search" /></span>
          <span className="scope-copy">
            <b>视频库问答</b>
            <span>跨你的视频库中已建立索引的视频检索，不限于某个知识库。</span>
          </span>
        </span>
        <span className="scope-cta">进入视频库问答 <Icon name="arrow-r" size="sm" /></span>
      </Link>
      <div className="scope-card">
        <span className="scope-head">
          <span className="scope-icon" aria-hidden="true"><Icon name="folder" /></span>
          <span className="scope-copy">
            <b>知识库问答</b>
            <span>只检索所选知识库的成员视频，范围越聚焦，回答越准。</span>
          </span>
        </span>
        {kbs.length ? (
          <div className="kb-pick-list">
            {kbs.map(kb => (
              <Link key={kb.id} className="kb-pick-row" href={`/chat/kb/${kb.id}`}>
                <span className="kb-pick-icon" aria-hidden="true"><Icon name="folder" size="sm" /></span>
                <span className="kb-pick-copy">
                  <b>{kb.name}</b>
                  <small>{kb.member_count} 个成员视频 · {fmtRelTime(kb.updated_at)}更新{kb.description ? ` · ${kb.description}` : ''}</small>
                </span>
                <Icon name="chev-r" size="sm" className="kb-pick-go" />
              </Link>
            ))}
          </div>
        ) : (
          <p className="kb-pick-empty">还没有知识库。<Link href="/kb">先创建一个</Link>，把相关视频收进去再问。</p>
        )}
      </div>
    </div>
    <section className="chat-entry-videos"><h2>单视频问答</h2><p>只使用所选视频的内容与证据。</p><input className="input" value={query} onChange={e => setQuery(e.target.value)} placeholder="搜索视频标题或文件名" aria-label="搜索问答视频" />
      {loading ? <LoadingBlock /> : error ? <p role="alert">{error}</p> : tasks.length ? <div className="chat-entry-video-list">{tasks.map(task => <Link key={task.id} href={`/chat/v/${task.id}`}><span>{taskTitle(task)}</span><small>{task.has_transcription ? '转写已就绪' : '等待转写'} · 仅此视频</small></Link>)}</div> : <p>没有匹配的视频。</p>}
    </section>
    {!!sessions.length && <section className="chat-entry-history"><h2>最近会话</h2>{sessions.slice(0, 8).map(item => <Link key={item.id} href={item.scope_type === 'video_library' ? `/chat/library?session=${item.id}` : item.scope_type === 'knowledge_base' ? `/chat/kb/${item.knowledge_base_id}?session=${item.id}` : `/chat/v/${item.task_id}?session=${item.id}`}><b>{item.title || '未命名会话'}</b><small>{item.scope_type === 'video_library' ? '视频库' : item.scope_type === 'knowledge_base' ? '知识库' : '单视频'}</small></Link>)}</section>}
  </div>
}
