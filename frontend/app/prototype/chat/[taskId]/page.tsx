'use client'

import { useEffect, useState } from 'react'
import { useParams } from 'next/navigation'
import VideoChatView from '@/components/prototype/c/VideoChatView'
import { api } from '@/lib/api'
import type { VideoTask } from '@/lib/types'
import { MOCK_TASKS } from '@/components/prototype/c/mocks'

export default function PrototypeChatPage() {
  const params = useParams<{ taskId: string }>()
  const taskId = Number(params.taskId)
  const [task, setTask] = useState<VideoTask | null>(null)

  useEffect(() => {
    api.getTask(taskId).then(setTask).catch(() => {
      setTask(MOCK_TASKS.find(t => t.id === taskId) ?? MOCK_TASKS[0])
    })
  }, [taskId])

  return <VideoChatView task={task} taskId={taskId} />
}
