'use client'

import { useState, useEffect, useRef, useCallback } from 'react'
import { useParams, useRouter } from 'next/navigation'
import { Settings2, Plus, Trash2 } from 'lucide-react'
import ChatInput from '@/components/ChatInput'
import ChatShell, { ChatHeader, ChatSidebar, ChatFooter } from '@/components/chat/ChatShell'
import ChatMessageRow from '@/components/chat/ChatMessageRow'
import { parseMessages, fmtSession } from '@/components/chat/chatUtils'
import { useConversationSession } from '@/components/chat/useConversationSession'
import KBModal from '@/components/KBModal'
import type { CiteRef } from '@/components/Citation'
import { api } from '@/lib/api'
import { useRole } from '@/lib/useRole'
import type { ChatMessage, Citation, KnowledgeBase } from '@/lib/types'

const DOT_COLORS = ['#9A4A1A', '#2A6B5E', '#3F62C2', '#6B7A2A', '#8C3A4A', '#B8842E', '#5C2A0D', '#1D3468']

export default function KBChatPage() {
  const params = useParams<{ kbId: string }>()
  const kbId = Number(params.kbId)
	const router = useRouter()

	const [kb, setKb] = useState<KnowledgeBase | null>(null)
	const topK = 8
	const [showManage, setShowManage] = useState(false)
	const scrollRef = useRef<HTMLDivElement>(null)
	const { isDemo } = useRole()

  const kbRef = useRef(kb)
  useEffect(() => { kbRef.current = kb }, [kb])
  const colorFor = useCallback((taskId: number) => {
    const members = kbRef.current?.videos || []
    const idx = members.findIndex(v => v.task_id === taskId)
    return DOT_COLORS[idx >= 0 ? idx % DOT_COLORS.length : 0]
  }, [])

	useEffect(() => { api.getKB(kbId).then(setKb).catch(() => {}) }, [kbId])

	const parseHistory = useCallback((messages: ChatMessage[]) => parseMessages(messages, colorFor), [colorFor])
	const mapCitations = useCallback((citations: Citation[]): CiteRef[] => citations.map((citation, index) => ({
		id: `C${index + 1}`,
		chunkIndex: citation.chunk_index,
		score: citation.score,
		content: citation.content,
		source: citation.source,
		videoTitle: citation.video_title,
		finalRank: citation.final_rank,
		color: colorFor(citation.task_id),
	})), [colorFor])
	const {
		session, sessions, messages, streaming, switchSession, newSession, send, stop, toggleCite,
	} = useConversationSession({
		scopeType: 'knowledge_base', targetId: kbId, basePath: `/kb/${kbId}`,
		mode: 'strict_rag', topK, parseHistory, mapCitations,
	})

  useEffect(() => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [messages, streaming])

  const clearSession = async () => {
    if (!session) return
    if (!window.confirm('确认清空当前会话的所有消息？此操作不可撤销。')) return
    try {
      await api.deleteSession(session.id)
      router.push('/kb')
    } catch { /* ignore */ }
  }

  return (
    <>
      <ChatShell
        scrollRef={scrollRef}
        header={
          <ChatHeader
            backHref="/kb"
            backLabel="知识库"
            kicker="跨视频问答"
            title={kb?.name || '加载中…'}
            actions={!isDemo ? (
              <button
                onClick={() => setShowManage(true)}
                className="h-8 px-3 rounded-lg border border-ink-0/10 text-[12px] flex items-center gap-1.5 hover:bg-paper-1 transition-colors"
              >
                <Settings2 className="w-3.5 h-3.5" />管理成员
              </button>
            ) : undefined}
          />
        }
        sidebar={
          <ChatSidebar>
            <div className="h-14 px-4 border-b border-ink-0/8 flex items-center justify-between shrink-0">
              <span className="text-[13px] font-medium text-ink-2">会话</span>
              <button onClick={newSession} className="text-[12px] text-sienna-700 hover:text-sienna-600">新建</button>
            </div>
            <div className="flex-1 overflow-y-auto px-2 py-2">
              <ul className="space-y-0.5">
                {sessions.map(s => (
                  <li key={s.id}>
                    <button
                      onClick={() => switchSession(s.id)}
                      title={fmtSession(s.created_at)}
                      className={`w-full flex items-center gap-2 px-2.5 py-2 rounded-lg text-left text-[13px] ui-row-hover ${
                        session?.id === s.id ? 'bg-sienna-500/8 text-sienna-800 font-medium' : 'text-ink-3'
                      }`}
                    >
                      <span className="truncate">{s.title || '新会话'}</span>
                    </button>
                  </li>
                ))}
                {sessions.length === 0 && <li className="text-[12px] text-ink-4 px-2.5 py-2">还没有会话</li>}
              </ul>
              <div className="mt-5 px-2.5">
                <div className="text-[11px] text-ink-4 mb-2">成员 · {kb?.videos?.length ?? 0}</div>
                <ul className="space-y-1.5">
                  {(kb?.videos || []).map((v, i) => (
                    <li key={v.task_id} className="flex items-center gap-2 py-0.5">
                      <span className="src-dot" style={{ background: DOT_COLORS[i % DOT_COLORS.length] }} />
                      <span className="text-[12px] truncate flex-1">{v.title}</span>
                    </li>
                  ))}
                  {(kb?.videos?.length ?? 0) === 0 && <li className="text-[12px] text-ink-4">暂无成员</li>}
                </ul>
                {!isDemo && (
                  <button
                    onClick={() => setShowManage(true)}
                    className="mt-2 w-full h-7 rounded-lg border border-dashed border-ink-0/15 text-[11px] text-ink-4 hover:border-sienna-500/40 hover:text-sienna-700 flex items-center justify-center gap-1"
                  >
                    <Plus className="w-3 h-3" />添加视频
                  </button>
                )}
              </div>
            </div>
          </ChatSidebar>
        }
        footer={
          <ChatFooter
            footerAction={
              session ? (
                <button
                  onClick={clearSession}
                  className="text-ink-4 hover:text-rust flex items-center gap-1 text-[11px]"
                >
                  <Trash2 className="w-3 h-3" />清空会话
                </button>
              ) : undefined
            }
          >
            <ChatInput
              onSend={send}
              onStop={stop}
              streaming={streaming}
              placeholder="就知识库里的视频提问…"
            />
          </ChatFooter>
        }
      >
        <>
          {messages.length === 0 && (
            <p className="text-[13px] text-ink-4 ui-fade-in">
              会在知识库全部视频里检索，引用会标出来源。
            </p>
          )}

          {messages.map((m, i) => (
            <ChatMessageRow
              key={i}
              msg={m}
              idx={i}
              onToggleCite={toggleCite}
              modeLabel="跨视频问答"
            />
          ))}
        </>
      </ChatShell>

      {showManage && kb && (
        <KBModal
          mode="manage"
          kb={kb}
          onClose={async () => { setShowManage(false); try { setKb(await api.getKB(kbId)) } catch {} }}
          onChanged={async () => { try { setKb(await api.getKB(kbId)) } catch {} }}
        />
      )}
    </>
  )
}
