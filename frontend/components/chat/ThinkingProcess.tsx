'use client'

import { useEffect, useState } from 'react'
import type { ChatMsg } from './chatUtils'
import type { ChatTraceStep } from './traceTypes'
import styles from './ThinkingProcess.module.css'

const statusText = { pending: '等待', running: '进行中', done: '完成', error: '失败', cancelled: '已停止' }

export function ThinkingProcess({ message }: { message: ChatMsg }) {
  const [expanded, setExpanded] = useState<boolean | null>(null)
  const [now, setNow] = useState(Date.now())
  const steps = message.trace ?? []
  const live = !!message.streaming
  useEffect(() => {
    if (!live) return
    setNow(Date.now())
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [live])
  if (!live && !steps.length && !Object.keys(message.reasoning ?? {}).length) return null
  const open = expanded ?? live
  const active = steps.findLast(step => step.status === 'running')
  const duration = message.processStartedAt
    ? Math.max(0, Math.floor(((message.processFinishedAt ?? now) - message.processStartedAt) / 1000))
    : undefined
  const summary = live ? active?.label ?? '正在连接…' : message.error ? '本轮未完成' : message.cancelled ? '已停止' : message.degraded ? '已结束 · 有限结果' : `已完成 ${steps.length} 个步骤`
  const attached = new Set(steps.map(step => step.kind === 'answer' ? 'answer' : step.id))
  return (
    <section className={styles.process} aria-label="思考与执行过程">
      <button type="button" className={styles.toggle} aria-expanded={open} onClick={() => setExpanded(!open)}>
        <span className={`${styles.indicator} ${live ? styles.live : ''}`} aria-hidden="true" />
        <span className={styles.title}>思考与执行过程</span>
        <span className={styles.summary}>{summary}{duration !== undefined ? ` · ${duration}秒` : ''}</span>
        <span aria-hidden="true">{open ? '⌃' : '⌄'}</span>
      </button>
      {open && <div className={styles.body}>
        {!steps.length && <p className={styles.waiting}>请求已发送，等待服务端开始处理。</p>}
        <ol className={styles.timeline}>
          {steps.map(step => <ProcessStep key={step.id} step={step} reasoning={message.reasoning?.[step.kind === 'answer' ? 'answer' : step.id]} now={now} />)}
        </ol>
        {Object.entries(message.reasoning ?? {}).filter(([id]) => !attached.has(id)).map(([id, text]) => <Reasoning key={id} text={text} />)}
        {message.error && <p className={styles.error}>{message.error}</p>}
        {message.cancelled && <p className={styles.waiting}>已保留收到的部分内容，本轮未确认保存。</p>}
      </div>}
    </section>
  )
}

function ProcessStep({ step, reasoning, now }: { step: ChatTraceStep; reasoning?: string; now: number }) {
  const duration = step.status === 'running' && step.startedAt ? Math.max(0, now - Date.parse(step.startedAt)) : step.durationMs
  return <li className={`${styles.step} ${styles[step.status]}`}>
    <span className={styles.dot} aria-hidden="true">{step.status === 'done' ? '✓' : step.status === 'error' ? '!' : step.status === 'cancelled' ? '−' : ''}</span>
    <div className={styles.stepContent}>
      <div className={styles.stepHeading}><strong>{step.label}</strong><span>{statusText[step.status]}{duration !== undefined ? ` · ${(duration / 1000).toFixed(1)}秒` : ''}</span></div>
      {step.detail && <p className={styles.detail}>{step.detail}</p>}
      {step.replan && <span className={styles.badge}>调整检索策略</span>}
      {!!step.evidenceRefs?.length && <span className={styles.badge}>参考已有 {step.evidenceRefs.length} 条证据</span>}
      {step.query && <p className={styles.detail}>检索：{step.query}</p>}
      {step.tool && step.kind !== 'plan' && <details className={styles.tool}><summary>工具详情</summary><code>{step.tool}</code>{step.toolInput && <pre>{step.toolInput}</pre>}{step.toolOutput && <p>{step.toolOutput}</p>}</details>}
      {reasoning && <Reasoning text={reasoning} />}
    </div>
  </li>
}

function Reasoning({ text }: { text: string }) {
  return <details className={styles.reasoning} open>
    <summary>模型思考 · 由模型接口提供</summary>
    <div>{text}</div>
    {text.length >= 64000 && <small>思考内容较长，仅展示前 64,000 个字符。</small>}
  </details>
}
