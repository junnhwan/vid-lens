import type { CiteRef } from '@/components/Citation'
import { citationTimeLabel, formatTime, hasReplayRange, needsCitationUpgrade } from '@/components/Citation'
import { fmtScore } from '@/lib/format'
import { ModalityTag, modalityView } from '@/components/ui/ModalityTag'
import { useToast } from '@/components/Toast'
import { Icon } from '@/components/ui/Icon'
import { DrawerVeil } from '@/components/ui/Modal'
import Link from '@/lib/router'

// 证据详情抽屉:行内 C# chip / 引用卡点开。展示模态、毫秒范围、anchor quote、
// 召回通道等后端真实携带的元数据;「跳转回放」由宿主决定行为(单视频内 seek
// 迷你播放器,跨视频/知识库范围跳到对应视频工作台),本组件只声明可用性。

interface EvidenceDrawerProps {
  cite: CiteRef
  cites?: CiteRef[]
  onSelect?: (cite: CiteRef) => void
  fallbackTaskId?: number
  /** 单视频范围下用于展示/复制的标题兜底(当前视频名) */
  fallbackTitle: string
  canJump: boolean
  jumpDisabledHint?: string
  onJump: (cite: CiteRef) => void
  onClose: () => void
}

function timeStatusText(status?: string): string {
  if (status === 'exact') return '来源时间'
  if (status === 'coarse') return '原片段时间'
  if (status === 'unknown') return '未知'
  return status || '—'
}

export function EvidenceDrawer({ cite, cites, onSelect, fallbackTaskId, fallbackTitle, canJump, jumpDisabledHint, onJump, onClose }: EvidenceDrawerProps) {
  const toast = useToast()

  const title = cite.videoTitle || fallbackTitle
  const quote = cite.anchorQuote || cite.content
  const hasRange = hasReplayRange(cite)
  const taskId = cite.taskId || fallbackTaskId
  const context = cite.displayContext || cite.content
  const quoteOffset = quote ? context.indexOf(quote) : -1
  const contextStart = quoteOffset < 0 ? 0 : Math.max(0, quoteOffset - Math.min(160, Math.max(0, 600 - quote.length)))
  const excerpt = context.slice(contextStart, contextStart + 600)
  const highlightStart = quoteOffset < 0 ? -1 : quoteOffset - contextStart
  const highlightEnd = Math.min(excerpt.length, highlightStart + quote.length)

  const copyCitation = () => {
    const text = `[${title} ${citationTimeLabel(cite)}] ${quote}`
    if (navigator.clipboard) {
      navigator.clipboard.writeText(text).then(
        () => toast.success('引用文本已复制'),
        () => toast.error('复制失败'),
      )
    }
  }

  return (
    <>
      <DrawerVeil onClose={onClose} />
      <div className="drawer" role="dialog" aria-label="证据详情">
        <div className="drawer-head">
          <span
            className="mono"
            style={{
              width: 26, height: 26, borderRadius: 8, display: 'grid', placeItems: 'center',
              fontSize: 11, fontWeight: 700, background: 'var(--acc-dim)', color: 'var(--acc-strong)', border: '1px solid var(--acc-line)',
            }}
          >
            {cite.id}
          </span>
          <h3>证据详情</h3>
          <button className="btn btn-ic btn-ghost" style={{ marginLeft: 'auto' }} onClick={onClose} aria-label="关闭">
            <Icon name="x" />
          </button>
        </div>
        <div className="drawer-body">
          {cites && cites.length > 1 && onSelect && <div className="evidence-source-choices" aria-label="本回答的引用">
            <span className="field-label">本回答的引用</span>
            <div>{cites.map(source => <button key={source.id} type="button" className={`chip ${source.id === cite.id ? 'chip-acc' : 'chip-mute'}`} aria-pressed={source.id === cite.id} aria-label={`切换到引用 ${source.id}`} title={`${citationTimeLabel(source)} · ${source.anchorQuote || source.content}`} onClick={() => onSelect(source)}>{source.id} · {citationTimeLabel(source)}</button>)}</div>
          </div>}
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
            <span style={{ fontSize: 13, fontWeight: 600 }}>{title}</span>
            {hasRange && (
              <span className="mono" style={{ fontSize: 11.5, color: 'var(--acc-strong)' }}>
                {citationTimeLabel(cite)}
              </span>
            )}
          </div>
          <div className="ev-quote">{quote}</div>
          {cite.timeRangeStatus === 'coarse' && <p className="evidence-time-hint">这句话的独立时间尚未记录；回放会从原片段开始，请结合下方原文核对。</p>}
          <div className="ev-meta-grid">
            <div className="ev-meta-cell"><div className="k">证据模态</div><div className="v"><ModalityTag modality={cite.modality} /></div></div>
            <div className="ev-meta-cell"><div className="k">时间状态</div><div className="v">{timeStatusText(cite.timeRangeStatus)}</div></div>
          </div>
          <div className="field-label">来源上下文</div>
          <p className="evidence-source-context" style={{ fontSize: 12, color: 'var(--tx-2)', lineHeight: 1.7 }}>
            {contextStart > 0 ? '…' : ''}
            {highlightStart >= 0 ? <>{excerpt.slice(0, highlightStart)}<mark>{excerpt.slice(highlightStart, highlightEnd)}</mark>{excerpt.slice(highlightEnd)}</> : excerpt}
            {cite.displayContextTruncated || contextStart + excerpt.length < context.length ? '…' : ''}
          </p>
          <div style={{ display: 'flex', gap: 9, marginTop: 18 }}>
            <button
              className="btn btn-primary"
              disabled={!canJump}
              title={!canJump ? (jumpDisabledHint || '该证据没有可回放的时间范围') : undefined}
              onClick={() => onJump(cite)}
            >
              <Icon name="play" size="sm" />
              {hasRange ? `${cite.timeRangeStatus === 'coarse' ? '从原片段' : '跳转'} ${formatTime(cite.startMS)} 回放` : '查看来源'}
            </button>
            <button className="btn" onClick={copyCitation}>
              <Icon name="file" size="sm" />复制引用
            </button>
          </div>
          {taskId && needsCitationUpgrade(cite) && <div className="evidence-upgrade">
            <p>这个视频的旧转写使用长片段时间。补齐后，新回答可以定位到更短的语音片段；历史回答保留原有引用。</p>
            <Link className="btn btn-sm" href={`/video/${taskId}?citations=upgrade#transcript`} onClick={onClose}><Icon name="refresh" size="sm" />补齐引用定位</Link>
          </div>}
          {cite.modality === 'visual_ocr' || cite.modality === 'visual_caption' ? (
            <p style={{ fontSize: 10.5, color: 'var(--tx-4)', marginTop: 10 }}>
              {modalityView(cite.modality).text}类证据来自关键帧观察,跳转后可对照画面核对。
            </p>
          ) : null}
          <details className="evidence-technical"><summary>技术详情</summary><div className="ev-meta-grid">
            <div className="ev-meta-cell"><div className="k">证据 ID</div><div className="v mono" style={{ fontWeight: 500 }}>{cite.evidenceId || cite.id}</div></div>
            <div className="ev-meta-cell"><div className="k">召回通道</div><div className="v mono" style={{ fontWeight: 500 }}>{cite.source || '—'}</div></div>
            <div className="ev-meta-cell"><div className="k">相关度</div><div className="v mono" style={{ fontWeight: 500 }}>{fmtScore(cite.score)}</div></div>
            <div className="ev-meta-cell"><div className="k">来源映射</div><div className="v">{cite.sourceMappingStatus || '—'}</div></div>
          </div></details>
        </div>
      </div>
    </>
  )
}
