import { useState } from 'react'
import { api } from '@/lib/api'
import type { TimelineAtom, VideoTask,VideoTimeline } from '@/lib/types'
import { formatTime } from '@/components/Citation'
import { Icon } from '@/components/ui/Icon'
import { ModalityTag } from '@/components/ui/ModalityTag'
import { FrameRead } from './ReadText'
import { taskVisualMode,visualModeLabel } from './videoView'
export interface VisualFrameView {
  key: string
  frameId?: number
  timeMs: number
  endMs: number
  ocr?: string
  caption?: string
  hasOcr: boolean
  hasCaption: boolean
}

export function groupVisualAtoms(atoms: TimelineAtom[]): VisualFrameView[] {
  const map = new Map<string, VisualFrameView>()
  for (const atom of atoms) {
    if (atom.modality !== 'visual_ocr' && atom.modality !== 'visual_caption') continue
    const key = atom.source_refs?.[0]?.stable_id || atom.id
    let view = map.get(key)
    if (!view) {
      view = { key, frameId: atom.source_refs?.[0]?.source_row_id, timeMs: atom.start_ms, endMs: atom.end_ms, hasOcr: false, hasCaption: false }
      map.set(key, view)
    }
    view.timeMs = Math.min(view.timeMs, atom.start_ms)
    view.endMs = Math.max(view.endMs, atom.end_ms)
    if (atom.modality === 'visual_ocr') {
      view.ocr = atom.content
      view.hasOcr = true
    } else {
      view.caption = atom.content
      view.hasCaption = true
    }
  }
  return [...map.values()].sort((a, b) => a.timeMs - b.timeMs)
}

function FramePreview({ src, timeMs }: { src: string | null; timeMs: number }) {
  const [failed, setFailed] = useState(false)
  return src && !failed
    ? <img src={src} alt={`${formatTime(timeMs)} 的已保存画面帧`} onError={() => setFailed(true)} style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', objectFit: 'cover' }} />
    : <div className="muted" role="status" style={{ padding: 12, fontSize: 12 }}>帧预览不可用</div>
}

export function VisualEvidencePanel({task,frames,timeline,videoDurationMs,playbackUrl,openVisualSettings,seek}: {
 task:VideoTask; frames:VisualFrameView[]; timeline:VideoTimeline|null;videoDurationMs:number;playbackUrl:string|null;openVisualSettings:(event?:{detail:number})=>void;seek:(ms:number)=>void
}) {
 const taskId=task.id

    const coverage = timeline?.visual_coverage
    const tailUncovered = !!coverage && videoDurationMs > 0 &&
      coverage.last_ms + Math.max(60_000, coverage.largest_gap_ms * 1.5) < videoDurationMs
    const evidenceTailUncovered = !!coverage && coverage.evidence_last_ms !== undefined && videoDurationMs > 0 &&
      coverage.evidence_last_ms + Math.max(60_000, coverage.largest_gap_ms * 1.5) < videoDurationMs
    return (
      <>
        <div className="workbench-visual-summary">
          <div><span className="workbench-eyebrow">画面分析</span><strong>{visualModeLabel(taskVisualMode(task))}</strong></div>
          <button className="btn btn-sm" onClick={openVisualSettings}><Icon name="settings" size="sm" />设置</button>
        </div>
        <p className="workbench-visual-hint">按内容选择 OCR 或画面描述。演讲、访谈通常无需启用；已有画面证据会保留。</p>
        {coverage ? <p style={{ fontSize: 13, color: 'var(--tx-3)', marginBottom: 10 }} role="status">
          已保存采样帧 {coverage.sampled_frames} 张，其中 {coverage.evidence_frames} 张生成了 OCR 或描述、{coverage.preview_frames} 张有预览。
          采样时间 {formatTime(coverage.first_ms)}–{formatTime(coverage.last_ms)}{videoDurationMs > 0 ? ` / 视频总长 ${formatTime(videoDurationMs)}` : '；视频总长待加载'}。
          {coverage.evidence_first_ms !== undefined && coverage.evidence_last_ms !== undefined ? ` 有文字证据的时间范围 ${formatTime(coverage.evidence_first_ms)}–${formatTime(coverage.evidence_last_ms)}。` : ' 尚无可用的 OCR 或描述文字。'}
          {tailUncovered ? ` 后段尚无采样帧，采样仅到 ${formatTime(coverage.last_ms)}。` : evidenceTailUncovered ? ' 后段采样帧尚未产出文字证据。' : ''}
        </p> : <p className="muted" style={{ fontSize: 13, marginBottom: 10 }}>尚无已保存的画面采样帧。</p>}
        {frames.length === 0 && <div className="empty"><Icon name="eye" size="lg" /><b>没有画面文字证据</b></div>}
        <div className="frames-list">
          {frames.map(f => (
            <button type="button" className="frame-row" key={f.key} onClick={() => seek(f.timeMs)}>
              <div className="frame-still">
                <FramePreview key={`${f.frameId}-${playbackUrl || ''}`} src={f.frameId ? api.visualFrameSrc(taskId, f.frameId, playbackUrl) : null} timeMs={f.timeMs} />
              </div>
              <div className="frame-copy">
                <div className="frame-meta">
                  <span className="frame-time">{formatTime(f.timeMs)}</span>
                  <span style={{ display: 'inline-flex', gap: 4 }}>
                    {f.hasOcr && <ModalityTag modality="visual_ocr" />}
                    {f.hasCaption && <ModalityTag modality="visual_caption" />}
                  </span>
                </div>
                <FrameRead>
                  {f.caption && <div className="frame-caption">{f.caption}</div>}
                  {f.ocr && <div className="frame-ocr">{f.ocr}</div>}
                </FrameRead>
              </div>
            </button>
          ))}
        </div>
      </>
    )
  }
