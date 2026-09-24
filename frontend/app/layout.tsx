import type { Metadata } from 'next'
import { ToastProvider } from '@/components/Toast'
import { IconSprite } from '@/components/ui/Icon'
import { ThemeProvider } from '@/components/theme/ThemeProvider'
import './globals.css'

const FONT_LINK = 'https://fonts.googleapis.com/css2?family=Noto+Sans+SC:wght@400;500;600;700&family=Noto+Serif+SC:wght@400;600;700&family=JetBrains+Mono:wght@400;500;600;700&display=swap'

const THEME_BOOT = `try{var t=localStorage.getItem('vidlens-theme');if(t==='light'||t==='dark')document.documentElement.setAttribute('data-theme',t)}catch(e){}`

// Google Fonts 只服务工作台的衬线/等宽文本。公开页(介绍页、文档站)走系统字体栈,
// 这里改为非阻塞加载,避免首屏被中文字体请求挡住;加载完成后再切回 all。
const FONT_BOOT = `try{var l=document.getElementById('gfonts');if(l)l.media='all'}catch(e){}`

export const metadata: Metadata = {
  title: '映知 VidLens',
  description: '视频转写与可回放问答',
}

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: THEME_BOOT }} />
        <link rel="preconnect" href="https://fonts.googleapis.com" />
        <link rel="preconnect" href="https://fonts.gstatic.com" crossOrigin="anonymous" />
        {/* media="print"→all 的切换会让 React 报一次属性不匹配,这里显式压掉 */}
        <link id="gfonts" href={FONT_LINK} rel="stylesheet" media="print" suppressHydrationWarning />
        <noscript>
          <link href={FONT_LINK} rel="stylesheet" />
        </noscript>
        <script dangerouslySetInnerHTML={{ __html: FONT_BOOT }} />
      </head>
      <body>
        <IconSprite />
        <ThemeProvider>
          <ToastProvider>{children}</ToastProvider>
        </ThemeProvider>
      </body>
    </html>
  )
}
