'use client'

import Link from 'next/link'
import { useCrumb } from '@/components/shell/AppShell'
import { Icon } from '@/components/ui/Icon'

// 阶段性占位:按 handoff 第六节顺序,后续阶段按原型逐页重写替换。
// 只承诺"还没重写",不假装功能存在。

export default function PhasePlaceholder({ title, phase }: { title: string; phase: string }) {
  useCrumb([title])
  return (
    <div className="page">
      <div className="card">
        <div className="empty">
          <Icon name="clock" size="lg" />
          <b>{title} · 按原型重写中</b>
          <p>该页面属于第 {phase} 阶段,将严格按 docs/prototype 的视觉与交互实现后上线。当前阶段已完成设计系统、应用外壳、工作台与视频库。</p>
          <Link href="/" className="btn btn-sm" style={{ marginTop: 8 }}>
            <Icon name="chev-l" size="sm" />
            返回工作台
          </Link>
        </div>
      </div>
    </div>
  )
}
