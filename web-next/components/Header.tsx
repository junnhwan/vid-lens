'use client'

import Link from 'next/link'
import { Upload } from 'lucide-react'
import ThemeSwitch from './ThemeSwitch'

// 顶栏：logo + 三页导航 + 主题切换 + 上传 + 头像
// 导航 active 态由当前路径决定。
export default function Header({ active, onUpload }: { active: 'library' | 'kb' | 'settings'; onUpload?: () => void }) {
  const navCls = (k: string) =>
    `px-2.5 py-1 rounded-md text-[13px] ${active === k ? 'bg-sienna-500/10 text-sienna-700 font-medium' : 'text-ink-3 hover:text-ink-0 hover:bg-ink-2/10'}`
  return (
    <header className="shrink-0 bg-paper-0 border-b border-ink-2/20">
      <div className="flex items-center px-6 h-14 gap-6">
        <Link href="/" className="flex items-center gap-2.5">
          <span className="w-7 h-7 rounded-md bg-ink-0 text-paper-0 flex items-center justify-center text-[13px] font-semibold">V</span>
          <span className="text-[15px] font-semibold tracking-tight text-ink-0">VidLens</span>
        </Link>
        <div className="h-5 w-px bg-ink-2/20" />
        <nav className="flex items-center gap-1">
          <Link href="/" className={navCls('library')}>视频库</Link>
          <Link href="/kb" className={navCls('kb')}>知识库</Link>
          <Link href="/settings" className={navCls('settings')}>设置</Link>
        </nav>
        <div className="ml-auto flex items-center gap-3">
          <ThemeSwitch />
          {onUpload && (
            <button onClick={onUpload} className="btn-ink h-8 px-3 rounded-md flex items-center gap-1.5 text-[13px] font-medium">
              <Upload className="w-3.5 h-3.5" /> 上传视频
            </button>
          )}
          <button className="w-8 h-8 rounded-full bg-sienna-500 text-paper-0 text-[12px] font-medium flex items-center justify-center">ZJ</button>
        </div>
      </div>
    </header>
  )
}
