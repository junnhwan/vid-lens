'use client'

import { useState, useEffect, useCallback } from 'react'
import KBModal from '@/components/KBModal'
import KBListView from '@/components/kb/KBListView'
import { api, ApiError } from '@/lib/api'
import { useRole } from '@/lib/useRole'
import type { KnowledgeBase } from '@/lib/types'

export default function KBListPage() {
  const [kbs, setKbs] = useState<KnowledgeBase[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [showModal, setShowModal] = useState(false)
  const [manageKB, setManageKB] = useState<KnowledgeBase | null>(null)
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
    <>
      <KBListView
        kbs={kbs}
        loading={loading}
        error={err}
        isDemo={isDemo}
        onRefresh={load}
        onCreate={() => setShowModal(true)}
        onManage={setManageKB}
        onDelete={onDelete}
      />
      {showModal && <KBModal mode="create" onClose={() => setShowModal(false)} onChanged={load} />}
      {manageKB && (
        <KBModal
          mode="manage"
          kb={manageKB}
          onClose={() => { setManageKB(null); load() }}
          onChanged={onManaged}
        />
      )}
    </>
  )
}
