'use client'

// 路由级错误边界：捕获任何运行时错误，避免甩出 Next 原生错误页踢出 app shell。
// reset() 让该路由段重新渲染（不清空全局状态）。
import { useEffect } from 'react'
import { AlertTriangle, RefreshCw } from 'lucide-react'

export default function RouteError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  useEffect(() => {
    // 生产可接 Sentry/上报；这里先 console，方便本地排查
    console.error('[route-error]', error)
  }, [error])

  return (
    <div className="flex-1 flex items-center justify-center px-4">
      <div className="text-center max-w-md">
        <div className="inline-flex items-center justify-center w-12 h-12 border border-rust/30 bg-rust/5 mb-4">
          <AlertTriangle className="w-5 h-5 text-rust" />
        </div>
        <h1 className="font-sans text-[18px] font-semibold text-ink-0 tight">出了点问题</h1>
        <p className="font-sans text-[13px] text-ink-3 mt-2 leading-relaxed">
          页面在渲染时遇到了错误。可以重试，或返回视频库继续。
        </p>
        {error?.message && (
          <p className="font-mono text-[11px] text-ink-4 mt-3 break-all bg-ink-0/[.03] px-3 py-2">{error.message}</p>
        )}
        <div className="flex items-center justify-center gap-2 mt-5">
          <button onClick={reset} className="btn-ink h-8 px-4 text-[12px] font-medium flex items-center gap-1.5">
            <RefreshCw className="w-3.5 h-3.5" />重试
          </button>
          <a href="/" className="btn-line h-8 px-4 text-[12px] font-medium flex items-center">返回视频库</a>
        </div>
      </div>
    </div>
  )
}
