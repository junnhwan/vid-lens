'use client'

import { useEffect, useState } from 'react'
import { api } from '@/lib/api'

// 当前用户角色：拉取一次 /user/profile 后模块级缓存，多个组件共享同一请求。
// 演示账号（DEMO）只读：前端据此隐藏上传/转写/摘要/索引等写入口，
// 避免陌生人点到后端 403 的困惑（后端仍强制拦截，前端隐藏只是体验层）。
let cachedRole: string | null | undefined = undefined
let cachedPromise: Promise<string | null> | null = null

function loadRole(): Promise<string | null> {
  if (cachedRole !== undefined) return Promise.resolve(cachedRole)
  if (!cachedPromise) {
    cachedPromise = api.profile()
      .then(u => { cachedRole = u.role; return u.role })
      .catch(() => { cachedRole = null; return null })
      .finally(() => { cachedPromise = null })
  }
  return cachedPromise
}

export function useRole(): { role: string | null; isDemo: boolean } {
  const [role, setRole] = useState<string | null>(null)
  useEffect(() => {
    loadRole().then(setRole)
  }, [])
  return { role, isDemo: role === 'DEMO' }
}