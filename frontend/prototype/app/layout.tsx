import type { Metadata } from 'next'
import { ToastProvider } from '@/components/Toast'
import '../../app/globals.css'

const FONT_LINK = 'https://fonts.googleapis.com/css2?family=Geist:wght@400;500;600;700&family=Geist+Mono:wght@400;500&family=Noto+Sans+SC:wght@400;500;600&display=swap'

export const metadata: Metadata = {
  title: 'VidLens Prototype Workspace',
  description: 'Development-only VidLens interface prototypes',
}

export default function PrototypeRootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN" data-theme="light">
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
