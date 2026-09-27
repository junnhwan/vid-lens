import Link from '@/lib/router'
import { BrandMark } from '@/components/ui/BrandMark'
import { Icon } from '@/components/ui/Icon'
import { DocsNav } from './DocsNav'
import { DocsToc } from './DocsToc'
import './docs.css'


export default function DocsLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="docs-root">
      <header className="docs-top">
        <Link href="/intro" className="docs-brand" title="项目介绍">
          <BrandMark size={30} />
          <span>
            <b>映知 · 文档</b>
            <i>VIDLENS DOCS</i>
          </span>
        </Link>
        <nav className="docs-topnav">
          <Link href="/intro">项目介绍</Link>
          <Link href="/" className="docs-cta-sm">
            进入工作台
            <Icon name="arrow-r" size="sm" />
          </Link>
        </nav>
      </header>
      <div className="docs-body">
        <DocsNav />
        <main className="docs-main">
          <div className="docs-shell">
            {children}
            <DocsToc />
          </div>
        </main>
      </div>
    </div>
  )
}
