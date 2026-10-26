import type { ReactNode } from "react"

export function ArticleHeader({ title, meta, sourceUrl, onDetails, detailsLabel = "生成信息与过程", children }: {
  title: string
  meta: string
  sourceUrl?: string
  onDetails?: () => void
  detailsLabel?: string
  children?: ReactNode
}) {
  return <header className="article-head reading-head">
    <div className="article-source-row">
      {sourceUrl ? <a className="article-source" href={sourceUrl} target="_blank" rel="noopener noreferrer" aria-label={`${meta}，打开原视频`}>
        {meta}<span aria-hidden="true">↗</span>
      </a> : <p className="article-source">{meta}</p>}
      {onDetails ? <button type="button" className="reading-details-trigger" aria-haspopup="dialog" onClick={onDetails}>{detailsLabel}<span aria-hidden="true">↗</span></button> : null}
    </div>
    <h1 id="article-top" tabIndex={-1}>{title}</h1>
    {children ? <div className="article-tools reading-tools">{children}</div> : null}
  </header>
}
