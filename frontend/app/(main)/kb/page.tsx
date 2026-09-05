'use client'

import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'
import { api, ApiError } from '@/lib/api'
import type { KnowledgeBase } from '@/lib/types'
import { fmtRelTime } from '@/lib/format'
import { useCrumb } from '@/components/shell/AppShell'
import { useToast } from '@/components/Toast'
import { Icon } from '@/components/ui/Icon'

// 知识库:跨视频问答入口(卡片进入问答)+ 新建 + 成员速览。
// 对应原型 #/kb;新建知识库是后端已有能力(原型里为演示关闭)。

export default function KBListPage() {
  const router = useRouter()
  const toast = useToast()
  useCrumb(['知识库'])

  const [kbs, setKbs] = useState<KnowledgeBase[]>([])
  const [loading, setLoading] = useState(true)
  const [createOpen, setCreateOpen] = useState(false)
  const [name, setName] = useState('')
  const [desc, setDesc] = useState('')
  const [creating, setCreating] = useState(false)

  useEffect(() => {
    let active = true
    api.listKBs()
      .then(list => { if (active) setKbs(list) })
      .catch(() => { if (active) toast.error('知识库列表加载失败') })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [toast])

  const create = async () => {
    if (!name.trim()) { toast.info('先给知识库起个名字'); return }
    setCreating(true)
    try {
      await api.createKB(name.trim(), desc.trim())
      toast.success('知识库已创建')
      setCreateOpen(false)
      setName(''); setDesc('')
      setKbs(await api.listKBs())
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '创建失败')
    } finally {
      setCreating(false)
    }
  }

  return (
    <div className="page page-wide">
      <div className="section-head" style={{ marginTop: 0 }}>
        <h2>知识库</h2>
        <span style={{ fontSize: 12.5, color: 'var(--tx-3)' }}>跨视频提问,回答会注明每个片段来自哪场视频</span>
        <span className="more" onClick={() => setCreateOpen(true)}><Icon name="plus" size="sm" />新建知识库</span>
      </div>

      {loading ? (
        <div className="card card-pad" style={{ color: 'var(--tx-3)', fontSize: 12.5 }}>加载中…</div>
      ) : kbs.length === 0 ? (
        <div className="card">
          <div className="empty">
            <Icon name="folder" size="lg" />
            <b>还没有知识库</b>
            <p>知识库把多个视频放进同一个检索范围,回答会注明每个片段来自哪场视频。</p>
            <button className="btn btn-sm btn-primary" onClick={() => setCreateOpen(true)}><Icon name="plus" size="sm" />新建知识库</button>
          </div>
        </div>
      ) : (
        <div className="kb-grid">
          {kbs.map(k => (
            <div key={k.id} className="kb-card" onClick={() => router.push(`/chat/kb/${k.id}`)}>
              <div className="kb-top">
                <div className="kb-icon" style={{ background: 'var(--acc-dim)', color: 'var(--acc-strong)', borderColor: 'var(--acc-line)' }}>
                  <Icon name="folder" />
                </div>
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
      )}

      {!loading && kbs.length > 0 && (
        <>
          <div className="section-head">
            <h2>「{kbs[0].name}」的成员视频</h2>
            <span className="more" onClick={() => router.push('/library')}>在视频库查看全部 <Icon name="chev-r" size="sm" /></span>
          </div>
          <KBMembers kbId={kbs[0].id} fallbackName={kbs[0].name} />
        </>
      )}

      {createOpen && (
        <div className="overlay" onClick={e => { if (e.target === e.currentTarget) setCreateOpen(false) }}>
          <div className="modal" style={{ width: 460 }}>
            <div className="modal-head">
              <h3>新建知识库</h3>
              <button className="btn btn-ic btn-ghost" onClick={() => setCreateOpen(false)} aria-label="关闭"><Icon name="x" /></button>
            </div>
            <div className="modal-body">
              <label className="field-label">名称</label>
              <input className="input" value={name} onChange={e => setName(e.target.value)} placeholder="例如:AI 前沿追踪" autoFocus />
              <label className="field-label" style={{ marginTop: 12 }}>描述(可选)</label>
              <input className="input" value={desc} onChange={e => setDesc(e.target.value)} placeholder="这个库收录什么内容" />
            </div>
            <div className="modal-foot">
              <button className="btn" onClick={() => setCreateOpen(false)}>取消</button>
              <button className="btn btn-primary" disabled={creating} onClick={() => void create()}>创建</button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function KBMembers({ kbId, fallbackName }: { kbId: number; fallbackName: string }) {
  const router = useRouter()
  const [videos, setVideos] = useState<KnowledgeBase['videos']>([])
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    let active = true
    api.getKB(kbId)
      .then(kb => { if (active) setVideos(kb.videos || []) })
      .catch(() => { /* 成员速览失败不阻塞页面 */ })
      .finally(() => { if (active) setLoaded(true) })
    return () => { active = false }
  }, [kbId])

  if (!loaded) return <div className="card card-pad" style={{ color: 'var(--tx-3)', fontSize: 12.5, padding: 8 }}>加载成员…</div>
  if (!videos || videos.length === 0) {
    return (
      <div className="card">
        <div className="empty" style={{ padding: 22 }}>
          <p>「{fallbackName}」还没有成员视频。在视频库上传并在设置中加入后,提问时会自动纳入检索范围。</p>
        </div>
      </div>
    )
  }
  return (
    <div className="card" style={{ padding: 8 }}>
      {videos.map(v => (
        <div key={v.task_id} className="kb-member-row" onClick={() => router.push(`/video/${v.task_id}`)}>
          <span className="vt" />
          <span className="nm">
            <b>{v.title || `任务 #${v.task_id}`}</b>
            <span className="mono">{indexStatusText(v.index_status, v.retrievable)}</span>
          </span>
          <button className="btn btn-sm btn-ghost" onClick={e => { e.stopPropagation(); router.push(`/chat/v/${v.task_id}`) }}>单独提问</button>
          <button className="btn btn-sm" onClick={e => { e.stopPropagation(); router.push(`/chat/kb/${kbId}`) }}>在库内提问</button>
        </div>
      ))}
    </div>
  )
}

function indexStatusText(indexStatus: string, retrievable: boolean): string {
  if (retrievable) return '已可检索'
  if (indexStatus === 'pending') return '索引排队中'
  if (indexStatus === 'building') return '索引构建中'
  if (indexStatus === 'failed') return '索引失败'
  return indexStatus || '未索引'
}
