'use client'

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useRouter } from 'next/navigation'
import { api, ApiError } from '@/lib/api'
import {
  TaskStatusEnum,
  type RAGIndexResult,
  type TimelineAtom,
  type VideoTask,
  type VideoTimeline,
} from '@/lib/types'
import { fmtRelTime, taskTitle } from '@/lib/format'
import { formatTime } from '@/components/Citation'
import { ModalityTag } from '@/components/ui/ModalityTag'
import { VideoPlayer, type VideoPlayerHandle } from '@/components/player/VideoPlayer'
import { MarkdownAnswer } from '@/components/chat/MarkdownAnswer'
import { useCrumb } from '@/components/shell/AppShell'
import { useToast } from '@/components/Toast'
import { Icon } from '@/components/ui/Icon'
import { expandTranscript } from '@/lib/transcript'
import { ProcessStrip } from '@/components/ProcessStrip'
import { VideoStill } from '@/components/VideoPoster'

// 视频工作台:播放器 + 多模态时间轴 + 画面证据 + 检索索引 + 摘要。
// 对应原型 #/video/:id。播放源用 /playback 签名 URL;时间轴/画面证据来自
// /timeline 的原子(转写、OCR、画面描述同轨),帧图像无浏览器预览通道,
// 卡片展示索引时保存的画面观察文本,回放统一 seek 播放器。

type TabKey = 'tl' | 'vf' | 'idx'
type ActionKind = 'transcribe' | 'analyze' | 'index' | 'download'

interface VisualFrameView {
  key: string
  timeMs: number
  endMs: number
  ocr?: string
  caption?: string
  hasOcr: boolean
  hasCaption: boolean
}

function groupVisualAtoms(atoms: TimelineAtom[]): VisualFrameView[] {
  const map = new Map<string, VisualFrameView>()
  for (const atom of atoms) {
    if (atom.modality !== 'visual_ocr' && atom.modality !== 'visual_caption') continue
    const key = atom.source_refs?.[0]?.stable_id || atom.id
    let view = map.get(key)
    if (!view) {
      view = { key, timeMs: atom.start_ms, endMs: atom.end_ms, hasOcr: false, hasCaption: false }
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

export default function VideoWorkbenchPage({ params }: { params: { id: string } }) {
  const taskId = Number(params.id)
  const router = useRouter()
  const toast = useToast()
  const playerRef = useRef<VideoPlayerHandle>(null)
  const prevTransRef = useRef(false)

  const [task, setTask] = useState<VideoTask | null>(null)
  const [timeline, setTimeline] = useState<VideoTimeline | null>(null)
  const [index, setIndex] = useState<RAGIndexResult | null>(null)
  const [playbackUrl, setPlaybackUrl] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [tab, setTab] = useState<TabKey>('tl')
  const [playheadMs, setPlayheadMs] = useState(0)
  const [busy, setBusy] = useState<ActionKind | ''>('')
  const [headSnap, setHeadSnap] = useState(false)
  const liveRowRef = useRef<HTMLDivElement>(null)

  useCrumb([
    { label: '视频库', href: '/library' },
    { label: task ? taskTitle(task) : `视频 #${taskId}` },
  ])

  useEffect(() => {
    let active = true
    setLoading(true)
    setLoadError('')
    setTask(null)
    setTimeline(null)
    setIndex(null)
    setPlaybackUrl(null)
    setPlayheadMs(0)
    void (async () => {
      let detail: VideoTask
      try {
        detail = await api.getTask(taskId)
      } catch (e) {
        if (!active) return
        setLoadError(e instanceof ApiError ? e.message : '视频详情加载失败')
        setLoading(false)
        return
      }
      if (!active) return
      setTask(detail)
      prevTransRef.current = detail.has_transcription
      setLoading(false)
      const [tl, idx, playback] = await Promise.all([
        detail.has_transcription ? api.getTimeline(taskId).catch(() => null) : Promise.resolve(null),
        api.getRagIndex(taskId).catch(() => null),
        api.getTaskPlaybackUrl(taskId).catch(() => null),
      ])
      if (!active) return
      setTimeline(tl)
      setIndex(idx)
      setPlaybackUrl(playback?.playback_url || null)
    })()
    return () => { active = false }
  }, [taskId])

  const processing = !!task && (task.status === TaskStatusEnum.Queued || task.status === TaskStatusEnum.Running)

  // 处理中每 5s 轮询任务详情;转写刚完成或任务刚完成时补拉时间轴与索引
  useEffect(() => {
    if (!processing) return
    const iv = setInterval(() => {
      void (async () => {
        try {
          const fresh = await api.getTask(taskId)
          const prev = prevTransRef.current
          prevTransRef.current = fresh.has_transcription
          setTask(fresh)
          const transJustDone = !prev && fresh.has_transcription
          const justCompleted = fresh.status === TaskStatusEnum.Completed
          if (transJustDone || justCompleted) {
            const [tl, idx] = await Promise.all([
              api.getTimeline(taskId).catch(() => null),
              api.getRagIndex(taskId).catch(() => null),
            ])
            setTimeline(tl)
            setIndex(idx)
          }
        } catch { /* 下个周期重试 */ }
      })()
    }, 5000)
    return () => clearInterval(iv)
  }, [processing, taskId])

  const transcriptAtoms = useMemo(
    () => (timeline?.atoms || []).filter(a => a.modality === 'transcript'),
    [timeline],
  )
  const transcriptRows = useMemo(() => expandTranscript(transcriptAtoms), [transcriptAtoms])
  const visualAtoms = useMemo(
    () => (timeline?.atoms || []).filter(a => a.modality === 'visual_ocr' || a.modality === 'visual_caption'),
    [timeline],
  )
  const frames = useMemo(() => groupVisualAtoms(timeline?.atoms || []), [timeline])
  const timelineMs = useMemo(
    () => (timeline?.atoms || []).reduce((max, a) => Math.max(max, a.end_ms, a.start_ms), 0),
    [timeline],
  )

  const seek = useCallback((ms: number, autoplay = true, cue?: string) => {
    playerRef.current?.seek(ms, autoplay, cue)
    setPlayheadMs(ms)
    setHeadSnap(true)
    window.setTimeout(() => setHeadSnap(false), 280)
  }, [])

  // 播放签名 URL 只有 5 分钟有效期,过期后重取一次并原位恢复
  const refreshPlaybackUrl = useCallback(async () => {
    try {
      const playback = await api.getTaskPlaybackUrl(taskId)
      if (playback?.playback_url) {
        setPlaybackUrl(playback.playback_url)
        return playback.playback_url
      }
    } catch { /* 保持失败态 */ }
    return null
  }, [taskId])

  const liveIndex = transcriptRows.findIndex(
    a => playheadMs >= a.start_ms && playheadMs < Math.max(a.end_ms, a.start_ms + 1),
  )

  useEffect(() => {
    const el = liveRowRef.current
    if (!el) return
    const root = el.closest('.rail-body')
    if (!(root instanceof HTMLElement)) return
    const rootBox = root.getBoundingClientRect()
    const box = el.getBoundingClientRect()
    if (box.top < rootBox.top + 12 || box.bottom > rootBox.bottom - 12) {
      el.scrollIntoView({ block: 'center', behavior: 'smooth' })
    }
  }, [liveIndex])

  const runAction = async (kind: Exclude<ActionKind, 'download'>, force = false) => {
    if (!task || busy) return
    setBusy(kind)
    try {
      if (kind === 'transcribe') {
        await api.transcribe(task.id, force)
        toast.success('转写已重新入队,已完成的分片不会重复调用')
      } else if (kind === 'analyze') {
        await api.analyze(task.id, force)
        toast.success('摘要任务已加入队列,完成后会出现在这里')
      } else {
        const r = await api.triggerRagIndex(task.id)
        setIndex(r)
        toast.success('索引构建已开始,重建只重做投影,不重做转写')
      }
      const fresh = await api.getTask(task.id).catch(() => null)
      if (fresh) {
        prevTransRef.current = fresh.has_transcription
        setTask(fresh)
      }
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '操作失败')
    } finally {
      setBusy('')
    }
  }

  const downloadAudio = async () => {
    if (!task || busy) return
    setBusy('download')
    try {
      const r = await api.downloadAudio(task.id)
      const a = document.createElement('a')
      a.href = r.download_url
      a.download = r.filename || ''
      document.body.appendChild(a)
      a.click()
      a.remove()
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '获取下载链接失败')
    } finally {
      setBusy('')
    }
  }

  if (loading) {
    return <div className="page"><div className="empty"><b>加载中…</b></div></div>
  }
  if (loadError || !task) {
    return (
      <div className="page">
        <div className="card">
          <div className="empty">
            <Icon name="alert" size="lg" />
            <b>视频加载失败</b>
            <p>{loadError || '任务不存在'}</p>
            <button className="btn btn-sm" style={{ marginTop: 8 }} onClick={() => router.push('/library')}>
              <Icon name="chev-l" size="sm" />返回视频库
            </button>
          </div>
        </div>
      </div>
    )
  }

  const failed = task.status === TaskStatusEnum.Failed || task.status === TaskStatusEnum.Dead
  const urlJob = failed && task.last_job_type === 'download'
  const title = taskTitle(task)

  const durPct = timelineMs > 0 ? timelineMs : 1
  const hasRail = timelineMs > 0

  const renderTL = () => {
    if (!task.has_transcription) {
      return (
        <div className="empty">
          <Icon name="activity" size="lg" />
          <b>转写完成后就能看到时间轴</b>
        </div>
      )
    }
    if (transcriptAtoms.length === 0 && visualAtoms.length === 0) {
      return (
        <div className="empty">
          <Icon name="activity" size="lg" />
          <b>时间轴暂无数据</b>
        </div>
      )
    }
    return (
      <>
        {hasRail && (
          <>
            <div
              className="tl-rail"
              onPointerDown={e => {
                const rect = e.currentTarget.getBoundingClientRect()
                const ratio = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
                seek(ratio * timelineMs)
              }}
            >
              {transcriptAtoms.map(a => (
                <div
                  key={`tt-${a.id}`}
                  className="tl-seg t-transcript"
                  style={{ left: `${(a.start_ms / durPct) * 100}%`, width: `${Math.max(0.3, ((a.end_ms - a.start_ms) / durPct) * 100)}%` }}
                />
              ))}
              {visualAtoms.map(a => (
                <div
                  key={`tv-${a.id}`}
                  className={`tl-seg ${a.modality === 'visual_ocr' ? 't-ocr' : 't-caption'}`}
                  style={{ left: `${(a.start_ms / durPct) * 100}%`, width: `max(6px, ${((a.end_ms - a.start_ms) / durPct) * 100}%)` }}
                  title={a.content}
                />
              ))}
              <div className={`tl-head${headSnap ? ' snap' : ''}`} style={{ left: `${(playheadMs / durPct) * 100}%` }} />
            </div>
            <div className="tl-scale mono">
              {Array.from({ length: 6 }, (_, i) => (
                <span key={i}>{formatTime((timelineMs * i) / 5)}</span>
              ))}
            </div>
            <div className="tl-legend">
              <span><i style={{ background: 'rgba(154,145,127,.5)' }} />解说转写</span>
              <span><i style={{ background: 'rgba(226,168,75,.6)' }} />画面 OCR</span>
              <span><i style={{ background: 'rgba(134,180,201,.55)' }} />画面描述</span>
              <span style={{ marginLeft: 'auto', color: 'var(--tx-4)' }}>点击任意位置跳转</span>
            </div>
          </>
        )}
        {transcriptRows.length > 0 && (
          <div className="transcript-list">
            {transcriptRows.map((a, i) => (
              <div
                key={a.id}
                ref={i === liveIndex ? liveRowRef : undefined}
                className={`t-row${i === liveIndex ? ' live' : ''}`}
                onClick={() => seek(a.start_ms)}
              >
                <span className="ts">{formatTime(a.start_ms)}</span>
                <span className="tx">{a.content}</span>
              </div>
            ))}
          </div>
        )}
      </>
    )
  }

  const renderVF = () => {
    if (frames.length === 0) {
      return (
        <div className="empty">
          <Icon name="eye" size="lg" />
          <b>没有画面索引</b>
        </div>
      )
    }
    return (
      <>
        <p style={{ fontSize: 13, color: 'var(--tx-3)', marginBottom: 12 }}>
          {frames.length} 帧 · 点击时间码回放
        </p>
        <div className="frames-grid">
          {frames.map(f => (
            <div className="frame-card" key={f.key}>
              <div className="vthumb-art" style={{ aspectRatio: '16/9', position: 'relative' }}>
                <VideoStill src={playbackUrl} timeMs={f.timeMs} seed={`${taskId}-${f.key}`} />
                <span
                  className="mono"
                  style={{ position: 'absolute', left: 8, top: 6, zIndex: 2, fontSize: 10.5, color: 'var(--tx-2)', background: 'rgba(10,9,7,.6)', padding: '1px 6px', borderRadius: 5 }}
                >
                  {formatTime(f.timeMs)}
                </span>
              </div>
              <div className="fc-body">
                <div className="row">
                  <span className="fc-time mono" style={{ cursor: 'pointer' }} onClick={() => seek(f.timeMs)}>{formatTime(f.timeMs)}</span>
                  <span style={{ display: 'inline-flex', gap: 4 }}>
                    {f.hasOcr && <ModalityTag modality="visual_ocr" />}
                    {f.hasCaption && <ModalityTag modality="visual_caption" />}
                  </span>
                </div>
                {f.ocr && <div className="fc-text mono" style={{ fontSize: 10.5, letterSpacing: '.02em' }}>{f.ocr}</div>}
                {f.caption && <div className="fc-text" style={f.ocr ? { marginTop: 5 } : undefined}>{f.caption}</div>}
              </div>
            </div>
          ))}
        </div>
      </>
    )
  }

  const renderIdx = () => {
    if (!index) {
      return (
        <div className="empty">
          <Icon name="layers" size="lg" />
          <b>索引状态不可用</b>
        </div>
      )
    }
    const stateView = index.indexed
      ? { chip: 'chip-ok', text: '已建立' }
      : index.needs_rebuild
        ? { chip: 'chip-warn', text: '需要重建' }
        : { chip: 'chip-mute', text: '未建立' }
    return (
      <>
        <div className="idx-list">
          <div className="idx-row"><span className="k">状态</span><span className="v"><span className={`chip ${stateView.chip}`}>{stateView.text}</span></span></div>
          <div className="idx-row"><span className="k">证据块</span><span className="v mono">{index.chunks} 块</span></div>
          <div className="idx-row"><span className="k">向量模型</span><span className="v mono">{index.embedding_model || '—'}</span></div>
          {index.last_error && (
            <div className="idx-row"><span className="k">最近错误</span><span className="v" style={{ color: 'var(--bad)', fontSize: 12 }}>{index.last_error}</span></div>
          )}
        </div>
        {index.needs_rebuild && (
          <p style={{ fontSize: 13, color: 'var(--tx-4)', marginTop: 12 }}>索引已过期,需要重建</p>
        )}
        <button
          className="btn btn-sm"
          style={{ marginTop: 14 }}
          disabled={busy !== ''}
          onClick={() => void runAction('index')}
        >
          <Icon name="layers" size="sm" />
          {index.indexed ? '重建索引' : '建立索引'}
        </button>
      </>
    )
  }

  return (
    <div className="page page-wide">
      <div className="ws">
        <div>
          <VideoPlayer
            ref={playerRef}
            src={playbackUrl}
            title={title}
            onPlayhead={ms => setPlayheadMs(ms)}
            onNeedRefresh={refreshPlaybackUrl}
            fallbackText={failed ? '任务处理失败,暂无可用播放源' : '播放源暂不可用,文件可能仍在处理'}
          />
          {processing && (
            <div style={{ marginTop: 12 }}>
              <ProcessStrip status={task.status} stage={task.stage} has_transcription={task.has_transcription} />
            </div>
          )}

          <div className="ws-actions">
            {task.has_summary ? (
              <button
                className="btn"
                onClick={() => document.getElementById('summaryBlock')?.scrollIntoView({ behavior: 'smooth', block: 'start' })}
              >
                <Icon name="file" size="sm" />查看摘要
              </button>
            ) : (
              <button
                className="btn"
                disabled={busy !== '' || processing || !task.has_transcription}
                title={!task.has_transcription ? '转写完成后才能生成摘要' : undefined}
                onClick={() => void runAction('analyze')}
              >
                <Icon name="wand" size="sm" />{busy === 'analyze' ? '已加入队列…' : '生成摘要'}
              </button>
            )}
            {task.has_transcription ? (
              <button className="btn" disabled={busy !== ''} onClick={() => void runAction('transcribe', true)}>
                <Icon name="refresh" size="sm" />重新转写
              </button>
            ) : processing ? (
              <button className="btn" disabled>
                <Icon name="activity" size="sm" />等待转写
              </button>
            ) : (
              <button className="btn" disabled={busy !== ''} onClick={() => void runAction('transcribe')}>
                <Icon name="activity" size="sm" />开始转写
              </button>
            )}
            <button className="btn" disabled={busy !== ''} onClick={() => void runAction('index')}>
              <Icon name="layers" size="sm" />{index?.indexed ? '重建索引' : '建立索引'}
            </button>
            <button className="btn" disabled={busy !== ''} onClick={() => void downloadAudio()}>
              <Icon name="download" size="sm" />下载音频
            </button>
            <span style={{ flex: 1 }} />
            <button className="btn btn-primary" onClick={() => router.push(`/chat/v/${task.id}`)}>
              <Icon name="message" size="sm" />进入问答
            </button>
          </div>

          {failed && (
            <div className="card card-pad" style={{ marginTop: 14, borderColor: 'rgba(224,131,115,.35)' }}>
              <div style={{ display: 'flex', gap: 10, alignItems: 'flex-start' }}>
                <span style={{ color: 'var(--bad)' }}><Icon name="alert" /></span>
                <div style={{ flex: 1 }}>
                  <b style={{ fontSize: 13 }}>{task.status === TaskStatusEnum.Dead ? '任务已废弃' : '处理失败'}</b>
                  <p style={{ fontSize: 12, color: 'var(--tx-3)', marginTop: 4 }}>
                    {task.error_msg || task.last_error_msg || '处理过程中出现错误'}
                    {task.max_retries > 0 ? ` · 重试 ${task.retry_count}/${task.max_retries}` : ''}
                  </p>
                  {urlJob ? (
                    <span className="chip chip-mute" style={{ marginTop: 10 }}>URL 任务,请删除后重新添加</span>
                  ) : (
                    <button
                      className="btn btn-sm"
                      style={{ marginTop: 10 }}
                      disabled={busy !== ''}
                      onClick={() => void runAction(task.last_job_type === 'analyze' ? 'analyze' : 'transcribe')}
                    >
                      重试
                    </button>
                  )}
                </div>
              </div>
            </div>
          )}

          <div className="summary-block" id="summaryBlock">
            {task.summary ? (
              <div className="card card-pad">
                <h3>
                  <Icon name="file" size="sm" />AI 摘要
                  <span style={{ marginLeft: 'auto', fontSize: 11, color: 'var(--tx-4)', fontWeight: 500 }}>
                    {task.summary.model_name} · {fmtRelTime(task.summary.created_at)}
                  </span>
                </h3>
                <div className="summary-body"><MarkdownAnswer content={task.summary.content} /></div>
              </div>
            ) : (
              <div className="empty card">
                <Icon name="wand" size="lg" />
                <b>还没有摘要</b>
              </div>
            )}
          </div>
        </div>

        <div className="card" style={{ overflow: 'hidden' }}>
          <div className="rail-tabs" style={{ padding: '10px 14px 0' }}>
            {([['tl', '转写时间轴'], ['vf', '画面证据'], ['idx', '检索索引']] as const).map(([key, label]) => (
              <button key={key} className={`rail-tab${tab === key ? ' on' : ''}`} onClick={() => setTab(key)}>
                {label}
              </button>
            ))}
          </div>
          <div className="rail-body" style={{ padding: '16px 18px 26px' }}>
            {tab === 'tl' ? renderTL() : tab === 'vf' ? renderVF() : renderIdx()}
          </div>
        </div>
      </div>
    </div>
  )
}
