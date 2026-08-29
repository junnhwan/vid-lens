import Link from 'next/link'
import { Compass } from 'lucide-react'

// 全局 404：匹配不到任何路由时显示。
export default function NotFound() {
  return (
    <div className="min-h-screen flex items-center justify-center px-4">
      <div className="text-center">
        <div className="inline-flex items-center justify-center w-12 h-12 border border-ink-0/15 bg-ink-0/[.03] mb-4">
          <Compass className="w-5 h-5 text-ink-3" />
        </div>
        <div className="font-mono text-[10px] text-ink-4 wide uppercase mb-2">— 404 —</div>
        <h1 className="font-sans text-[18px] font-semibold text-ink-0 tight">页面不存在</h1>
        <p className="font-sans text-[13px] text-ink-3 mt-2">这个地址没有被任何路由匹配。</p>
        <Link href="/" className="btn-ink h-8 px-4 mt-5 text-[12px] font-medium inline-flex items-center">返回视频库</Link>
      </div>
    </div>
  )
}
