import type { Metadata } from 'next'

export const metadata: Metadata = {
  title: '映知 VidLens · 项目介绍',
  description: '让视频成为可检索、可追问、可回放验证的知识库',
}

export default function IntroLayout({ children }: { children: React.ReactNode }) {
  return children
}
