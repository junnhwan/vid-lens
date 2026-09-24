'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { Icon } from '@/components/ui/Icon'

const ITEMS = [
  { href: '/docs', label: '快速开始', icon: 'bolt', exact: true },
  { href: '/docs/features', label: '功能说明', icon: 'layers', exact: false },
  { href: '/docs/config', label: '配置与常见问题', icon: 'settings', exact: false },
  { href: '/docs/changelog', label: '更新日志', icon: 'clock', exact: false },
] as const

export function DocsNav() {
  const pathname = usePathname()
  return (
    <nav className="docs-side" aria-label="文档目录">
      <span className="docs-sep">使用文档</span>
      {ITEMS.map(item => {
        const active = item.exact ? pathname === item.href : pathname.startsWith(item.href)
        return (
          <Link key={item.href} href={item.href} className={`docs-nav-item${active ? ' active' : ''}`}>
            <Icon name={item.icon} size="sm" />
            {item.label}
          </Link>
        )
      })}
    </nav>
  )
}
