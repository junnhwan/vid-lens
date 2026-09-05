import type { Metadata } from 'next'
import { ToastProvider } from '@/components/Toast'
import { IconSprite } from '@/components/ui/Icon'
import './globals.css'

// 字体与 docs/prototype/index.html 一致:Space Grotesk(latin)+ JetBrains Mono(时间码/ID)。
// 中文走系统字体栈(见 globals.css 的 --font-ui),不额外下载 webfont。
const FONT_LINK = 'https://fonts.googleapis.com/css2?family=Space+Grotesk:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500;600;700&display=swap'

export const metadata: Metadata = {
  title: '映知 VidLens — 可回放的视频问答',
  description: '上传视频,转写与画面索引,每个回答都带可回放的时间点证据引用。',
}

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN">
      <head>
        <link rel="preconnect" href="https://fonts.googleapis.com" />
        <link rel="preconnect" href="https://fonts.gstatic.com" crossOrigin="anonymous" />
        <link href={FONT_LINK} rel="stylesheet" />
      </head>
      <body>
        <IconSprite />
        <ToastProvider>{children}</ToastProvider>
      </body>
    </html>
  )
}
