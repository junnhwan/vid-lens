'use client'

import { useEffect, useState } from 'react'
import { api } from '@/lib/api'
import type { User } from '@/lib/types'

// 当前用户：拉取一次 /user/profile 后模块级缓存，多个组件共享同一请求。
// 演示账号（DEMO）只读：前端据此隐藏上传/转写/摘要/索引等写入口，
// 避免陌生人点到后端 403 的困惑（后端仍强制拦截，前端隐藏只是体验层）。
let cachedUser: User | null | undefined = undefined
let cachedPromise: Promise<User | null> | null = null

function loadUser(): Promise<User | null> {
  if (cachedUser !== undefined) return Promise.resolve(cachedUser)
  if (!cachedPromise) {
    cachedPromise = api.profile()
      .then(u => { cachedUser = u; return u })
      .catch(() => { cachedUser = null; return null })
      .finally(() => { cachedPromise = null })
  }
  return cachedPromise
}

// 退出登录后必须重置，否则缓存会残留旧用户（如 DEMO → 注册用户后仍显示演示态）。
export function resetUserCache() {
  cachedUser = undefined
  cachedPromise = null
}

export function useRole(): { role: string | null; isDemo: boolean; user: User | null } {
  const [user, setUser] = useState<User | null>(null)
  useEffect(() => {
    loadUser().then(setUser)
  }, [])
  const role = user?.role ?? null
  return { role, isDemo: role === 'DEMO', user }
}