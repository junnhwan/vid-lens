import Link from 'next/link'

// 全局 404:匹配不到任何路由时显示。
export default function NotFound() {
  return (
    <div style={{ minHeight: '100dvh', display: 'grid', placeItems: 'center', padding: 24 }}>
      <div style={{ textAlign: 'center' }}>
        <div className="mono" style={{ fontSize: 10, color: 'var(--tx-4)', letterSpacing: '0.14em', marginBottom: 8 }}>— 404 —</div>
        <b style={{ fontSize: 17 }}>页面不存在</b>
        <p style={{ fontSize: 13, color: 'var(--tx-3)', marginTop: 8 }}>这个地址没有被任何路由匹配。</p>
        <Link href="/" className="btn" style={{ marginTop: 20, display: 'inline-flex' }}>返回工作台</Link>
      </div>
    </div>
  )
}
