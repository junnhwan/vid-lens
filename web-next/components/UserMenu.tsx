'use client'

import { useEffect, useRef, useState } from 'react'
import { useRouter } from 'next/navigation'
import { LogOut } from 'lucide-react'
import { useRole, resetUserCache } from '@/lib/useRole'
import { clearToken } from '@/lib/api'

// 用户菜单：头像点击展开，显示当前用户信息 + 退出登录。
// 退出即清 token + 重置角色缓存 + 跳登录页（后端 JWT 无状态，无需服务端登出）。
// 共享 Header 与聊天/知识库详情页的自定义顶栏都复用本组件。
export default function UserMenu() {
  const router = useRouter()
  const { isDemo, user } = useRole()
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  // 点击外部 / Escape 关闭
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(false) }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const logout = () => {
    clearToken()
    resetUserCache()
    router.push('/login')
  }

  const displayName = user?.nickname || user?.username || ''
  const avatarChar = (displayName || '?').charAt(0).toUpperCase()

  return (
    <div className="relative" ref={ref}>
      <button
        onClick={() => setOpen(o => !o)}
        className="w-8 h-8 rounded-full bg-sienna-500 text-paper-0 text-[12px] font-medium flex items-center justify-center"
        title={displayName || '用户'}
      >
        {avatarChar}
      </button>
      {open && (
        <div className="absolute right-0 top-full mt-2 w-48 rounded-md border border-ink-2/20 bg-paper-0 shadow-lg z-50 overflow-hidden">
          <div className="px-3 py-2.5 border-b border-ink-2/10">
            <div className="font-sans text-[13px] font-medium text-ink-0 truncate">{displayName || '未登录'}</div>
            <div className="font-mono text-[10px] text-ink-4 mt-0.5 truncate">
              @{user?.username || '—'}
              {isDemo && <span className="ml-1.5 text-sienna-700">演示</span>}
            </div>
          </div>
          <button
            onClick={logout}
            className="w-full flex items-center gap-2 px-3 py-2.5 text-[12px] text-ink-2 hover:bg-ink-2/10 hover:text-ink-0"
          >
            <LogOut className="w-3.5 h-3.5" /> 退出登录
          </button>
        </div>
      )}
    </div>
  )
}