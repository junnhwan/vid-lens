import Link from '@/lib/router'
import { Icon } from '@/components/ui/Icon'
import type { VideoTask } from '@/lib/types'
import { fmtRelTime, taskTitle } from '@/lib/format'

export function ProductHero({ onImport, current, loading }: { onImport: () => void; current?: VideoTask; loading?: boolean }) {
  return <section className="product-hero">
    <div className="product-hero-copy">
      <p className="product-eyebrow">VIDLENS / STUDIO</p>
      <h1>{current ? '从上次的线索，继续理解。' : '让每段视频，成为可验证的知识。'}</h1>
      <p>{current ? '打开最近的视频，沿着转写和画面回看来源；把关键理解整理成可编辑的笔记。' : '导入课程或技术教程，整理转写与画面证据，随时提问并回到原视频核对。'}</p>
      <div className="product-actions">{current && <Link className="btn btn-primary" href={`/video/${current.id}`}><Icon name="play" />继续学习</Link>}<button className={current ? 'btn' : 'btn btn-primary'} onClick={onImport}><Icon name="plus" />导入视频</button><Link className="btn btn-ghost" href="/artifacts">查看成果<Icon name="chev-r" /></Link></div>
    </div>
    <div className="product-hero-art">
      <span className="studio-current-label">{current ? 'RECENTLY IN YOUR LIBRARY' : 'YOUR WORKSPACE'}</span>
      {current ? <div><div className="studio-current-title">{taskTitle(current)}</div><p className="studio-current-copy">{fmtRelTime(current.updated_at)}更新 · 已有可用转写</p><Link className="studio-current-link" href={`/video/${current.id}`}>打开视频工作台 <Icon name="arrow-r" size="sm" /></Link></div> : <div className="studio-current-empty">{loading ? '正在读取最近的视频…' : '还没有可继续学习的视频。导入后，这里会显示你最近的一段资料。'}</div>}
    </div>
  </section>
}
