'use client'

import Link from 'next/link'
import { ArrowRight, LayoutGrid, FlaskConical } from 'lucide-react'

/** 原型导航中枢：分清「设计预览」与「正式产品」 */
export default function PrototypeHubPage() {
  return (
    <div className="min-h-[100dvh] bg-paper-1 text-ink-0 proto-root">
      <div className="max-w-[1080px] mx-auto px-6 sm:px-8 pt-16 pb-28 lg:pt-20 grid lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)] gap-12 lg:gap-20">
        <header className="lg:pt-4">
          <p className="text-[12px] text-ink-4 mb-5">开发预览</p>
          <h1 className="text-[40px] sm:text-[48px] font-semibold tracking-tight leading-[1.08] text-ink-0 text-balance">
            映知
          </h1>
          <p className="text-[15px] text-ink-3 mt-3 italic leading-relaxed">观之以映，释之以知</p>
          <p className="text-[14px] text-ink-3 mt-6 max-w-[36ch] leading-relaxed">
            正式产品连真实后端。设计原型用 mock 数据看布局，不发请求。
          </p>
        </header>

        <div className="space-y-12">
          <HubGroup
            icon={<LayoutGrid className="w-3.5 h-3.5" />}
            title="正式产品"
            hint="日常开发走这里"
          >
            <HubLink href="/" label="视频库首页" desc="上传、管理、进入问答" />
            <HubLink href="/chat/101" label="单视频问答" desc="Agent / 严格 RAG / 普通模式" />
            <HubLink href="/kb" label="知识库" desc="跨视频问答" />
            <HubLink href="/settings" label="AI 配置" desc="BYOK 密钥" />
          </HubGroup>

          <HubGroup
            icon={<FlaskConical className="w-3.5 h-3.5" />}
            title="设计原型"
            hint="只看 UI"
          >
            <HubLink href="/prototype/dashboard" label="工作台原型" desc="视频库布局定稿预览" />
            <HubLink href="/prototype/agent-chat" label="Agent 问答原型" desc="融合版 / 极简版" primary />
            <HubLink href="/prototype/settings" label="设置页原型" desc="AI 配置布局" />
          </HubGroup>
        </div>
      </div>
    </div>
  )
}

function HubGroup({ icon, title, hint, children }: {
  icon: React.ReactNode; title: string; hint: string; children: React.ReactNode
}) {
  return (
    <section>
      <div className="flex items-baseline justify-between gap-4 mb-3">
        <h2 className="text-[13px] font-medium text-ink-1 flex items-center gap-2">
          <span className="text-sienna-600">{icon}</span>
          {title}
        </h2>
        <span className="text-[11px] text-ink-5">{hint}</span>
      </div>
      <div className="proto-hub-rows">
        {children}
      </div>
    </section>
  )
}

function HubLink({ href, label, desc, primary }: {
  href: string; label: string; desc: string; primary?: boolean
}) {
  return (
    <Link
      href={href}
      className="group flex items-center justify-between gap-4 py-3.5 transition-colors hover:bg-sienna-500/5"
    >
      <div className="min-w-0 pl-0.5">
        <div className={`text-[14px] font-medium truncate ${primary ? 'text-sienna-700' : 'text-ink-0'}`}>
          {label}
        </div>
        <div className="text-[12px] text-ink-4 mt-0.5">{desc}</div>
      </div>
      <ArrowRight className="w-4 h-4 text-ink-5 shrink-0 transition-transform duration-200 group-hover:translate-x-0.5 group-hover:text-sienna-600" />
    </Link>
  )
}
