'use client'

const CUES = [
  { t: '00:14', cite: 'C1', text: '他把第一性原理拆成可验证的三步。', kind: '口述' },
  { t: '01:02', cite: 'C2', text: '白板上出现了完整的调用链。', kind: '画面' },
  { t: '03:41', cite: 'C3', text: '结论被钉回这一帧,而不是整段转写。', kind: '引用' },
  { t: '07:18', cite: 'C1', text: '同一句话再次出现时,时间码没有漂。', kind: '口述' },
]

const TICKS = ['00:00', '02:30', '05:00', '07:30', '10:00']

export function ProjectorStage() {
  return (
    <div className="px-stage" aria-hidden="true">
      <div className="px-grain" />
      <div className="px-vignette" />
      <div className="px-beam" />
      <div className="px-sprocket px-sprocket-l" />
      <div className="px-sprocket px-sprocket-r" />

      <div className="px-reel">
        <i /><i /><i />
      </div>

      <div className="px-telecine">
        <div className="px-ticks">
          {TICKS.map(t => <span key={t} className="mono">{t}</span>)}
        </div>
        <div className="px-track">
          <span className="px-seg px-seg-a" />
          <span className="px-seg px-seg-b" />
          <span className="px-seg px-seg-c" />
          <span className="px-head" />
        </div>
        <div className="px-cues">
          {CUES.map((c, i) => (
            <div key={c.t} className="px-cue" style={{ animationDelay: `${0.4 + i * 0.55}s` }}>
              <span className="px-cue-t mono">{c.t}</span>
              <span className="px-cue-k">{c.kind}</span>
              <span className="px-cue-cite mono">{c.cite}</span>
              <span className="px-cue-tx">{c.text}</span>
            </div>
          ))}
        </div>
      </div>

      <div className="px-scan" />
    </div>
  )
}
