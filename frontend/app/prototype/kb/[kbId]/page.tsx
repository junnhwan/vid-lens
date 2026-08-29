'use client'

import { useEffect, useState } from 'react'
import { useParams } from 'next/navigation'
import KBChatView from '@/components/prototype/c/KBChatView'
import { api } from '@/lib/api'
import type { KnowledgeBase } from '@/lib/types'
import { MOCK_KBS } from '@/components/prototype/c/mocks'

export default function PrototypeKBChatPage() {
  const params = useParams<{ kbId: string }>()
  const kbId = Number(params.kbId)
  const [kb, setKb] = useState<KnowledgeBase | null>(null)

  useEffect(() => {
    api.getKB(kbId).then(setKb).catch(() => {
      setKb(MOCK_KBS.find(k => k.id === kbId) ?? MOCK_KBS[0])
    })
  }, [kbId])

  return <KBChatView kb={kb} kbId={kbId} />
}
