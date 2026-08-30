'use client'

import { useEffect, useState } from 'react'
import Link from 'next/link'
import { MessageCircle, Library } from 'lucide-react'
import { api } from '@/lib/api'
import { taskTitle } from '@/lib/format'
import { TaskStatusEnum } from '@/lib/types'
import type { KnowledgeBase, VideoTask } from '@/lib/types'

const RECENT_LIMIT = 5

/** AppShell 侧栏：最近可问答入口（单视频 + 知识库） */
export default function QaRecentShortcuts() {
  const [tasks, setTasks] = useState<VideoTask[]>([])
  const [kbs, setKbs] = useState<KnowledgeBase[]>([])

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const [taskRes, kbList] = await Promise.all([
          api.listTasks(1, 30, ''),
          api.listKBs(),
        ])
        if (cancelled) return
        const recent = (taskRes.list || [])
          .filter(t => t.status === TaskStatusEnum.Completed && t.has_transcription)
          .slice(0, RECENT_LIMIT)
        setTasks(recent)
        setKbs((kbList || []).slice(0, RECENT_LIMIT))
      } catch { /* 侧栏静默失败 */ }
    })()
    return () => { cancelled = true }
  }, [])

  if (tasks.length === 0 && kbs.length === 0) return null

  return (
    <div className="px-4 pt-2 pb-2 border-t border-ink-0/8 mt-2">
      <div className="text-[10px] uppercase tracking-wider text-ink-4 mb-2 px-1">最近可问答</div>
      {tasks.length > 0 && (
        <>
          <div className="text-[10px] text-sienna-700/80 px-2 mb-1">单视频</div>
          <ul className="space-y-0.5 mb-2">
            {tasks.map(v => (
              <li key={v.id}>
                <Link
                  href={`/chat/${v.id}`}
                  className="flex items-center gap-2 px-2 py-1.5 rounded-lg text-[12px] text-ink-2 hover:bg-sienna-500/8 ui-row-hover"
                >
                  <MessageCircle className="w-3 h-3 text-sienna-600 shrink-0" />
                  <span className="truncate">{taskTitle(v)}</span>
                </Link>
              </li>
            ))}
          </ul>
        </>
      )}
      {kbs.length > 0 && (
        <>
          <div className="text-[10px] text-ink-4 px-2 mb-1">知识库</div>
          <ul className="space-y-0.5">
            {kbs.map(k => (
              <li key={k.id}>
                <Link
                  href={`/kb/${k.id}`}
                  className="flex items-center gap-2 px-2 py-1.5 rounded-lg text-[12px] text-ink-2 hover:bg-ink-0/4 ui-row-hover"
                >
                  <Library className="w-3 h-3 text-ink-4 shrink-0" />
                  <span className="truncate">{k.name}</span>
                </Link>
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  )
}
