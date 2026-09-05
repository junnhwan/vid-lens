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
import { useCrumb } from '@/components/shell/AppShell'
import { useToast } from '@/components/Toast'
import { Icon } from '@/components/ui/Icon'

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

  useCrumb(['视频库', task ? taskTitle(task) : `视频 #${taskId}`])

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
  const visualAtoms = useMemo(
    () => (timeline?.atoms || []).filter(a => a.modality === 'visual_ocr' || a.modality === 'visual_caption'),
    [timeline],
  )
  const frames = useMemo(() => groupVisualAtoms(timeline?.atoms || []), [timeline])
  const timelineMs = useMemo(
    () => (timeline?.atoms || []).reduce((max, a) => Math.max(max, a.end_ms, a.start_ms), 0),
    [timeline],
  )

  const seek = useCallback((ms: number, autoplay = true) => {
    playerRef.current?.seek(ms, autoplay)
    setPlayheadMs(ms)
  }, [])

  const liveIndex = transcriptAtoms.findIndex(
    a => playheadMs >= a.start_ms && playheadMs < Math.max(a.end_ms, a.start_ms + 1),
  )

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
          <p>时间轴会把解说转写与画面观察排在同一条可回放的时间线上。</p>
        </div>
      )
    }
    if (transcriptAtoms.length === 0 && visualAtoms.length === 0) {
      return (
        <div className="empty">
          <Icon name="activity" size="lg" />
          <b>时间轴暂无数据</b>
          <p>转写分片仍在处理,还没有产生可定位的时间片段。</p>
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
              <div className="tl-head" style={{ left: `${(playheadMs / durPct) * 100}%` }} />
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
        {transcriptAtoms.length > 0 && (
          <div className="transcript-list">
            {transcriptAtoms.map((a, i) => (
              <div key={a.id} className={`t-row${i === liveIndex ? ' live' : ''}`} onClick={() => seek(a.start_ms)}>
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
          <p>视觉分支会与转写并行:关键帧 OCR 与画面描述分开入索引,失败不影响文本问答。</p>
        </div>
      )
    }
    return (
      <>
        <p style={{ fontSize: 12, color: 'var(--tx-3)', marginBottom: 12 }}>
          关键帧经感知哈希去重后,OCR 与画面描述分别入索引。下面 {frames.length} 帧都带稳定时间戳,点击时间码即可回放。
        </p>
        <p style={{ fontSize: 11.5, color: 'var(--tx-4)', marginBottom: 12 }}>
          帧图像存储在对象存储,当前版本未开放浏览器预览,卡片展示索引时保存的画面观察文本。
        </p>
        <div className="frames-grid">
          {frames.map(f => (
            <div className="frame-card" key={f.key}>
              <div className="vthumb-art" style={{ aspectRatio: '16/9', position: 'relative' }}>
                <span
                  className="mono"
                  style={{ position: 'absolute', left: 8, top: 6, fontSize: 10.5, color: 'var(--tx-2)', background: 'rgba(10,9,7,.6)', padding: '1px 6px', borderRadius: 5 }}
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
          <p>查询索引需要已配置默认 AI Profile(用于向量维度校验);配置后即可在这里查看与重建索引。</p>
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
          <p style={{ fontSize: 11.5, color: 'var(--tx-4)', marginTop: 12 }}>
            投影已过期(needs_rebuild)。重建索引只重做检索投影,不重做转写。
          </p>
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
            onPlayhead={setPlayheadMs}
            fallbackText={failed ? '任务处理失败,暂无可用播放源' : '播放源暂不可用,文件可能仍在处理'}
          />

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
                <div className="summary-body">{renderMiniMarkdown(task.summary.content)}</div>
              </div>
            ) : (
              <div className="empty card">
                <Icon name="wand" size="lg" />
                <b>还没有摘要</b>
                <p>摘要由 LLM 基于转写生成,生成后可以在这里直接阅读。</p>
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

// 摘要是 Markdown:这里只渲染原型 summaryHTML 覆盖的子集(段落 / "- " 列表 / **加粗** / # 标题)。
function renderMiniMarkdown(md: string) {
  const blocks: React.ReactNode[] = []
  let listItems: string[] = []
  const flushList = (key: string) => {
    if (listItems.length === 0) return
    const items = listItems
    blocks.push(<ul key={key}>{items.map((item, i) => <li key={i}>{mdInline(item)}</li>)}</ul>)
    listItems = []
  }
  md.split('\n').forEach((raw, i) => {
    const ln = raw.trim()
    if (!ln) { flushList(`ul-${i}`); return }
    if (ln.startsWith('- ')) { listItems.push(ln.slice(2)); return }
    flushList(`ul-${i}`)
    if (/^#{1,6}\s/.test(ln)) {
      blocks.push(<p key={i}><b>{mdInline(ln.replace(/^#{1,6}\s/, ''))}</b></p>)
    } else {
      blocks.push(<p key={i}>{mdInline(ln)}</p>)
    }
  })
  flushList('ul-end')
  return blocks
}

function mdInline(s: string): React.ReactNode[] {
  return s.split(/\*\*(.+?)\*\*/g).map((part, i) => (i % 2 === 1 ? <b key={i}>{part}</b> : part))
}
