'use client'

import { useEffect, useRef, useState } from 'react'
import { Icon } from '@/components/ui/Icon'

/* ============================================================
   样例数据全部来自本地部署真实视频
   task#2《用AI写代码：制作电子书阅读器全流程》28:25
   章节 / 转写段取自 video_transcription_chunks 的真实内容,
   画面为 02:00 / 10:00 / 16:00 三处真实视觉帧(public/intro-demo-frame-*.jpg)。
   ============================================================ */
const DUR = 1705 // 28:25

const CHAPTERS = [
  { t: 0, name: '开场' },
  { t: 100, name: '选择 Agent' },
  { t: 300, name: '环境搭建' },
  { t: 560, name: '产品设计 MVP' },
  { t: 980, name: '技术方案' },
  { t: 1260, name: '编码实现' },
] as const

const SEGS = [
  { t: 0, d: 100, text: 'AI 做出来的效果真的跟你预期的一样吗？' },
  { t: 100, d: 200, text: '主流选项有 Codex 和 Claude Code，建议直接选 Codex。' },
  { t: 300, d: 260, text: '先初始化 Git 仓库，再写 AGENTS.md 项目说明书。' },
  { t: 560, d: 240, text: '跟 AI 讨论 MVP：第一版只做最核心的功能。' },
  { t: 800, d: 180, text: '把共识整理成产品设计文档，作为开发底稿。' },
  { t: 980, d: 280, text: '技术方案：Electron + React + TypeScript。' },
  { t: 1260, d: 240, text: '让 Codex 按文档实现产品，写完自测。' },
  { t: 1500, d: 205, text: '实际运行验收，出错了就让 AI 修。' },
] as const

const FRAMES = [
  { t: 0, src: '/intro-demo-frame-a.jpg' },
  { t: 300, src: '/intro-demo-frame-b.jpg' },
  { t: 900, src: '/intro-demo-frame-c.jpg' },
] as const

const QUESTIONS = [
  { q: '为什么先只支持 TXT？', cite: 595, a: '目标是先跑通主流程：导入 TXT、纵向滚动、明亮主题；书签与分页留到后续迭代。' },
  { q: 'AGENTS.md 有什么用？', cite: 300, a: '它相当于项目说明书：每轮会话 Codex 都会优先读取，快速了解项目背景与开发规范。' },
  { q: '为什么让 AI 自己跑测试？', cite: 1260, a: 'AI 没法验证就会写出一堆 bug；给它可操作环境的插件后，它能自测并修到通过。' },
] as const

/** 压缩后的演示时间尺度：1 真实秒 ≈ 12 时间线秒（走完一片约 2.4 分钟） */
const SPEED = 12
const fmtClock = (s: number) =>
  `${String(Math.floor(s / 60)).padStart(2, '0')}:${String(Math.floor(s % 60)).padStart(2, '0')}`
const pctOf = (t: number) => (t / DUR) * 100
/**
 * 轨道内容定位:百分比相对 .is-track-body(避开左侧标签栏的可用区),
 * 播放头在 .is-tracks 全宽上,用同一公式换算,两者才能对齐。
 * 100% - gutter - 4px 对应 .is-track-body 的 left/right 内缩。
 */
const trackPct = (t: number) =>
  `calc(var(--lbl-gutter) + (100% - var(--lbl-gutter) - 4px) * ${(pctOf(t) / 100).toFixed(4)})`

/**
 * 介绍页首屏的交互示意图：监视器 + 章节/转写/证据三轨 + 可拖拽播放头 + 带引用的问答。
 * 融合:  G 剪辑台(可拖拽/可点击 seek) + F 自动循环演示 + D 微交互(发光、抬升)。
 * 降级:  无 JS / prefers-reduced-motion 时保持首帧与静态文案完整可见。
 */
export function IntroStage() {
  const [t, setT] = useState(560)
  const [playing, setPlaying] = useState(false)
  const [answer, setAnswer] = useState<{ q: string; a: string; cite: number } | null>(null)
  const timelineRef = useRef<HTMLDivElement>(null)
  const dragRef = useRef(false)
  const rafRef = useRef<number | null>(null)

  /* ---------- 自动走带（reduced-motion 下不启动） ---------- */
  useEffect(() => {
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
    setPlaying(true)
  }, [])

  useEffect(() => {
    if (!playing) return
    let last = performance.now()
    const step = (now: number) => {
      setT(prev => {
        const next = prev + ((now - last) / 1000) * SPEED
        return next >= DUR ? 0 : next
      })
      last = now
      rafRef.current = requestAnimationFrame(step)
    }
    rafRef.current = requestAnimationFrame(step)
    return () => {
      if (rafRef.current) cancelAnimationFrame(rafRef.current)
    }
  }, [playing])

  /* ---------- 派生状态 ---------- */
  let chapterIdx = 0
  CHAPTERS.forEach((c, i) => {
    if (t >= c.t) chapterIdx = i
  })
  let frameIdx = 0
  FRAMES.forEach((f, i) => {
    if (t >= f.t) frameIdx = i
  })
  const activeSeg = SEGS.findIndex(s => t >= s.t && t < s.t + s.d)

  /* ---------- 交互：点击 / 拖拽 seek ---------- */
  const seekFromEvent = (e: { clientX: number }) => {
    const el = timelineRef.current
    if (!el) return
    const box = el.getBoundingClientRect()
    if (box.width <= 0) return
    // 与 .is-track-body 同一套坐标:扣除左侧标签栏与右内边距,播放头才不会偏移
    const gutter = parseFloat(getComputedStyle(el).getPropertyValue('--lbl-gutter')) || 0
    const usable = Math.max(1, box.width - gutter - 4)
    const ratio = Math.min(1, Math.max(0, (e.clientX - box.left - gutter) / usable))
    setT(ratio * (DUR - 1))
  }
  const onPointerDown = (e: React.PointerEvent) => {
    dragRef.current = true
    setPlaying(false)
    seekFromEvent(e)
  }
  useEffect(() => {
    const move = (e: PointerEvent) => {
      if (dragRef.current) seekFromEvent(e)
    }
    const up = () => {
      dragRef.current = false
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
    return () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
    }
  }, [])

  /* ---------- 窄 chip 隐藏文字：放不下就不显示,而不是"开…"式残字 ----------
     时间线本来就是块状外观;完整名字在 title tooltip 与下方转写行里。 */
  useEffect(() => {
    const root = timelineRef.current
    if (!root) return
    const update = () => {
      for (const btn of Array.from(root.querySelectorAll<HTMLElement>('.is-chap, .is-seg'))) {
        // .is-seg 的文字在内层 span 里且 span 自带裁剪,要量内层才量得出溢出
        const inner = (btn.firstElementChild as HTMLElement | null) ?? btn
        btn.classList.toggle('tight', inner.scrollWidth > inner.clientWidth + 1)
      }
    }
    update()
    const ro = new ResizeObserver(update)
    ro.observe(root)
    return () => ro.disconnect()
  }, [])

  /* ---------- 提问：答案 + 引用滑动跳转 ---------- */
  const ask = (item: (typeof QUESTIONS)[number]) => {
    setAnswer({ q: item.q, a: item.a, cite: item.cite })
    // 先停掉自动走带,否则两个 raf 同时 setT,播放头会抖动
    setPlaying(false)
    const from = t
    const start = performance.now()
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      setT(item.cite)
      return
    }
    const glide = (now: number) => {
      const p = Math.min(1, (now - start) / 800)
      setT(from + (item.cite - from) * (1 - Math.pow(1 - p, 3)))
      if (p < 1) requestAnimationFrame(glide)
    }
    requestAnimationFrame(glide)
  }

  return (
    <div className="intro-stage" role="group" aria-label="产品交互演示：真实视频的时间线与问答">
      <div className="is-card rv-item">
        <div className="is-frame">
          {FRAMES.map((f, i) => (
            /* eslint-disable-next-line @next/next/no-img-element */
            <img key={f.src} src={f.src} alt="" className={i === frameIdx ? 'on' : ''} />
          ))}
          <span className="is-sheen" />
          <span className="is-live mono">真实数据</span>
          <button
            type="button"
            className="is-play"
            aria-label={playing ? '暂停时间线演示' : '播放时间线演示'}
            onClick={() => setPlaying(p => !p)}
          >
            <Icon name={playing ? 'pause' : 'play'} />
          </button>
          <span className="is-tc mono">{fmtClock(t)} / 28:25</span>
          <span className="is-chapter">
            章节 · <b>{CHAPTERS[chapterIdx].name}</b>
          </span>
        </div>
      </div>

      {/* ---- 三轨时间线 + 播放头 ---- */}
      <div className="is-deck rv-item">
        <div className="is-tracks" ref={timelineRef} onPointerDown={onPointerDown}>
          <div className="is-track is-track-chap">
            <span className="is-track-lbl mono">CHAPTERS</span>
            <div className="is-track-body">
              {CHAPTERS.map((c, i) => {
                const end = i < CHAPTERS.length - 1 ? CHAPTERS[i + 1].t : DUR
                return (
                  <button
                    type="button"
                    key={c.name}
                    className={`is-chap${i === chapterIdx ? ' active' : ''}`}
                    style={{ left: `${pctOf(c.t)}%`, width: `calc(${pctOf(end - c.t)}% - 3px)` }}
                    onClick={() => {
                      setT(c.t + 1)
                      setPlaying(false)
                    }}
                    title={`跳到 ${fmtClock(c.t)} · ${c.name}`}
                  >
                    {c.name}
                  </button>
                )
              })}
            </div>
          </div>
          <div className="is-track is-track-seg">
            <span className="is-track-lbl mono">TRANSCRIPT</span>
            <div className="is-track-body">
              {SEGS.map((s, i) => (
                <button
                  type="button"
                  key={s.t}
                  className={`is-seg${i === activeSeg ? ' active' : ''}`}
                  style={{ left: `${pctOf(s.t)}%`, width: `calc(${pctOf(s.d)}% - 3px)` }}
                  onClick={() => {
                    setT(s.t + 1)
                    setPlaying(false)
                  }}
                  title={`${fmtClock(s.t)} · ${s.text}`}
                >
                  <span>{s.text}</span>
                </button>
              ))}
            </div>
          </div>
          <div className="is-track is-track-ev">
            <span className="is-track-lbl mono">EVIDENCE</span>
            <div className="is-track-body">
              {FRAMES.map((f, i) => (
                <button
                  type="button"
                  key={f.src}
                  className={`is-thumb${i === frameIdx ? ' active' : ''}`}
                  style={{
                    /* clamp 防止左右两端被轨道 overflow:hidden 裁掉半个缩略图 */
                    left: `clamp(33px, ${pctOf(f.t)}%, calc(100% - 33px))`,
                  }}
                  onClick={() => {
                    setT(f.t + 1)
                    setPlaying(false)
                  }}
                  title={`跳到画面证据 ${fmtClock(f.t)}`}
                >
                  {/* eslint-disable-next-line @next/next/no-img-element */}
                  <img src={f.src} alt="" loading="lazy" />
                </button>
              ))}
            </div>
          </div>
          <span className="is-playhead" style={{ left: trackPct(t) }} />
        </div>
      </div>

      {/* ---- 与播放头联动的转写原文 ---- */}
      <div className="is-lines rv-item">
        <p className={`is-line${activeSeg >= 0 ? ' hit' : ''}`}>
          <span className="is-t mono">{fmtClock(activeSeg >= 0 ? SEGS[activeSeg].t : 0)}</span>
          {activeSeg >= 0 ? SEGS[activeSeg].text : SEGS[0].text}
        </p>
      </div>

      {/* ---- 问答 ---- */}
      <div className="is-qa rv-item">
        <div className="is-chips">
          {QUESTIONS.map(item => (
            <button
              type="button"
              key={item.q}
              className="is-chip"
              onClick={() => ask(item)}
            >
              {item.q}
            </button>
          ))}
        </div>
        <p className="is-q">{answer?.q ?? '「AI 做出来的效果，怎么才能保证符合预期？」'}</p>
        <p className="is-a">{answer?.a ?? '先与 AI 讨论边界、写成产品文档，再让它按文档实现并自测 —— 每一步都能回溯查看。'}</p>
        {answer ? (
          <button
            type="button"
            className="is-cite mono"
            onClick={() => {
              setPlaying(false)
              setT(answer.cite)
            }}
          >
            <Icon name="play" size="sm" />
            引用 · {fmtClock(answer.cite)}
          </button>
        ) : (
          <span className="is-cite mono is-cite-idle">
            <Icon name="play" size="sm" />
            引用 · 09:55
          </span>
        )}
      </div>
    </div>
  )
}
