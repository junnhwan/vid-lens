'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import { useRouter } from 'next/navigation'
import { api, ApiError } from '@/lib/api'
import type { ChatSession, KnowledgeBase, VideoTask } from '@/lib/types'
import { fmtRelTime, taskTitle } from '@/lib/format'
import { taskCategory } from '@/lib/taskStatus'
import { VideoCard } from '@/components/VideoCard'
import { useShell } from '@/components/shell/AppShell'
import { useToast } from '@/components/Toast'
import { Icon } from '@/components/ui/Icon'

// 工作台:提问式首页(范围=知识库)+ 处理中任务 + 最近视频/知识库/会话。
// 对应原型 #/dashboard;上传模态由外壳提供。

export default function DashboardPage() {
  const router = useRouter()
  const toast = useToast()
  const { user, openUpload } = useShell()

  const [tasks, setTasks] = useState<VideoTask[]>([])
  const [kbs, setKbs] = useState<KnowledgeBase[]>([])
  const [sessions, setSessions] = useState<ChatSession[]>([])
  const [loading, setLoading] = useState(true)
  const [kbId, setKbId] = useState<number | null>(null)
  const [scopeOpen, setScopeOpen] = useState(false)
  const [question, setQuestion] = useState('')
  const [sending, setSending] = useState(false)
  const askRef = useRef<HTMLTextAreaElement | null>(null)

  useEffect(() => {
    let active = true
    void (async () => {
      const [taskPage, kbList, sessionList] = await Promise.all([
        api.listTasks(1, 50).catch(() => null),
        api.listKBs().catch(() => []),
        api.listSessions().catch(() => []),
      ])
      if (!active) return
      setTasks(taskPage?.list || [])
      setKbs(kbList)
      setSessions(sessionList)
      if (kbList.length > 0) setKbId(kbList[0].id)
      setLoading(false)
    })()
    return () => { active = false }
  }, [])

  const selectedKb = kbs.find(k => k.id === kbId) || null
  const processing = tasks.filter(t => taskCategory(t) !== 'ready')
  const taskTitleById = useCallback((id: number) => {
    const t = tasks.find(x => x.id === id)
    return t ? taskTitle(t) : null
  }, [tasks])
  const kbNameById = useCallback((id: number) => kbs.find(k => k.id === id)?.name || null, [kbs])

  const autoGrow = (el: HTMLTextAreaElement | null) => {
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(120, el.scrollHeight)}px`
  }

  const send = async () => {
    const q = question.trim()
    if (!q) { toast.info('先输入一个问题'); return }
    if (!selectedKb) { toast.info('还没有知识库,先在「知识库」页创建一个'); return }
    setSending(true)
    try {
      const session = await api.createSession({ knowledge_base_id: selectedKb.id, scope_type: 'knowledge_base' })
      router.push(`/chat/kb/${selectedKb.id}?session=${session.id}&q=${encodeURIComponent(q)}`)
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '创建会话失败')
      setSending(false)
    }
  }

  const retry = async (t: VideoTask) => {
    try {
      if (t.last_job_type === 'analyze') await api.analyze(t.id)
      else await api.transcribe(t.id)
      toast.success('已重新入队,已完成的分片不会重复调用')
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '重试失败')
    }
  }

  const greeting = (() => {
    const h = new Date().getHours()
    if (h >= 5 && h < 11) return '早上好'
    if (h >= 11 && h < 13) return '中午好'
    if (h >= 13 && h < 18) return '下午好'
    if (h >= 18 && h < 23) return '晚上好'
    return '夜深了'
  })()

  return (
    <div className="page">
      <section className="hero-ask">
        <h1>{greeting},{user?.nickname || user?.username || '…'}。今天想让<em>哪些视频</em>替你说话?</h1>
        <div className="sub">所有回答都带时间点引用,可以一路回溯到画面。</div>
        <div className="ask-bar">
          <textarea
            ref={el => { askRef.current = el; autoGrow(el) }}
            rows={1}
            value={question}
            onChange={e => { setQuestion(e.target.value); autoGrow(e.target) }}
            onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); void send() } }}
            placeholder="向整个知识库提问,或先选一个范围…"
          />
          <button className="ask-send" onClick={() => void send()} disabled={sending} aria-label="发送">
            <Icon name="send" />
          </button>
        </div>
        <div className="ask-scope">
          <span style={{ fontSize: 12, color: 'var(--tx-4)' }}>范围</span>
          <button
            className="chip chip-acc"
            onClick={() => kbs.length > 0 && setScopeOpen(v => !v)}
          >
            <Icon name="folder" />
            {selectedKb ? selectedKb.name : '选择知识库'}
          </button>
          <button className="chip" onClick={() => router.push('/kb')}>更换范围</button>
          {scopeOpen && (
            <>
              <div style={{ position: 'fixed', inset: 0, zIndex: 30 }} onClick={() => setScopeOpen(false)} />
              <div className="scope-pop">
                {kbs.map(k => (
                  <button
                    key={k.id}
                    className={k.id === kbId ? 'on' : ''}
                    onClick={() => { setKbId(k.id); setScopeOpen(false) }}
                  >
                    <Icon name="folder" size="sm" />
                    {k.name}
                    <span className="mono" style={{ marginLeft: 'auto', fontSize: 10, color: 'var(--tx-4)' }}>{k.member_count}</span>
                  </button>
                ))}
              </div>
            </>
          )}
        </div>
        <div className="suggest-row">
          <button className="suggest" onClick={() => { setQuestion('这些视频里讨论了哪些主要观点?'); askRef.current?.focus() }}>
            这些视频里讨论了哪些主要观点?
          </button>
          <button className="suggest" onClick={openUpload}>上传一个新视频</button>
        </div>
      </section>

      <div className="section-head">
        <h2>继续处理</h2>
        <span className="more" onClick={() => router.push('/library')}>全部视频 <Icon name="chev-r" size="sm" /></span>
      </div>
      <div style={{ display: 'grid', gap: 10 }}>
        {loading && <div className="card card-pad" style={{ color: 'var(--tx-3)', fontSize: 12.5 }}>正在加载任务…</div>}
        {!loading && processing.map(t => {
          const failed = t.status === 4 || t.status === 5
          const noRetry = failed && t.last_job_type === 'download'
          return (
            <div key={t.id} className="proc-row" style={{ cursor: 'pointer' }} onClick={() => router.push(`/video/${t.id}`)}>
              <div className="proc-left">
                <h5>{taskTitle(t)}</h5>
                <div className="stage">
                  {failed
                    ? <span style={{ color: 'var(--bad)' }}>{t.error_msg || '处理失败'}</span>
                    : `${t.stage === 'none' ? '' : ''}${stageOrIdle(t)}`}
                </div>
              </div>
              {failed
                ? noRetry
                  ? <span className="chip chip-mute">URL 任务,请删除后重新添加</span>
                  : <button className="btn btn-sm" onClick={e => { e.stopPropagation(); void retry(t) }}>重试</button>
                : <span className={`chip ${t.status === 2 ? 'chip-acc' : 'chip-mute'}`}>{t.status === 2 ? '处理中' : '排队中'}</span>}
            </div>
          )
        })}
        {!loading && processing.length === 0 && (
          <div className="empty"><b>没有进行中的任务</b><p>转写、摘要和索引都在后台排队,这里会显示它们的进度。</p></div>
        )}
      </div>

      <div className="section-head">
        <h2>最近视频</h2>
        <span className="more" onClick={openUpload}><Icon name="plus" size="sm" />上传</span>
      </div>
      {tasks.length > 0 ? (
        <div className="video-grid">{tasks.slice(0, 4).map(t => <VideoCard key={t.id} task={t} />)}</div>
      ) : (
        !loading && (
          <div className="card">
            <div className="empty">
              <Icon name="video" size="lg" />
              <b>还没有视频</b>
              <p>上传第一个视频,转写完成后就可以向它提问。</p>
              <button className="btn btn-sm btn-primary" onClick={openUpload}><Icon name="upload" size="sm" />上传视频</button>
            </div>
          </div>
        )
      )}

      <div className="section-head">
        <h2>知识库</h2>
        <span className="more" onClick={() => router.push('/kb')}>管理 <Icon name="chev-r" size="sm" /></span>
      </div>
      {kbs.length > 0 ? (
        <div className="kb-grid">
          {kbs.map(k => (
            <div key={k.id} className="kb-card" onClick={() => router.push(`/chat/kb/${k.id}`)}>
              <div className="kb-top">
                <div className="kb-icon" style={{ background: 'var(--acc-dim)', color: 'var(--acc-strong)', borderColor: 'var(--acc-line)' }}><Icon name="folder" /></div>
                <div>
                  <h4>{k.name}</h4>
                  <div className="kb-sub">{k.member_count} 个视频 · {fmtRelTime(k.updated_at)}更新</div>
                </div>
              </div>
              {k.description && <div className="kb-desc">{k.description}</div>}
              <div className="kb-foot">
                <span style={{ marginLeft: 'auto', color: 'var(--tx-3)', fontSize: 12, display: 'inline-flex', alignItems: 'center', gap: 5 }}>
                  进入问答 <Icon name="chev-r" size="sm" />
                </span>
              </div>
            </div>
          ))}
        </div>
      ) : (
        !loading && (
          <div className="card">
            <div className="empty">
              <Icon name="folder" size="lg" />
              <b>还没有知识库</b>
              <p>知识库把多个视频放进同一个检索范围,回答会注明每个片段来自哪场视频。</p>
              <button className="btn btn-sm" onClick={() => router.push('/kb')}>去创建</button>
            </div>
          </div>
        )
      )}

      <div className="section-head"><h2>最近会话</h2></div>
      <div className="card" style={{ padding: 8 }}>
        {sessions.length > 0 ? sessions.slice(0, 8).map(s => {
          const isKb = s.knowledge_base_id > 0
          const where = isKb
            ? kbNameById(s.knowledge_base_id) || '知识库会话'
            : taskTitleById(s.task_id) || '单视频会话'
          const href = isKb ? `/chat/kb/${s.knowledge_base_id}?session=${s.id}` : `/chat/v/${s.task_id}?session=${s.id}`
          return (
            <button key={s.id} className="session-row" onClick={() => router.push(href)}>
              <Icon name="message" />
              <span className="q">{s.title || '未命名会话'}</span>
              <span className="where">{where}</span>
              <span className="where">{fmtRelTime(s.updated_at)}</span>
            </button>
          )
        }) : (
          !loading && <div className="empty"><b>还没有会话</b><p>发起一次提问后,最近的对话会出现在这里。</p></div>
        )}
      </div>
    </div>
  )
}

function stageOrIdle(t: VideoTask): string {
  const labels: Record<string, string> = {
    downloading: '下载中', uploaded: '已上传', transcribing: '转写中',
    visual_indexing: '画面索引中', summarizing: '生成摘要中', indexing: '构建索引中', none: '处理中',
  }
  return labels[t.stage] || '处理中'
}
