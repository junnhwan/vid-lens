'use client'

import { useEffect, useState } from 'react'

// 5 套主题色块，点击写 data-theme 到 <html>，CSS 变量自动级联。
// 不做系统明暗自动跟随（PRD 要求先手动切）。
const THEMES = [
  { name: 'sienna', color: '#9A4A1A' },
  { name: 'indigo', color: '#2B4A8C' },
  { name: 'verdigris', color: '#B8842E' },
  { name: 'rust', color: '#A03A18' },
  { name: 'ink', color: '#0E0E11' },
] as const

export default function ThemeSwitch({ className = '' }: { className?: string }) {
  const [current, setCurrent] = useState<string>('sienna')

  // 初始读 localStorage，避免 hydration 后再切一次
  useEffect(() => {
    const saved = localStorage.getItem('vidlens-theme') || 'sienna'
    document.documentElement.setAttribute('data-theme', saved)
    setCurrent(saved)
  }, [])

  const apply = (name: string) => {
    document.documentElement.setAttribute('data-theme', name)
    localStorage.setItem('vidlens-theme', name)
    setCurrent(name)
  }

  return (
    <div className={`flex items-center gap-1.5 px-1.5 py-1 border border-ink-2/20 rounded-md ${className}`} title="切换主题">
      {THEMES.map((t) => (
        <span
          key={t.name}
          className={`swatch ${current === t.name ? 'is-on' : ''}`}
          style={{ background: t.color }}
          onClick={() => apply(t.name)}
        />
      ))}
    </div>
  )
}
