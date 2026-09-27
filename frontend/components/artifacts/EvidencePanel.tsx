import Link from '@/lib/router'
import { useRef } from 'react'
import { useQueries, useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { artifactApi, artifactError } from '@/lib/artifacts/api'
import type { Evidence, StudyBlock } from '@/lib/artifacts/schema'
import { canReplay, evidenceTime, isPointEvidence } from '@/lib/artifacts/view'
import { VideoPlayer, type VideoPlayerHandle } from '@/components/player/VideoPlayer'
import { Icon } from '@/components/ui/Icon'

export function EvidencePanel({ evidence, loading, error, onRetry, preview = false, outdated = false }: {
  evidence?: Evidence; loading?: boolean; error?: string; onRetry?: () => void; preview?: boolean; outdated?: boolean
}) {
  const player = useRef<VideoPlayerHandle>(null)
  const playback = useQuery({ queryKey: ['artifact-playback', evidence?.manifest_id, evidence?.source_id], queryFn: () => api.playbackSrc(evidence!.source_id), enabled: !!evidence && !preview && !outdated, staleTime: 60_000 })
  return <div className="artifact-evidence">
    <header><span><Icon name="link" />回到原视频</span><span className="mono">SOURCE</span></header>
    {loading ? <div className="evidence-empty" role="status">正在核对来源权限…</div> : error ? <div className="evidence-empty" role="alert"><Icon name="alert" /><p>{error}</p>{onRetry && <button className="btn btn-sm" onClick={onRetry}>重新读取证据</button>}</div> : !evidence ? <div className="evidence-empty"><Icon name="video" size="lg" /><h3>理解有来处</h3><p>选择一条引用或导图节点，查看它关联的视频片段。</p></div> : <>
      <div className="evidence-source-status"><span>● {preview ? '开发样例' : '来源已关联'}</span><span>{evidence.modality === 'transcript' ? '视频转写' : evidence.modality === 'visual_ocr' ? '画面文字' : evidence.modality === 'visual_caption' ? '画面观察' : '视频证据'}</span></div>
      {!preview && !outdated && playback.data ? <VideoPlayer key={`${evidence.source_id}-${evidence.id}`} ref={player} src={playback.data} compact initialTimeMs={canReplay(evidence) ? evidence.start_ms! : undefined} title={evidence.source_title} fallbackText="暂时无法播放，仍可核对下方来源文字" onNeedRefresh={() => api.playbackSrc(evidence.source_id)} /> : <div className={`evidence-media-empty${playback.isPending && !outdated && !preview ? ' loading' : ''}`} role={playback.error ? 'alert' : 'status'}><Icon name={playback.error ? 'alert' : 'video'} size="lg" /><p>{outdated ? '视频来源已更新' : preview ? '契约样例没有视频媒体' : playback.isPending ? '正在准备视频片段…' : '视频暂时无法播放'}</p><span>{outdated ? '旧快照时间不用于定位新视频' : preview ? '正式页面使用原视频回放' : '下方来源文字和引用仍可核对'}</span>{!preview && !outdated && !playback.isPending && <button className="btn btn-sm" onClick={() => void playback.refetch()}>重试播放源</button>}</div>}
      <h3>{evidence.source_title}</h3>
      <div className="evidence-transcript-label">原文片段</div>
      <p className="evidence-time mono">{evidenceTime(evidence)}</p>
      <blockquote>{evidence.content}</blockquote>
      <p className="evidence-hint">{evidence.time_range_status === 'unknown' ? '来源未提供可靠时间范围，因此不提供定位回放。' : evidence.time_range_status === 'coarse' ? '此处为粗粒度时间范围，回放后请结合上下文核对。' : '时间来自视频证据，可以直接定位回看。'}</p>
      <button className="btn evidence-replay" disabled={preview || outdated || !playback.data || !canReplay(evidence)} onClick={() => player.current?.seek(evidence.start_ms!, true, evidence.id)}><Icon name="play" size="sm" />{canReplay(evidence) ? isPointEvidence(evidence) ? '定位到这个画面' : '从片段开始回看' : '没有可定位的时间'}</button>
      {!preview && <Link href={`/video/${evidence.source_id}${canReplay(evidence) && !outdated ? `?t=${evidence.start_ms}` : ''}`} className="btn btn-ghost evidence-source-link">打开视频详情<Icon name="chev-r" size="sm" /></Link>}
      <details className="evidence-identity"><summary>来源标识</summary><dl><dt>证据</dt><dd>{evidence.id}</dd><dt>快照</dt><dd>{evidence.manifest_id}</dd><dt>原始观察</dt><dd>{evidence.source_identity}</dd></dl></details>
    </>}
  </div>
}

export function RemoteEvidencePanel({ manifestId, refs, evidenceId, onSelect, outdated }: { manifestId?: string; refs: StudyBlock['evidence_refs']; evidenceId?: string; onSelect: (id: string) => void; outdated?: boolean }) {
  const ids = [...new Set(refs.map(ref => ref.evidence_id))]
  const queries = useQueries({ queries: ids.map(id => ({ queryKey: ['artifact-evidence', manifestId, id], queryFn: ({ signal }: { signal: AbortSignal }) => artifactApi.evidence(manifestId!, id, signal), enabled: !!manifestId, staleTime: 0, refetchInterval: 15_000 })) })
  const activeIndex = Math.max(0, refs.findIndex(ref => ref.evidence_id === evidenceId))
  const query = queries[ids.indexOf(refs[activeIndex]?.evidence_id)]
  // An inaccessible quote never reuses cached content; other references remain selectable.
  return <div className="artifact-evidence-stack"><div className="evidence-choices" aria-label="此块关联的全部依据"><b>关联依据 · {refs.length}</b>{refs.length ? refs.map((ref, index) => {
    const result = queries[ids.indexOf(ref.evidence_id)]
    const item = result?.error ? undefined : result?.data
    return <button key={`${ref.evidence_id}-${index}`} className={index === activeIndex ? 'selected' : ''} aria-pressed={index === activeIndex} onClick={() => onSelect(ref.evidence_id)}><span className="evidence-choice-head"><strong>{index + 1}. {ref.relation === 'contradicts' ? '相反证据' : ref.relation === 'context' ? '背景证据' : '支持依据'}</strong><small>{item ? `${item.modality === 'transcript' ? '转写' : item.modality === 'visual_ocr' ? '画面文字' : item.modality === 'visual_caption' ? '画面观察' : '视频'} · ${evidenceTime(item)}` : result?.error ? '读取失败' : '读取中…'}</small></span><span className="evidence-choice-excerpt">{item ? item.content : result?.error ? artifactError(result.error) : '正在核对来源…'}</span></button>
  }) : <p>这个块还没有来源引用。</p>}</div><EvidencePanel outdated={outdated} evidence={query?.error ? undefined : query?.data} loading={!!refs.length && !!query?.isPending} error={query?.error ? artifactError(query.error) : undefined} onRetry={() => void query?.refetch()} /></div>
}
