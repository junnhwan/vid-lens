import { Icon } from '@/components/ui/Icon'

const LINES = [
  { t: '00:12', hit: false, text: '所以我们把检索范围限制在当前账号已建立的索引内…' },
  { t: '00:14', hit: true, text: '每条画面证据都保留来源与时间，可以逐条回放核对…' },
  { t: '00:21', hit: false, text: '接下来演示单视频、视频库和知识库三种提问范围的区别…' },
] as const

/**
 * 介绍页首屏的静态示意图:视频帧 + 转写片段 + 一次带引用的问答。
 * 纯静态(无动画、无绝对定位浮层),不会遮挡文案,也不复用登录页的放映机组件。
 */
export function IntroStage() {
  return (
    <div className="intro-stage" aria-hidden="true">
      <div className="is-card rv-item">
        <div className="is-frame">
          <span className="is-play">
            <Icon name="play" />
          </span>
          <span className="is-tc mono">00:14 / 42:08</span>
        </div>
        <div className="is-meta">
          <b>产品发布会 · 第 3 节</b>
          <span className="is-tags">
            <em className="is-tag">已转写</em>
            <em className="is-tag">已建索引</em>
          </span>
        </div>
      </div>

      <div className="is-lines rv-item">
        {LINES.map(l => (
          <p key={l.t} className={`is-line${l.hit ? ' hit' : ''}`}>
            <span className="is-t mono">{l.t}</span>
            {l.text}
          </p>
        ))}
      </div>

      <div className="is-qa rv-item">
        <p className="is-q">画面证据在这里的作用是什么？</p>
        <p className="is-a">它保留了来源与时间，每条引用都能跳回原画面核对。</p>
        <span className="is-cite mono">
          <Icon name="play" size="sm" />
          C1 · 00:14
        </span>
      </div>
    </div>
  )
}
