import Link from '@/lib/router'
import { Icon } from '@/components/ui/Icon'
import type { VideoTask } from '@/lib/types'
import { fmtRelTime, taskTitle } from '@/lib/format'

export function ProductHero({ onImport, current, resumeHref, resumeLabel, resumeUpdatedAt, loading }: { onImport: () => void; current?: VideoTask; resumeHref?: string; resumeLabel?: string; resumeUpdatedAt?: string; loading?: boolean }) {
  return <section className="product-hero">
    <div className="product-hero-copy">
      <p className="product-eyebrow">VIDLENS / STUDIO</p>
      <h1>{current ? '从上次的线索，继续理解。' : '让每段视频，成为可验证的知识。'}</h1>
      <p>{current ? '回到已保存的笔记段落或视频时间，接着核对与整理。' : '导入课程或技术教程，整理转写与画面证据，随时提问并回到原视频核对。'}</p>
      <div className="product-actions">{current && resumeHref && <Link className="btn btn-primary" href={resumeHref}><Icon name="play" />继续学习</Link>}<button className={current ? 'btn' : 'btn btn-primary'} onClick={onImport}><Icon name="plus" />导入视频</button><Link className="btn btn-ghost" href="/artifacts">查看成果<Icon name="chev-r" /></Link></div>
    </div>
    <div className="product-hero-art">
      <span className="studio-current-label">{current ? 'YOUR LAST STUDY POSITION' : 'YOUR WORKSPACE'}</span>
      {current ? <div><div className="studio-current-title">{taskTitle(current)}</div><p className="studio-current-copy">{resumeLabel || '已保存学习位置'} · {fmtRelTime(resumeUpdatedAt || current.updated_at)}更新</p><Link className="studio-current-link" href={resumeHref || `/video/${current.id}`}>回到学习位置 <Icon name="arrow-r" size="sm" /></Link></div> : <div className="studio-current-empty">{loading ? '正在读取学习位置…' : '还没有保存学习位置。打开视频播放或选择笔记段落后，这里会显示你的进度。'}</div>}
    </div>
  </section>
}
