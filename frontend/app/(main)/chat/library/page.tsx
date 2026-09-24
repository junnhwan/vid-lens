'use client'

import { ChatWorkspace } from '@/components/chat/ChatWorkspace'
import { useCrumb } from '@/components/shell/AppShell'

export default function VideoLibraryChatPage() {
  useCrumb([{ label: '问答', href: '/chat' }, { label: '视频库问答' }])
  return <ChatWorkspace scopeType="video_library" targetId={0} scopeName="整个视频库 · 已建立索引的视频" playbackUrl={null} suggestions={[
    '这些视频共同讨论了哪些主题？', '不同视频对这个问题有什么差异？',
  ]} />
}
