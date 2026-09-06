'use client'

import './stage.css'

const WAVE = [28, 62, 88, 44, 96, 58, 80, 36, 72, 90, 40, 84, 52, 76, 34, 68]

export function ProjectorStage() {
  return (
    <div className="px-stage" aria-hidden="true">
      <div className="px-grain" />
      <div className="px-vignette" />
      <div className="px-beam" />

      <div className="px-story">
        <div className="px-row">
          <div className="px-gate">
            <div className="px-sprockets" />
            <div className="px-picture">
              <div className="px-file">
                <span className="px-play" />
              </div>
              <div className="px-scene" />
              <i className="px-ocr a" />
              <i className="px-ocr b" />
              <i className="px-ocr c" />
              <div className="px-scanline" />
              <span className="px-badge mono">C1</span>
              <span className="px-tc mono">00:14</span>
            </div>
            <div className="px-sprockets" />
          </div>

          <div className="px-side">
            <div className="px-wave">
              {WAVE.map((h, i) => (
                <i key={i} style={{ '--h': `${h}%`, animationDelay: `${i * 0.05}s` } as React.CSSProperties} />
              ))}
            </div>
            <div className="px-graph">
              <span className="n n1" />
              <span className="n n2" />
              <span className="n n3" />
              <span className="n n4" />
              <span className="n n5" />
              <i className="e e1" />
              <i className="e e2" />
              <i className="e e3" />
              <i className="e e4" />
            </div>
          </div>
        </div>

        <div className="px-timeline">
          <span className="px-seg s1" />
          <span className="px-seg s2" />
          <span className="px-seg s3" />
          <span className="px-pin p1 mono">C1</span>
          <span className="px-pin p2 mono">C2</span>
          <span className="px-head" />
        </div>

        <div className="px-frames">
          <div className="px-tile t1"><i /><b /></div>
          <div className="px-tile t2"><i /><b /></div>
          <div className="px-tile t3"><i /><b /></div>
          <div className="px-tile t4"><i /><b /></div>
        </div>

        <div className="px-beats">
          <i className="d1" />
          <i className="d2" />
          <i className="d3" />
          <i className="d4" />
        </div>
      </div>
    </div>
  )
}
