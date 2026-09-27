import { Icon, type IconName } from './Icon'

/** 统一加载态。card 变体带卡片壳；bare 为居中灰字。 */
export function LoadingBlock({ label = '加载中…', variant = 'bare' }: { label?: string; variant?: 'bare' | 'card' }) {
  return (
    <div className={`empty${variant === 'card' ? ' card' : ''}`} role="status">
      <b>{label}</b>
    </div>
  )
}

/** 统一错误态：alert 图标 + 消息 + 可选重试。 */
export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="empty card" role="alert">
      <Icon name="alert" size="lg" />
      <b>{message}</b>
      {onRetry && (
        <button className="btn btn-sm" style={{ marginTop: 10 }} onClick={onRetry}>
          <Icon name="refresh" size="sm" />重试
        </button>
      )}
    </div>
  )
}

/** 统一空态壳：card=带卡片，bare=裸居中。rail 面板同用 bare。 */
export function EmptyState({ icon, title, desc, action, variant = 'card' }: {
  icon?: IconName
  title?: string
  desc?: string
  action?: React.ReactNode
  variant?: 'card' | 'bare'
}) {
  return (
    <div className={`empty${variant === 'card' ? ' card' : ''}`}>
      {icon && <Icon name={icon} size="lg" />}
      {title && <b>{title}</b>}
      {desc && <p>{desc}</p>}
      {action}
    </div>
  )
}

/** 视频/知识库卡网格骨架。 */
export function CardSkeleton({ count = 4, gridClass = 'video-grid' }: { count?: number; gridClass?: string }) {
  return (
    <div className={gridClass} role="status" aria-label="内容加载中">
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className="card">
          <div className="skel" style={{ aspectRatio: '16/9', borderRadius: 'var(--r-card) var(--r-card) 0 0' }} />
          <div style={{ padding: '13px 15px 15px', display: 'grid', gap: 9 }}>
            <div className="skel" style={{ height: 14, width: '68%' }} />
            <div className="skel" style={{ height: 11, width: '42%' }} />
          </div>
        </div>
      ))}
    </div>
  )
}

/** 保留各页面内容形状的真实请求加载态；不会延长请求或模拟进度。 */
export function ProductSkeleton({ kind, count = 3 }: { kind: 'rows' | 'artifacts' | 'tasks' | 'article'; count?: number }) {
  if (kind === 'article') return <div className="product-skeleton-article" role="status" aria-label="正在读取笔记">
    <div className="product-skeleton-paper"><span className="skel title" /><span className="skel lead" /><span className="skel line" /><span className="skel line" /><span className="skel line short" /><span className="skel heading" /><span className="skel line" /><span className="skel line short" /></div>
    <div className="product-skeleton-evidence"><span className="skel heading" /><span className="skel media" /><span className="skel line" /><span className="skel line short" /></div>
  </div>
  return <div className={'product-skeleton-' + kind} role="status" aria-label="内容加载中">
    {Array.from({ length: count }, (_, index) => <div className="product-skeleton-item" key={index}>
      {kind === 'artifacts' && <span className="skel cover" />}
      {kind === 'tasks' && <span className="skel icon" />}
      <div className="product-skeleton-lines"><span className="skel heading" /><span className="skel line" /><span className="skel line short" /></div>
    </div>)}
  </div>
}
