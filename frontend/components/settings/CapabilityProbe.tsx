import { useEffect, useRef, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import type { ProfilePurpose } from '@/lib/types'
import { Icon } from '@/components/ui/Icon'
import './CapabilityProbe.css'

export type ProbeTarget = {
  purpose: ProfilePurpose
  label: string
  model: string
  base_url: string
  api_key: string
  provider: string
  profile_id: number
  embedding_dim?: number
}

type ProbeStatus = 'queued' | 'running' | 'success' | 'failed'
type ProbeResult = { status: ProbeStatus; detail: string; ms?: number }

const statusLabels: Record<ProbeStatus | 'idle', string> = {
  idle: '未探测', queued: '等待中', running: '探测中', success: '可用', failed: '需要处理',
}

export function CapabilityProbe({ targets, disabled = false }: { targets: ProbeTarget[]; disabled?: boolean }) {
  const [results, setResults] = useState<Partial<Record<ProfilePurpose, ProbeResult>>>({})
  const [running, setRunning] = useState(false)
  const [hasRun, setHasRun] = useState(false)
  const generation = useRef(0)
  const inFlight = useRef(false)
  const mounted = useRef(true)
  const fingerprint = JSON.stringify(targets)

  useEffect(() => {
    const mountedRef = mounted
    const generationRef = generation
    mountedRef.current = true
    return () => { mountedRef.current = false; generationRef.current++ }
  }, [])

  // A draft edit invalidates old results. Wait for the current request to settle before another run.
  useEffect(() => {
    generation.current++
    setResults({})
    setHasRun(false)
  }, [fingerprint])

  const run = async () => {
    if (inFlight.current || disabled || targets.length === 0) return
    inFlight.current = true
    const runGeneration = generation.current
    setRunning(true)
    setHasRun(false)
    const initial: Partial<Record<ProfilePurpose, ProbeResult>> = {}
    targets.forEach(target => { initial[target.purpose] = { status: 'queued', detail: '等待前一项完成' } })
    setResults(initial)

    try {
      for (const target of targets) {
        if (runGeneration !== generation.current) return
        setResults(previous => ({ ...previous, [target.purpose]: { status: 'running', detail: '正在发送小样本请求…' } }))
        const start = performance.now()
        try {
          const response = await api.probeCapability(target)
          if (runGeneration !== generation.current) return
          const detail = target.purpose === 'embedding'
            ? `实际返回 ${response.dimension} 维${target.embedding_dim ? '，与配置一致' : ''}`
            : target.purpose === 'asr'
              ? '静音样本请求已接受；仍需用真实语音验收转写质量'
              : '测试请求成功'
          setResults(previous => ({ ...previous, [target.purpose]: { status: 'success', detail, ms: Math.round(performance.now() - start) } }))
        } catch (error) {
          if (runGeneration !== generation.current) return
          const message = error instanceof ApiError ? error.message : '网络或探测请求失败'
          const detail = target.purpose === 'asr'
            ? `${message}；静音样本可能被服务商拒绝，请用真实语音复核`
            : message
          setResults(previous => ({ ...previous, [target.purpose]: { status: 'failed', detail, ms: Math.round(performance.now() - start) } }))
        }
      }
      if (runGeneration === generation.current) setHasRun(true)
    } finally {
      inFlight.current = false
      if (mounted.current) setRunning(false)
    }
  }

  const completed = targets.filter(target => ['success', 'failed'].includes(results[target.purpose]?.status || '')).length
  const failed = targets.filter(target => results[target.purpose]?.status === 'failed').length
  const summary = running && completed === 0 && !Object.keys(results).length
    ? '配置已变更，正在等待先前的请求结束…'
    : running
      ? `${completed} / ${targets.length} 项完成 · 正在依次调用模型`
      : hasRun
        ? failed > 0
          ? `${completed - failed} 项可用 · ${failed} 项待处理`
          : `${completed} 项能力均可用 · 语音质量仍建议用真实片段复核`
        : '准备就绪 · 逐项检查所选模型的调用能力'

  return <section className="capability-probe" aria-label="模型可用性检查" aria-busy={running}>
    <div className="capability-probe__head">
      <div className="capability-probe__intro">
        <h4><Icon name="scan" size="sm" />模型可用性检查</h4>
        <p>依次发送小样本请求；保留各项状态、耗时和失败原因。</p>
      </div>
      <button type="button" className="btn btn-sm btn-primary capability-probe__run" disabled={disabled || running || targets.length === 0} onClick={() => void run()}>
        <Icon name="scan" size="sm" />{running ? '逐项探测中…' : hasRun ? '重新探测全部' : '开始能力检查'}
      </button>
    </div>
    <div className="capability-probe__progress" role="status" aria-live="polite" aria-atomic="true">
      <span>{summary}</span>
      <div className="capability-probe__segments" aria-hidden="true">
        {targets.map(target => <i key={target.purpose} className={results[target.purpose]?.status || 'idle'} />)}
      </div>
    </div>
    <ol className="capability-probe__list">
      {targets.map((target, index) => {
        const result = results[target.purpose]
        const status = result?.status || 'idle'
        return <li className={`capability-probe__row ${status}`} key={target.purpose}>
          <span className={`capability-probe__state ${status}`} aria-hidden="true">
            {status === 'success' ? <Icon name="check" size="sm" /> : status === 'failed' ? <Icon name="alert" size="sm" /> : status === 'running' ? <span className="capability-probe__pulse" /> : String(index + 1).padStart(2, '0')}
          </span>
          <span className="capability-probe__model"><b>{target.label}</b><small>{target.model || '未填写模型'}</small></span>
          <span className="capability-probe__detail">{result?.detail || '尚未发送样本请求'}</span>
          <span className="capability-probe__result"><b>{statusLabels[status]}</b>{result?.ms !== undefined && <small>{result.ms} ms</small>}</span>
        </li>
      })}
    </ol>
    <div className={`capability-probe__foot${hasRun && failed ? ' has-error' : ''}`}>
      <Icon name={hasRun && failed ? 'alert' : hasRun ? 'check' : 'info'} size="sm" />
      <span>{hasRun ? failed ? '检查已结束，请处理异常项后重新探测。' : '检查完成，所有小样本请求成功。' : '实际调用可能产生少量服务商费用；模型列表可读取不代表模型可调用。'}</span>
    </div>
  </section>
}
