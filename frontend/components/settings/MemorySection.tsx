'use client'

import { useCallback, useEffect, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import type { MemoryItem, MemoryPreferenceView, User } from '@/lib/types'
import { Icon } from '@/components/ui/Icon'
import { useToast } from '@/components/Toast'

// 记忆治理(设置页):用户级长期记忆偏好开关 + 已有记忆的撤回/删除。
// 后端只有一个用户偏好开关(能力开关 × 用户偏好),会话总结异步抽取与召回都由它统一控制;
// 撤回不删除历史,只是不再参与召回;删除才是彻底移除。

const SOURCE_TEXT: Record<string, string> = {
  conversation_summary: '会话总结',
  manual: '人工添加',
  session_summary: '会话总结',
}

// model.MemoryPolicyReason 的展示文案;未知 token 原样显示
const REASON_TEXT: Record<string, string> = {
  capability_disabled: '服务端未开启记忆能力',
  session_disabled: '会话策略关闭记忆',
  session_enabled: '会话策略开启记忆',
  user_enabled: '用户偏好已开启',
  user_disabled: '用户偏好已关闭',
  policy_unavailable: '记忆策略服务不可用',
}

function reasonText(reason: string): string {
  return REASON_TEXT[reason] || reason
}

function fmtDay(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function importanceText(v: number): string {
  if (v >= 0.7) return '重要度 高'
  if (v >= 0.4) return '重要度 中'
  return '重要度 低'
}

export function MemorySection({ user }: { user: User | null }) {
  const toast = useToast()
  const [pref, setPref] = useState<MemoryPreferenceView | null>(null)
  const [items, setItems] = useState<MemoryItem[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [savingPref, setSavingPref] = useState(false)
  const [busyId, setBusyId] = useState<string | null>(null)

  const load = useCallback(async (userId: number) => {
    setLoading(true)
    setLoadError('')
    try {
      const [p, list] = await Promise.all([
        api.getMemoryPreference(),
        api.listMemories('user', String(userId)).catch(() => [] as MemoryItem[]),
      ])
      setPref(p)
      setItems(list)
    } catch (e) {
      setLoadError(e instanceof ApiError ? e.message : '记忆治理信息加载失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (user) void load(user.id)
  }, [user, load])

  const togglePref = async () => {
    if (!pref || savingPref) return
    if (!pref.capability_enabled) {
      toast.info(reasonText(pref.reason || '') || '服务端未开启记忆能力')
      return
    }
    setSavingPref(true)
    try {
      const next = await api.updateMemoryPreference(!pref.enabled, pref.version)
      setPref(next)
      toast.success(next.enabled ? '长期记忆已开启' : '长期记忆已关闭')
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        toast.info('偏好刚被其他请求修改,已重新读取')
        if (user) void load(user.id)
      } else {
        toast.error(e instanceof ApiError ? e.message : '修改偏好失败')
      }
    } finally {
      setSavingPref(false)
    }
  }

  const withdraw = async (item: MemoryItem) => {
    if (busyId) return
    setBusyId(item.id)
    try {
      await api.withdrawMemory(item.id)
      toast.success('已撤回,该记忆不再参与召回')
      if (user) void load(user.id)
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '撤回失败')
    } finally {
      setBusyId(null)
    }
  }

  const remove = async (item: MemoryItem) => {
    if (busyId) return
    if (!window.confirm('彻底删除这条记忆?撤回只停止召回,删除不可恢复。')) return
    setBusyId(item.id)
    try {
      await api.deleteMemory(item.id)
      toast.success('记忆已删除')
      if (user) void load(user.id)
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '删除失败')
    } finally {
      setBusyId(null)
    }
  }

  return (
    <>
      <h3 style={{ fontSize: 14, fontWeight: 600, marginBottom: 4 }}>记忆治理</h3>
      <p style={{ fontSize: 12.5, color: 'var(--tx-3)', marginBottom: 16 }}>
        记忆按用户 / 视频 / 知识库 / 会话隔离,写入异步进行;撤回不会删除历史,只是不再参与召回。
      </p>

      {loading && <div className="empty card"><p>正在加载记忆治理…</p></div>}
      {!loading && loadError && (
        <div className="empty card">
          <Icon name="alert" size="lg" />
          <b>{loadError}</b>
          <button className="btn btn-sm" style={{ marginTop: 10 }} onClick={() => user && void load(user.id)}>
            <Icon name="refresh" size="sm" />重试
          </button>
        </div>
      )}

      {!loading && !loadError && pref && (
        <>
          <div className="pref-row">
            <div className="pr-body">
              <b>长期记忆偏好</b>
              <span>
                开启后回答参考召回的记忆,会话结束后异步抽取偏好与事实,冲突时保留版本并降低置信度
                {pref.reason ? ` · ${reasonText(pref.reason)}` : ''}
              </span>
            </div>
            <button
              className={`switch${pref.enabled ? ' on' : ''}`}
              disabled={savingPref || !pref.capability_enabled}
              title={!pref.capability_enabled ? (reasonText(pref.reason || '') || '服务端未开启记忆能力') : undefined}
              style={!pref.capability_enabled ? { opacity: 0.5, cursor: 'not-allowed' } : undefined}
              onClick={() => void togglePref()}
              aria-label="长期记忆偏好开关"
            />
          </div>
          {pref.capability_enabled && !pref.effective_enabled && (
            <p style={{ fontSize: 11.5, color: 'var(--tx-4)', marginTop: 8 }}>{reasonText(pref.reason || '') || '当前记忆能力未生效。'}</p>
          )}

          <div className="section-head" style={{ marginTop: 24 }}>
            <h2 style={{ fontSize: 14 }}>已有记忆</h2>
            <span style={{ fontSize: 12, color: 'var(--tx-3)' }}>{items.length} 条 · 用户范围</span>
          </div>

          {items.length === 0 ? (
            <div className="empty card">
              <Icon name="bulb" size="lg" />
              <b>还没有长期记忆</b>
              <p>开启偏好后,会话结束时会异步总结出可复用的偏好与事实。</p>
            </div>
          ) : items.map(m => (
            <div key={m.id} className={`memory-item${m.status === 'withdrawn' ? ' withdrawn' : ''}`}>
              <div className="mi-body">
                <div className="mi-text">{m.content}</div>
                <div className="mi-meta">
                  <span className="chip chip-mute">{scopeText(m.scope_type)}</span>
                  {m.status === 'conflicted' && <span className="chip chip-warn">冲突待确认</span>}
                  {m.status === 'withdrawn'
                    ? <span className="chip chip-mute">已撤回</span>
                    : <span className="chip chip-mute">{importanceText(m.importance)}</span>}
                  <span className="mi-when">
                    {fmtDay(m.created_at)}{m.source_type ? ` · 来源: ${SOURCE_TEXT[m.source_type] || m.source_type}` : ''}
                  </span>
                </div>
              </div>
              {m.status !== 'withdrawn' ? (
                <button className="btn btn-sm btn-ghost" disabled={busyId === m.id} onClick={() => void withdraw(m)}>
                  撤回
                </button>
              ) : (
                <button className="btn btn-sm btn-ghost" disabled={busyId === m.id} onClick={() => void remove(m)}>
                  删除
                </button>
              )}
            </div>
          ))}
        </>
      )}
    </>
  )
}

function scopeText(scope: string): string {
  if (scope === 'user') return '用户'
  if (scope === 'video') return '视频'
  if (scope === 'knowledge_base') return '知识库'
  if (scope === 'run') return '运行'
  return scope
}
