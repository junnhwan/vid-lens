'use client'

import { useEffect, useState } from 'react'
import { Sun, Moon } from 'lucide-react'

// 浅色(light) / 深色(dark) 两档。切主题写 data-theme 到 <html>，CSS 变量自动级联。
// 旧版本有 5 套主题（sienna/indigo/verdigris/rust/ink），localStorage 里可能残留旧值，
// 加载时做迁移：sienna/indigo/verdigris/rust → light，ink → dark。
const LIGHT_KEYS = ['sienna', 'indigo', 'verdigris', 'rust']
const DARK_KEYS = ['ink']

type ThemeName = 'light' | 'dark'

export default function ThemeSwitch({ className = '' }: { className?: string }) {
  const [current, setCurrent] = useState<ThemeName>('light')

  // 初始读 localStorage，避免 hydration 后再切一次
  useEffect(() => {
    const saved = localStorage.getItem('vidlens-theme') || 'light'
    // 'dark' 是当前新值；DARK_KEYS 是旧版本主题名（ink）的迁移映射。
    const norm: ThemeName = saved === 'dark' || DARK_KEYS.includes(saved) ? 'dark' : 'light'
    document.documentElement.setAttribute('data-theme', norm)
    setCurrent(norm)
  }, [])

  const apply = (name: ThemeName) => {
    document.documentElement.setAttribute('data-theme', name)
    localStorage.setItem('vidlens-theme', name)
    setCurrent(name)
  }

  const item = (name: ThemeName, icon: React.ReactNode, label: string) => {
    const on = current === name
    return (
      <button
        key={name}
        onClick={() => apply(name)}
        title={label}
        className={`group flex items-center gap-1 h-6 px-2 rounded-[5px] text-[10px] font-medium transition-all
          ${on
            ? 'bg-paper-0 text-ink-0 shadow-[0_1px_2px_rgba(0,0,0,0.06),0_0_0_1px_rgba(0,0,0,0.04)]'
            : 'text-ink-4 hover:text-ink-2 hover:bg-paper-3/60'}`}
      >
        {icon}
        <span className="wide">{label}</span>
      </button>
    )
  }

  return (
    <div className={`flex items-center gap-0.5 p-0.5 rounded-md bg-paper-2 border border-ink-0/10 shrink-0 ${className}`} title="切换主题">
      {item('light', <Sun className={`w-3.5 h-3.5 transition-colors ${current === 'light' ? 'text-sienna-500' : ''}`} strokeWidth={current === 'light' ? 2 : 1.5} />, '浅色')}
      {item('dark', <Moon className={`w-3.5 h-3.5 transition-colors ${current === 'dark' ? 'text-sienna-500' : ''}`} strokeWidth={current === 'dark' ? 2 : 1.5} />, '深色')}
    </div>
  )
}