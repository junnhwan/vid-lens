'use client'

import { useMemo, useState } from 'react'
import type { CiteRef } from '@/components/Citation'
import type { AgentClaim, AgentEvidenceArtifact, EvidenceLedgerView } from '@/lib/types'
import { api } from '@/lib/api'
import { ModalityTag } from '@/components/ui/ModalityTag'
import { Icon, type IconName } from '@/components/ui/Icon'
import { useToast } from '@/components/Toast'
import { DrawerVeil } from '@/components/ui/Modal'

// 证据账本:GET /agent/evidence-ledgers/:run_id 的展示层。
// claim 状态色、置信、反例检索 counter_query、像素核验结果、关联证据跳证据抽屉,
// 以及人工更正入口(POST corrections,追加新修订,不覆盖历史)。
// 右栏「证据账本」tab 与账本抽屉共用同一组 claim 卡。

export const CLAIM_STATUS_VIEW: Record<string, { chip: string; icon: IconName; text: string }> = {
  verified: { chip: 'chip-ok', icon: 'shield-check', text: '已验证' },
  uncertain: { chip: 'chip-warn', icon: 'alert', text: '不确定' },
  unsupported: { chip: 'chip-bad', icon: 'x', text: '无证据支撑' },
  corrected: { chip: 'chip-info', icon: 'pencil', text: '已更正' },
  hypothesized: { chip: 'chip-mute', icon: 'clock', text: '待核' },
}

const INSPECT_RESULT_VIEW: Record<string, { chip: string; text: string }> = {
  support: { chip: 'chip-ok', text: '支持' },
  contradict: { chip: 'chip-bad', text: '矛盾' },
  insufficient: { chip: 'chip-warn', text: '证据不足' },
}

const RELATION_TEXT: Record<string, string> = {
  support: '支持',
  contradict: '矛盾',
  insufficient: '证据不足',
  supports: '支撑',
  contradicts: '矛盾',
  context: '背景',
}

function statusView(status?: string) {
  return CLAIM_STATUS_VIEW[status || ''] || CLAIM_STATUS_VIEW.hypothesized
}

/** 更正会追加更高修订号的新行:按 root_claim_id 分组,只展示最新修订。
    后端 nil 切片序列化为 null,入参需容忍空值 */
export function latestClaimsByRoot(claims: AgentClaim[] | null | undefined): AgentClaim[] {
  const byRoot = new Map<string, AgentClaim>()
  for (const claim of claims ?? []) {
    const key = claim.root_claim_id || claim.id
    const current = byRoot.get(key)
    if (!current || claim.revision > current.revision) byRoot.set(key, claim)
  }
  return [...byRoot.values()]
}

function locatorInfo(evidence: AgentEvidenceArtifact): { taskId?: number; chunkIndex?: number } {
  try {
    const loc = JSON.parse(evidence.stable_locator) as { task_id?: number; chunk_index?: number }
    return { taskId: loc.task_id, chunkIndex: loc.chunk_index }
  } catch {
    return {}
  }
}

/** 账本证据 → 聊天引用:优先 evidence_id 匹配,退化到 stable_locator 的 task_id + chunk_index */
function resolveCite(evidence: AgentEvidenceArtifact, cites: CiteRef[]): CiteRef | undefined {
  if (evidence.source_ref) {
    const byEvidenceId = cites.find(c => c.evidenceId && c.evidenceId === evidence.source_ref)
    if (byEvidenceId) return byEvidenceId
  }
  const { taskId, chunkIndex } = locatorInfo(evidence)
  if (taskId != null && chunkIndex != null) {
    return cites.find(c => c.taskId === taskId && c.chunkIndex === chunkIndex)
  }
  return undefined
}

function clip(text: string | undefined, max = 44): string {
  const value = (text || '').trim()
  if (!value) return ''
  return value.length > max ? `${value.slice(0, max)}…` : value
}

function formatConfidence(confidence: number): string {
  return `置信 ${confidence.toFixed(2)}`
}

export interface LedgerClaimsProps {
  view: EvidenceLedgerView
  /** 当前消息的引用列表:账本证据据此解析出可跳转的 C# 引用 */
  cites: CiteRef[]
  onOpenEvidence: (cite: CiteRef, cites: CiteRef[]) => void
  /** 人工更正提交成功后触发(宿主据此刷新账本) */
  onCorrected?: () => void
}

export function LedgerClaims({ view, cites, onOpenEvidence, onCorrected }: LedgerClaimsProps) {
  const claims = useMemo(() => latestClaimsByRoot(view.claims ?? []), [view.claims])
  if (claims.length === 0) {
    return (
      <div className="rail-empty" style={{ paddingTop: 30 }}>
        <p>这次运行没有可展示的 claim。</p>
      </div>
    )
  }
  return (
    <>
      {claims.map(claim => (
        <ClaimCard
          key={claim.id}
          claim={claim}
          view={view}
          cites={cites}
          onOpenEvidence={onOpenEvidence}
          onCorrected={onCorrected}
        />
      ))}
    </>
  )
}

interface ClaimCardProps {
  claim: AgentClaim
  view: EvidenceLedgerView
  cites: CiteRef[]
  onOpenEvidence: (cite: CiteRef, cites: CiteRef[]) => void
  onCorrected?: () => void
}

function ClaimCard({ claim, view, cites, onOpenEvidence, onCorrected }: ClaimCardProps) {
  const toast = useToast()
  const [correcting, setCorrecting] = useState(false)
  const [text, setText] = useState('')
  const [reason, setReason] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const status = statusView(claim.status)
  const inspection = claim.inspection
  const inspectResult = inspection ? INSPECT_RESULT_VIEW[inspection.result] || INSPECT_RESULT_VIEW.insufficient : null

  const rows = useMemo(() => (
    (view.claim_evidence ?? [])
      .filter(link => link.claim_id === claim.id)
      .map(link => {
        const evidence = (view.evidence ?? []).find(e => e.id === link.evidence_id)
        return { link, evidence, cite: evidence ? resolveCite(evidence, cites) : undefined }
      })
      .filter(row => row.evidence || row.cite)
  ), [view, claim.id, cites])

  const submitCorrection = async () => {
    if (submitting) return
    if (!text.trim() || !reason.trim()) {
      toast.info('更正内容与更正原因都需要填写')
      return
    }
    setSubmitting(true)
    try {
      await api.correctEvidenceClaim(claim.id, { text: text.trim(), reason: reason.trim() })
      toast.success('已追加人工更正,生成新的 Claim 修订')
      setCorrecting(false)
      setText('')
      setReason('')
      onCorrected?.()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : '提交更正失败')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="claim-card">
      <div className="claim-top">
        <span className={`chip ${status.chip}`}>
          <Icon name={status.icon} size="sm" />{status.text}
        </span>
        {claim.revision > 1 && (
          <span className="mono" style={{ fontSize: 10, color: 'var(--tx-4)' }}>rev {claim.revision}</span>
        )}
        {claim.confidence > 0 && (
          <span className="mono" style={{ marginLeft: 'auto', fontSize: 10, color: 'var(--tx-4)' }}>
            {formatConfidence(claim.confidence)}
          </span>
        )}
      </div>
      <div className="claim-text">{claim.text}</div>
      {rows.length > 0 && (
        <div className="claim-evis">
          {rows.map(({ link, evidence, cite }) => (
            <div
              key={`${link.evidence_id}`}
              className="claim-ev"
              title={cite ? '查看证据详情' : undefined}
              style={cite ? undefined : { cursor: 'default' }}
              onClick={() => { if (cite) onOpenEvidence(cite, cites) }}
            >
              <span className="eid mono">{cite ? cite.id : clip(evidence?.source_ref, 14) || '证据'}</span>
              <span style={{ flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {clip(evidence?.quote_text || cite?.anchorQuote || cite?.content, 44) || clip(link.validation_reason, 44)}
              </span>
              {(evidence?.source_type && evidence.source_type !== 'rag_chunk') && (
                <ModalityTag modality={evidence.source_type} />
              )}
              <span
                className="mono"
                style={{ fontSize: 9.5, color: link.relation === 'contradicts' ? 'var(--bad)' : 'var(--tx-4)', flex: 'none' }}
                title={link.validation_reason}
              >
                {RELATION_TEXT[link.relation] || link.relation}
              </span>
            </div>
          ))}
        </div>
      )}
      {claim.validation_note && (
        <div className="claim-note">
          <Icon name="info" size="sm" />
          <span>{claim.validation_note}</span>
        </div>
      )}
      {inspection && (
        <>
          <div className="inspect-line mono">
            {inspection.version || 'claim-inspector'}
            <span className={`chip ${inspectResult?.chip || 'chip-mute'}`} style={{ height: 16, fontSize: 9.5, padding: '0 6px' }}>
              {inspectResult?.text || inspection.result}
            </span>
            · 反例检索{inspection.search_completed ? '已完成' : '未完成'}
          </div>
          {inspection.counter_query && (
            <div className="claim-note" style={{ marginTop: 6 }}>
              <Icon name="search" size="sm" />
              <span className="mono" style={{ fontSize: 10.5 }}>counter_query: {inspection.counter_query}</span>
            </div>
          )}
          {(inspection.evidence ?? []).some(e => e.pixel_checked || e.pixel_required) && (
            (inspection.evidence ?? []).filter(e => e.pixel_checked || e.pixel_required).map((e, i) => (
              <div key={`${e.source_ref}-${i}`} className="claim-note" style={{ marginTop: 6 }}>
                <Icon name="eye" size="sm" />
                <span>
                  像素核验:{e.pixel_checked
                    ? `${RELATION_TEXT[e.pixel_relation || ''] || e.pixel_relation || '已核验'}${e.pixel_observation || e.pixel_reason ? ` · ${clip(e.pixel_observation || e.pixel_reason, 60)}` : ''}`
                    : '计划内核验未执行'}
                </span>
              </div>
            ))
          )}
        </>
      )}
      {claim.status !== 'verified' && !correcting && (
        <button className="btn btn-sm" style={{ marginTop: 10 }} onClick={() => setCorrecting(true)}>
          <Icon name="pencil" size="sm" />追加更正
        </button>
      )}
      {correcting && (
        <div className="correction-box">
          <textarea
            placeholder="更正后的表述,将追加为新的 Claim 修订,不覆盖历史…"
            value={text}
            onChange={e => setText(e.target.value)}
          />
          <input
            className="input"
            style={{ marginTop: 8, height: 32, fontSize: 12.5 }}
            placeholder="更正原因(必填)"
            value={reason}
            onChange={e => setReason(e.target.value)}
          />
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 8 }}>
            <button className="btn btn-sm" disabled={submitting} onClick={() => setCorrecting(false)}>取消</button>
            <button className="btn btn-sm btn-primary" disabled={submitting} onClick={() => void submitCorrection()}>
              提交更正
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

// ---- 账本抽屉 ----

export interface LedgerDrawerProps {
  runId: string
  view?: EvidenceLedgerView
  loading?: boolean
  error?: string
  cites: CiteRef[]
  /** 产生该 run 的聊天路径:agent | research | evidence_funnel */
  modeLabel?: string
  onRetry: (runId: string) => void
  onOpenEvidence: (cite: CiteRef, cites: CiteRef[]) => void
  onClose: () => void
}

export function LedgerDrawer({ runId, view, loading, error, cites, modeLabel = 'agent', onRetry, onOpenEvidence, onClose }: LedgerDrawerProps) {
  const refresh = () => onRetry(runId)
  return (
    <>
      <DrawerVeil onClose={onClose} />
      <div className="drawer" style={{ width: 520 }} role="dialog" aria-label="证据账本">
        <div className="drawer-head">
          <Icon name="shield-check" />
          <h3>证据账本</h3>
          <span className="mono" style={{ fontSize: 10, color: 'var(--tx-4)', wordBreak: 'break-all' }}>{runId}</span>
          <button className="btn btn-ic btn-ghost" style={{ marginLeft: 'auto' }} onClick={onClose} aria-label="关闭">
            <Icon name="x" />
          </button>
        </div>
        <div className="drawer-body">
          <div className="run-meta" style={{ marginBottom: 14 }}>
            <span className="chip chip-mute mono">{modeLabel}</span>
            <span className="chip chip-mute">{view ? `${latestClaimsByRoot(view.claims).length} 条 claim · ${(view.evidence ?? []).length} 条证据` : '—'}</span>
            <span className="chip chip-mute">追加式,不覆盖历史</span>
          </div>
          <p style={{ fontSize: 13, color: 'var(--tx-3)', marginBottom: 14 }}>
            「已验证」表示来源可回放,不代表语义真值。
          </p>
          {loading && <div className="rail-empty" style={{ paddingTop: 30 }}><p>正在加载证据账本…</p></div>}
          {!loading && error && (
            <div className="rail-empty" style={{ paddingTop: 30 }}>
              <Icon name="alert" size="lg" />
              <p style={{ marginTop: 10 }}>{error}</p>
              <button className="btn btn-sm" style={{ marginTop: 10 }} onClick={refresh}>
                <Icon name="refresh" size="sm" />重试
              </button>
            </div>
          )}
          {!loading && !error && view && (
            <LedgerClaims view={view} cites={cites} onOpenEvidence={onOpenEvidence} onCorrected={refresh} />
          )}
        </div>
      </div>
    </>
  )
}
