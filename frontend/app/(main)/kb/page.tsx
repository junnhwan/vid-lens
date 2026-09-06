'use client'

import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'
import { api, ApiError } from '@/lib/api'
import type { KnowledgeBase } from '@/lib/types'
import { fmtRelTime } from '@/lib/format'
import { useCrumb } from '@/components/shell/AppShell'
import { useToast } from '@/components/Toast'
import { Icon } from '@/components/ui/Icon'
import { Modal } from '@/components/ui/Modal'

export default function KBListPage() {
  const router = useRouter()
  const toast = useToast()
  useCrumb([{ label: '知识库' }])

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
      const created = await api.createKB(name.trim(), desc.trim())
      toast.success('知识库已创建')
      setCreateOpen(false)
      setName(''); setDesc('')
      router.push(`/kb/${created.id}`)
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
        <span className="more" onClick={() => setCreateOpen(true)}><Icon name="plus" size="sm" />新建知识库</span>
      </div>

      {loading ? (
        <div className="card card-pad" style={{ color: 'var(--tx-3)', fontSize: 12.5 }}>加载中…</div>
      ) : kbs.length === 0 ? (
        <div className="card">
          <div className="empty">
            <Icon name="folder" size="lg" />
            <b>还没有知识库</b>
            <button className="btn btn-sm btn-primary" onClick={() => setCreateOpen(true)}><Icon name="plus" size="sm" />新建知识库</button>
          </div>
        </div>
      ) : (
        <div className="kb-grid">
          {kbs.map(k => (
            <div key={k.id} className="kb-card" onClick={() => router.push(`/kb/${k.id}`)}>
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
                  查看成员 <Icon name="chev-r" size="sm" />
                </span>
              </div>
            </div>
          ))}
        </div>
      )}

      {createOpen && (
        <Modal
          title="新建知识库"
          onClose={() => setCreateOpen(false)}
          confirmOnClose={!!(name || desc)}
          width={460}
          footer={(
            <>
              <button className="btn" onClick={() => setCreateOpen(false)}>取消</button>
              <button className="btn btn-primary" disabled={creating} onClick={() => void create()}>创建</button>
            </>
          )}
        >
          <label className="field-label">名称</label>
          <input className="input" value={name} onChange={e => setName(e.target.value)} placeholder="例如:AI 前沿追踪" autoFocus />
          <label className="field-label" style={{ marginTop: 12 }}>描述(可选)</label>
          <input className="input" value={desc} onChange={e => setDesc(e.target.value)} placeholder="这个库收录什么" />
        </Modal>
      )}
    </div>
  )
}
