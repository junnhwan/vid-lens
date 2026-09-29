import Link from '@/lib/router'
import { Icon } from '@/components/ui/Icon'
import type { VideoTask } from '@/lib/types'
import { fmtRelTime, taskTitle } from '@/lib/format'

export function ProductHero({ onImport, current, resumeHref, resumeLabel, resumeUpdatedAt, loading }: { onImport: () => void; current?: VideoTask; resumeHref?: string; resumeLabel?: string; resumeUpdatedAt?: string; loading?: boolean }) {
  return <section className="home-resume" aria-label={current ? '继续学习' : '开始学习'}>
    <div className="home-resume-copy">
      <p className="home-resume-label">{current ? '继续学习' : '工作台'}</p>
      <h1>{current ? taskTitle(current) : '从一段视频开始学习'}</h1>
      <p>{current ? `${resumeLabel || '已保存学习位置'} · ${fmtRelTime(resumeUpdatedAt || current.updated_at)}更新` : loading ? '正在读取学习位置…' : '导入视频，阅读内容、提问并整理学习笔记。'}</p>
    </div>
    <div className="product-actions">{current && resumeHref && <Link className="btn btn-primary" href={resumeHref}><Icon name="play" />继续学习</Link>}<button className={current ? 'btn' : 'btn btn-primary'} onClick={onImport}><Icon name="plus" />导入视频</button></div>
  </section>
}
