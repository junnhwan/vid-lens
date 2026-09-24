'use client'

import { useEffect, useRef, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import type { ProfilePurpose } from '@/lib/types'

export type ProbeTarget = { purpose: ProfilePurpose; label: string; model: string; base_url: string; api_key: string; provider: string; profile_id: number; embedding_dim?: number }
type ProbeResult = { status: 'queued' | 'running' | 'success' | 'failed'; detail: string; ms?: number }

export function CapabilityProbe({ targets, disabled = false }: { targets: ProbeTarget[]; disabled?: boolean }) {
  const [results, setResults] = useState<Partial<Record<ProfilePurpose, ProbeResult>>>({})
  const [running, setRunning] = useState(false)
  const generation = useRef(0)
  const fingerprint = JSON.stringify(targets)
  useEffect(() => { generation.current++; setResults({}); setRunning(false) }, [fingerprint])

  const run = async () => {
    if (running || disabled) return
    const runGeneration = generation.current
    setRunning(true)
    const initial: Partial<Record<ProfilePurpose, ProbeResult>> = {}
    targets.forEach(target => { initial[target.purpose] = { status: 'queued', detail: '等待前一个模型完成' } })
    setResults(initial)
    for (const target of targets) {
      if (runGeneration !== generation.current) return
      setResults(previous => ({ ...previous, [target.purpose]: { status: 'running', detail: '正在调用所选模型' } }))
      const start = performance.now()
      try {
        const response = await api.probeCapability(target)
        if (runGeneration !== generation.current) return
        const detail = target.purpose === 'embedding' ? `实际返回 ${response.dimension} 维` : target.purpose === 'asr' ? '静音样本请求已被接受；请再用真实语音验收转写质量' : '测试请求成功'
        setResults(previous => ({ ...previous, [target.purpose]: { status: 'success', detail, ms: Math.round(performance.now() - start) } }))
      } catch (error) {
        if (runGeneration !== generation.current) return
        const message = error instanceof ApiError ? error.message : '网络或探测请求失败'
        setResults(previous => ({ ...previous, [target.purpose]: { status: 'failed', detail: target.purpose === 'asr' ? `${message}；静音样本可能被服务商拒绝，请用真实语音复核` : message, ms: Math.round(performance.now() - start) } }))
      }
    }
    if (runGeneration === generation.current) setRunning(false)
  }

  return <div style={{ marginTop: 16 }}>
    <button type="button" className="btn btn-sm" disabled={disabled || running || targets.length === 0} onClick={() => void run()}>{running ? '逐项探测中…' : '实际探测所选模型'}</button>
    <p style={{ fontSize: 12, color: 'var(--tx-3)', marginTop: 6 }}>依次向每个已配置模型发送小样本请求，可能产生少量服务商费用。拉取模型列表只验证列表接口，不代表模型可调用。</p>
    {targets.map(target => {
      const result = results[target.purpose]
      return <div key={target.purpose} style={{ fontSize: 13, padding: '7px 0', borderBottom: '1px solid var(--line)' }} aria-live="polite">
        <b>{target.label} · {target.model || '未填写模型'}</b> · {result?.status === 'queued' ? '排队中' : result?.status === 'running' ? '探测中' : result?.status === 'success' ? '成功' : result?.status === 'failed' ? '失败' : '未探测'}
        {result && <> · {result.detail}{result.ms !== undefined ? ` · ${result.ms}ms` : ''}</>}
      </div>
    })}
  </div>
}
