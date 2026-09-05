'use client'

// 路由级错误边界:捕获任何运行时错误,避免甩出 Next 原生错误页。
import { useEffect } from 'react'

export default function RouteError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  useEffect(() => {
    console.error('[route-error]', error)
  }, [error])

  return (
    <div style={{ minHeight: '60vh', display: 'grid', placeItems: 'center', padding: 24 }}>
      <div style={{ textAlign: 'center', maxWidth: 420 }}>
        <b style={{ fontSize: 17 }}>出了点问题</b>
        <p style={{ fontSize: 13, color: 'var(--tx-3)', marginTop: 8, lineHeight: 1.7 }}>
          页面在渲染时遇到了错误。可以重试,或返回工作台继续。
        </p>
        {error?.message && (
          <p className="mono" style={{ fontSize: 11, color: 'var(--tx-4)', marginTop: 12, wordBreak: 'break-all' }}>{error.message}</p>
        )}
        <div style={{ display: 'flex', gap: 10, justifyContent: 'center', marginTop: 20 }}>
          <button className="btn" onClick={reset}>重试</button>
          <a href="/" className="btn btn-ghost">返回工作台</a>
        </div>
      </div>
    </div>
  )
}
