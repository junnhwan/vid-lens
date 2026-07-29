import type { Metadata } from 'next'
import { ToastProvider } from '@/components/Toast'
import './globals.css'

// 字体：mockup 用 Google Fonts <link> 加载 Geist + Geist Mono + Noto Sans SC。
// next/font/google 在 Next 14.2.5 无 Geist 导出，沿用 <link> 方式与 mockup 一致。
// 禁用 Inter/Roboto/Fraunces/Noto Serif。
const FONT_LINK = 'https://fonts.googleapis.com/css2?family=Geist:wght@400;500;600;700&family=Geist+Mono:wght@400;500&family=Noto+Sans+SC:wght@400;500;600&display=swap'

export const metadata: Metadata = {
  title: 'VidLens · 映知 — AI 视频理解',
  description: '上传视频，ASR 转写 / LLM 摘要 / 转写内容 RAG 问答',
}

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN" data-theme="sienna">
      <head>
        <link rel="preconnect" href="https://fonts.googleapis.com" />
        <link rel="preconnect" href="https://fonts.gstatic.com" crossOrigin="anonymous" />
        <link href={FONT_LINK} rel="stylesheet" />
      </head>
      <body className="bg-paper-1 text-ink-0 font-sans antialiased">
        <ToastProvider>{children}</ToastProvider>
      </body>
    </html>
  )
}
