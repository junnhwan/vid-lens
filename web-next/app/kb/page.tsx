'use client'

import { useState, useEffect, useCallback } from 'react'
import Link from 'next/link'
import { Plus, Trash2, Library, ArrowRight } from 'lucide-react'
import Header from '@/components/Header'
import KBModal from '@/components/KBModal'
import { api, ApiError } from '@/lib/api'
import { useRole } from '@/lib/useRole'
import type { KnowledgeBase } from '@/lib/types'

// 知识库列表入口（Header "知识库" 指向此）。
// 列出 KB + 新建 + 进入问答 + 管理 + 删除。
export default function KBListPage() {
  const [kbs, setKbs] = useState<KnowledgeBase[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [showModal, setShowModal] = useState(false)
  const [manageKB, setManageKB] = useState<KnowledgeBase | null>(null)
  // 演示账号只读：隐藏新建/删除/管理成员等写入口，仅保留查看与问答。
  const { isDemo } = useRole()

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setKbs(await api.listKBs())
      setErr('')
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载失败')
    } finally { setLoading(false) }
  }, [])

  useEffect(() => { load() }, [load])

  const onDelete = async (id: number) => {
    if (!confirm('删除该知识库？成员关系会解除，不影响视频本身。')) return
    try { await api.deleteKB(id); load() } catch (e) { setErr(e instanceof ApiError ? e.message : '删除失败') }
  }

  // 管理成员后刷新当前 manageKB 详情
  const onManaged = async () => {
    await load()
    if (manageKB) {
      try {
        const fresh = await api.getKB(manageKB.id)
        setManageKB(fresh)
      } catch { /* ignore */ }
    }
  }

  return (
    <div className="flex flex-col h-screen">
      <Header active="kb" />
      <main className="flex-1 overflow-y-auto">
        <div className="max-w-[920px] mx-auto px-8 py-8">
          <div className="pb-6 border-b border-ink-0/15">
            <div className="font-sans text-[10px] text-ink-4">知识库</div>
            <div className="flex items-end justify-between gap-4 mt-1.5">
              <h1 className="font-sans text-[36px] leading-[1.05] font-medium tight text-ink-0">知识库<span className="text-sienna-500">.</span></h1>
              {!isDemo && (
                <button onClick={() => setShowModal(true)} className="btn-ink h-8 px-3.5 font-sans text-[11px] flex items-center gap-1.5">
                  <Plus className="w-3.5 h-3.5" />新建知识库
                </button>
              )}
            </div>
            <p className="font-sans italic text-[14px] text-ink-3 mt-1.5">跨视频严格 RAG。添加多个视频，引用标注来源。</p>
          </div>

          {err && <div className="py-6 text-[13px] text-rust">{err}<button onClick={load} className="ml-2 underline">重试</button></div>}

          {loading ? (
            <div className="mt-7 space-y-3">
              {[0,1,2].map(i => <div key={i} className="border border-ink-0/15 px-5 py-4"><div className="sk h-4 w-1/3 mb-2" /><div className="sk h-3 w-1/2" /></div>)}
            </div>
          ) : kbs.length === 0 ? (
            <div className="py-16 text-center">
              <Library className="w-8 h-8 text-ink-4 mx-auto mb-3" />
              <div className="font-mono text-[10px] text-ink-4 wide uppercase mb-2">— 暂无知识库 —</div>
              {!isDemo && (
                <button onClick={() => setShowModal(true)} className="btn-line h-8 px-3 mt-2 text-[12px] font-medium inline-flex items-center gap-1.5"><Plus className="w-3.5 h-3.5" />创建第一个</button>
              )}
            </div>
          ) : (
            <ul className="mt-7 space-y-2">
              {kbs.map(kb => (
                <li key={kb.id} className="entry border border-ink-0/15 px-5 py-4 hover:bg-ink-0/[.02]">
                  <div className="flex items-start gap-4">
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <h3 className="font-sans text-[16px] font-medium tight text-ink-0 truncate">{kb.name}</h3>
                        <span className="font-mono text-[10px] text-ink-4">№KB-{pad(kb.id)}</span>
                      </div>
                      {kb.description && <p className="font-sans text-[13px] text-ink-3 mt-0.5 line-clamp-1">{kb.description}</p>}
                      <div className="font-mono text-[10px] text-ink-4 mt-1.5">{kb.member_count} 个视频 · 创建于 {fmt(kb.created_at)}</div>
                    </div>
                    <div className="flex items-center gap-2 shrink-0">
                      {!isDemo && (
                        <button onClick={() => setManageKB(kb)} className="btn-line h-7 px-2.5 text-[10px] font-medium">管理成员</button>
                      )}
                      <Link href={`/kb/${kb.id}`} className="btn-ink h-7 px-2.5 text-[10px] font-medium flex items-center gap-1">去问答 <ArrowRight className="w-3 h-3" /></Link>
                      {!isDemo && (
                        <button onClick={() => onDelete(kb.id)} className="w-7 h-7 flex items-center justify-center text-ink-4 hover:text-rust"><Trash2 className="w-3.5 h-3.5" /></button>
                      )}
                    </div>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>
      </main>

      {showModal && <KBModal mode="create" onClose={() => setShowModal(false)} onChanged={load} />}
      {manageKB && <KBModal mode="manage" kb={manageKB} onClose={() => { setManageKB(null); load() }} onChanged={onManaged} />}
    </div>
  )
}

function pad(n: number) { return n < 10 ? `0${n}` : `${n}` }
function fmt(iso: string) {
  const d = new Date(iso)
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}
