import { useEffect, useRef, useState, type ReactNode } from 'react'
import Link from '@/lib/router'
import { Icon } from '@/components/ui/Icon'
import { BrandMark } from '@/components/ui/BrandMark'
import { useTheme } from '@/components/theme/ThemeProvider'
import { useMediaQuery } from '@/components/ui/useMediaQuery'

export function ShellFrame({ children, pathname, crumb, user, onImport, onLogout, products = false, preview = false }: {
  children: ReactNode
  pathname: string
  crumb: { label: string; href?: string }[]
  user: { name: string; detail: string }
  onImport?: () => void
  onLogout?: () => void
  products?: boolean
  preview?: boolean
}) {
  const { theme, toggle } = useTheme()
  const [open, setOpen] = useState(false)
  const rail = useRef<HTMLElement>(null)
  const menu = useRef<HTMLButtonElement>(null)
  const main = useRef<HTMLDivElement>(null)
  const mobile = useMediaQuery('(max-width: 860px)')
  useEffect(() => {
    rail.current?.toggleAttribute('inert', mobile && !open)
    main.current?.toggleAttribute('inert', mobile && open)
  }, [mobile, open])
  useEffect(() => { setOpen(false) }, [pathname])
  useEffect(() => {
    if (!open) return
    const element = rail.current
    element?.querySelector<HTMLElement>('a,button')?.focus()
    const handle = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { setOpen(false); menu.current?.focus() }
      if (event.key !== 'Tab' || !element) return
      const controls = Array.from(element.querySelectorAll<HTMLElement>('a[href], button:not([disabled])')).filter(el => el.offsetParent !== null)
      const first = controls[0], last = controls[controls.length - 1]
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
      if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
    }
    window.addEventListener('keydown', handle)
    return () => window.removeEventListener('keydown', handle)
  }, [open])
  const nav = [
    { href: '/', label: '工作台', icon: 'home' as const, active: pathname === '/' },
    { href: '/library', label: '视频库', icon: 'video' as const, active: /^\/(library|video)/.test(pathname) },
    { href: '/kb', label: '知识库', icon: 'folder' as const, active: pathname.startsWith('/kb') },
    { href: '/chat', label: '问答', icon: 'message' as const, active: pathname.startsWith('/chat') },
  ]
  const productNav = [
    { href: '/artifacts', label: '成果', icon: 'layers' as const },
    { href: '/tasks', label: '任务', icon: 'activity' as const },
  ]
  const href = (path: string) => preview && (path === '/' || path === '/artifacts' || path === '/tasks') ? `/dev/product?view=${path === '/' ? 'home' : path.slice(1)}` : path
  return <div className="app">
    {open && <div className="rail-veil" onClick={() => { setOpen(false); menu.current?.focus() }} />}
    <aside ref={rail} id="rail" className={`rail${open ? ' open' : ''}`} aria-label="主导航">
      <Link href={href('/')} className="brand"><BrandMark /><div><div className="brand-name">映知</div><div className="brand-sub">VIDLENS</div></div></Link>
      {nav.map(item => <Link key={item.href} href={href(item.href)} className={`nav-item${item.active ? ' active' : ''}`} aria-current={item.active ? 'page' : undefined}><Icon name={item.icon} />{item.label}</Link>)}
      {products && <div className="product-nav-group">{productNav.map(item => <Link key={item.href} href={href(item.href)} className={`nav-item${pathname.startsWith(item.href) ? ' active' : ''}`} aria-current={pathname.startsWith(item.href) ? 'page' : undefined}><Icon name={item.icon} />{item.label}</Link>)}</div>}
      <div className="rail-spacer" />
      <button className="nav-item" onClick={toggle} aria-label={theme === 'dark' ? '切换到浅色' : '切换到深色'}><Icon name={theme === 'dark' ? 'sun' : 'moon'} />{theme === 'dark' ? '浅色' : '深色'}</button>
      <Link href="/settings" className={`nav-item${pathname.startsWith('/settings') ? ' active' : ''}`}><Icon name="settings" />设置</Link>
      <Link href="/docs" className="nav-item"><Icon name="file" />文档</Link>
      <Link href="/settings" className="rail-user"><span className="avatar">{user.name.trim().charAt(0).toUpperCase() || '·'}</span><span className="who"><b>{user.name}</b><span>{user.detail}</span></span></Link>
      {onLogout && <button className="nav-item" onClick={onLogout}><Icon name="logout" />退出登录</button>}
    </aside>
    <div ref={main} className="main">
      <header className="topbar">
        <button ref={menu} className="topbar-menu" onClick={() => setOpen(!open)} aria-label={open ? '关闭菜单' : '打开菜单'} aria-expanded={open} aria-controls="rail"><Icon name={open ? 'x' : 'menu'} /></button>
        <nav className="crumb" aria-label="面包屑">{crumb.map((item, i) => <span className="crumb-seg" key={`${i}-${item.label}`}>{i > 0 && <span className="div">/</span>}{i === crumb.length - 1 || !item.href ? <b>{item.label}</b> : <Link href={item.href}>{item.label}</Link>}</span>)}</nav>
        <div className="product-topbar-actions">{preview && <span className="product-preview-label">开发预览 · 契约样例</span>}{onImport && <button className="btn btn-sm" onClick={onImport}><Icon name="plus" size="sm" />导入视频</button>}</div>
      </header>
      <main className="content" id="content">{children}</main>
    </div>
  </div>
}
