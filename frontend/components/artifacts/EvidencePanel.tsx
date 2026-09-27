import Link from '@/lib/router'
import { useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { artifactApi, artifactError } from '@/lib/artifacts/api'
import type { Evidence } from '@/lib/artifacts/schema'
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
      {!preview && !outdated && playback.data ? <VideoPlayer key={`${evidence.source_id}-${evidence.id}`} ref={player} src={playback.data} compact initialTimeMs={canReplay(evidence) ? evidence.start_ms! : undefined} title={evidence.source_title} fallbackText="暂时无法播放，仍可核对下方来源文字" onNeedRefresh={() => api.playbackSrc(evidence.source_id)} /> : <div className="evidence-media-empty"><Icon name="video" size="lg" /><p>{outdated ? '视频来源已更新' : preview ? '契约样例没有视频媒体' : playback.isPending ? '正在读取视频…' : '视频暂时无法播放'}</p><span>{outdated ? '旧快照时间不用于定位新视频' : preview ? '正式页面使用原视频回放' : '仍可核对下方来源文字'}</span>{!preview && !outdated && !playback.isPending && <button className="btn btn-sm" onClick={() => void playback.refetch()}>重试播放源</button>}</div>}
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

export function RemoteEvidencePanel({ manifestId, evidenceId, outdated }: { manifestId?: string; evidenceId?: string; outdated?: boolean }) {
  const query = useQuery({ queryKey: ['artifact-evidence', manifestId, evidenceId], queryFn: ({ signal }) => artifactApi.evidence(manifestId!, evidenceId!, signal), enabled: !!manifestId && !!evidenceId, staleTime: 0, refetchInterval: 15_000 })
  // Never keep showing a cached quote after the server denies access or deletes the source.
  return <EvidencePanel outdated={outdated} evidence={query.error ? undefined : query.data} loading={!!evidenceId && query.isPending} error={query.error ? artifactError(query.error) : undefined} onRetry={() => void query.refetch()} />
}
