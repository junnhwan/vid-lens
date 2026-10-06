import Link from '@/lib/router'
import { Icon } from '@/components/ui/Icon'
import type { VideoTask } from '@/lib/types'
import { fmtRelTime, taskTitle } from '@/lib/format'
import { LearningPath } from './LearningPath'

export function ProductHero({ onImport, current, resumeHref, resumeLabel, resumeUpdatedAt, loading, firstRun = false }: { onImport: () => void; current?: VideoTask; resumeHref?: string; resumeLabel?: string; resumeUpdatedAt?: string; loading?: boolean; firstRun?: boolean }) {
  return <section className={`home-resume${firstRun ? ' home-first-run' : ''}`} aria-label={current ? '继续学习' : '开始学习'}>
    <div className="home-resume-main">
    <div className="home-resume-copy">
      <p className="home-resume-label">{current ? '继续学习' : firstRun ? '你的第一段学习旅程' : '工作台'}</p>
      <h1>{current ? taskTitle(current) : '从一段视频开始学习'}</h1>
      <p>{current ? `${resumeLabel || '已保存学习位置'} · ${fmtRelTime(resumeUpdatedAt || current.updated_at)}更新` : loading ? '正在读取学习位置…' : '导入视频，阅读内容、提问并整理学习笔记。'}</p>
    </div>
    <div className="product-actions">{current && resumeHref && <Link className="btn btn-primary" href={resumeHref}><Icon name="play" />继续学习</Link>}<button className={current ? 'btn' : 'btn btn-primary'} onClick={onImport}><Icon name="plus" />导入视频</button></div>
    </div>
    {firstRun && <>
      <div className="home-story" aria-hidden="true">
        <div className="home-story-screen"><span className="home-story-kicker">VIDLENS / PLAY · LEARN · KEEP</span><div className="home-story-play"><Icon name="play" /></div><div className="home-story-wave">{[24, 45, 66, 38, 80, 54, 30, 64, 42, 72, 48, 24].map((height, i) => <i key={i} style={{ height: `${height}%` }} />)}</div><div className="home-story-timeline"><i /><i /><i /><span /></div></div>
        <div className="home-story-note"><Icon name="file" /><span>理解，然后留下来</span><i /><i /><i /><div><b /><b /><b /></div></div>
        <svg className="home-story-link" viewBox="0 0 100 100" fill="none"><path d="M8 22 C60 22 40 78 92 78" /><circle cx="8" cy="22" r="3" /><circle cx="92" cy="78" r="3" /></svg>
      </div>
      <LearningPath />
    </>}
  </section>
}
