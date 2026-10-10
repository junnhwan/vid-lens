import { TranscriptPanel } from '@/components/video/TranscriptPanel'
import { VisualEvidencePanel, groupVisualAtoms } from '@/components/video/VisualEvidencePanel'
import { RetrievalIndexPanel } from '@/components/video/RetrievalIndexPanel'
import { artifactError } from '@/lib/artifacts/api'
import { useVideoActions } from '@/components/video/useVideoActions'
import { taskVisualMode, visualModeLabel, visualChoices, indexActionLabel, indexConfirm } from '@/components/video/videoView'
import { useVideoWorkbenchData } from '@/components/video/useVideoWorkbenchData'
import { useVideoPlayback } from '@/components/video/useVideoPlayback'
import { studyAvailability, visualAvailability } from '@/lib/taskCapabilities'
import { ArtifactCreateDialog } from '@/components/artifacts/ArtifactCreateDialog'
import Link from '@/lib/router'

import { useEffect, useMemo, useState } from 'react'
import { useRouter } from '@/lib/router'
import {
  TaskStatusEnum,
} from '@/lib/types'
import { taskTitle } from '@/lib/format'
import { needsCitationUpgrade } from '@/components/Citation'
import { VideoPlayer } from '@/components/player/VideoPlayer'
import { SummaryWorkspace } from '@/components/summary/SummaryWorkspace'
import { SummaryRevisionPanel } from '@/components/summary/SummaryRevisionPanel'
import { VideoQuestionsPanel } from '@/components/chat/VideoQuestionsPanel'
import { useCrumb, useShell } from '@/components/shell/AppShell'
import { useToast } from '@/components/Toast'
import { Icon } from '@/components/ui/Icon'
import { ConfirmModal, Modal } from '@/components/ui/Modal'
import KBModal from '@/components/KBModal'
import { groupTranscriptSources } from '@/lib/transcript'
import { ProcessStrip } from '@/components/ProcessStrip'
import { TranscriptionProgressPanel } from '@/components/TranscriptionProgressPanel'
import { VisualProgressPanel } from '@/components/VisualProgressPanel'
import { summaryFailureView } from '@/lib/summaryFailure'
import { canGenerateSummary, summaryRunning, summaryStatusText } from '@/lib/summaryState'
import { LoadingBlock, ErrorState } from '@/components/ui/AsyncState'
import './VideoWorkbench.css'

// 视频工作台:播放器钉住 + 右栏时间轴/画面/索引。摘要走阅读弹窗。
// 对应原型 #/video/:id。播放源用 /playback 签名 URL;时间轴/画面证据来自
// /timeline 的原子(转写、OCR、画面描述同轨)。画面卡片读取对应已保存帧；
// 点击卡片仍按该帧时间跳转播放器。

type TabKey = 'tl' | 'vf' | 'idx'
function VideoWorkbench({ params, searchParams }: { params: { id: string }; searchParams?: { t?: string; citations?: string } }) {
  const [summaryMode,setSummaryMode]=useState(searchParams?.citations !== 'upgrade')
  const [artifactMode, setArtifactMode] = useState<'new' | 'reorganize' | null>(null)
  const taskId = Number(params.id)
  const router = useRouter()
  const toast = useToast()
  const { user } = useShell()
  const readOnly = user?.role === 'DEMO'
  const data = useVideoWorkbenchData(taskId, !!user)
  const { task, timeline, index, playbackUrl, transcriptionProgress, relatedArtifacts, loading, loadError, subError, refreshPlaybackUrl } = data
  const { playerRef, playheadMs, videoDurationMs, setVideoDurationMs, headSnap, seek, onPlayhead, rowAtPlayhead, studyError } = useVideoPlayback(taskId)
  const { ai, videoPreflight, busy, titleBusy,titleDraft,setTitleDraft,editingTitle,setEditingTitle,visualSettingBusy,visualBuildBusy,visualDraft,setVisualDraft,visualSettingsOpen,setVisualSettingsOpen,pendingAction,setPendingAction,runAction,downloadMedia,saveTitle,saveVisualMode,confirmAndRun } = useVideoActions(data, readOnly)
  const [tab, setTab] = useState<TabKey>('tl')
  const [seen, setSeen] = useState<Record<TabKey, boolean>>({ tl: true, vf: false, idx: false })
  const openTab = (key: TabKey) => {
    setTab(key)
    setSeen(s => (s[key] ? s : { ...s, [key]: true }))
  }
  const [summaryOpen, setSummaryOpen] = useState(false)
  const [questionsOpen, setQuestionsOpen] = useState(false)
  const [moreOpen, setMoreOpen] = useState(false)
  const [dialogInstant, setDialogInstant] = useState(false)
  const [kbOpen, setKbOpen] = useState(false)

  useCrumb([
    { label: '视频库', href: '/library' },
    { label: task ? taskTitle(task) : `视频 #${taskId}` },
  ])

  const processing = task?.status === TaskStatusEnum.Queued || task?.status === TaskStatusEnum.Running
  const generatingSummary = !!task && summaryRunning(task)
  const visualProcessing = !!task && ['queued', 'running'].includes(task.visual_status)
  const readableArtifact = relatedArtifacts.data?.list.find(item => !!item.current_version_id)
  const pendingArtifact = relatedArtifacts.data?.list.find(item => !!item.latest_run && ['queued', 'running'].includes(item.latest_run.status))
  const transcriptAtoms = useMemo(
    () => (timeline?.atoms || []).filter(a => a.modality === 'transcript'),
    [timeline],
  )
  const transcriptRows = useMemo(() => groupTranscriptSources(transcriptAtoms), [transcriptAtoms])
  const citationUpgradeAvailable = transcriptAtoms.some(atom => needsCitationUpgrade({ modality: atom.modality, startMS: atom.start_ms, endMS: atom.end_ms, timeRangeStatus: atom.time_range_status }))
  useEffect(() => {
    if (searchParams?.citations !== 'upgrade' || !timeline) return
    setTab('tl')
    const section = document.getElementById('transcript')
    section?.scrollIntoView?.({ block: 'nearest' })
  }, [searchParams?.citations, timeline])
  const visualAtoms = useMemo(
    () => (timeline?.atoms || []).filter(a => a.modality === 'visual_ocr' || a.modality === 'visual_caption'),
    [timeline],
  )
  const studyCapability = studyAvailability(task, timeline)
  const visualCapability = task ? visualAvailability(task) : { ready: false, reason: '正在读取视频' }
  const frames = useMemo(() => groupVisualAtoms(timeline?.atoms || []), [timeline])
  const timelineMs = useMemo(
    () => (timeline?.atoms || []).reduce((max, a) => Math.max(max, a.end_ms, a.start_ms), 0),
    [timeline],
  )

  const exactLiveIndex = transcriptRows.findIndex(a => a.time_range_status === 'exact' && rowAtPlayhead(a))
  const liveIndex = exactLiveIndex >= 0 ? exactLiveIndex : transcriptRows.findIndex(rowAtPlayhead)

  const openVisualSettings = (event?: { detail: number }) => {
    if (!task) return
    setDialogInstant(event?.detail === 0)
    setVisualDraft(taskVisualMode(task))
    setVisualSettingsOpen(true)
  }

  if (loading) {
    return <div className="page"><LoadingBlock label="正在加载…" variant="card" /></div>
  }
  if (loadError || !task) {
    return (
      <div className="page">
        <ErrorState message={loadError || '视频加载失败'} onRetry={() => void data.refreshTask()} />
      </div>
    )
  }

  if(summaryMode) return <><SummaryWorkspace key={task.id} task={task} readOnly={readOnly} playbackUrl={playbackUrl} playerRef={playerRef} onPlayhead={onPlayhead} onDuration={setVideoDurationMs} onSeek={seek} refreshPlaybackUrl={refreshPlaybackUrl} onChanged={data.refreshAfterAction} onGenerate={()=>runAction('analyze',task.has_summary)} onTechnical={()=>setSummaryMode(false)} busy={busy!==''} indexed={!!index?.indexed} initialTimeMS={searchParams?.t?Number(searchParams.t):undefined} />{videoPreflight.dialog}</>

  const failed = task.status === TaskStatusEnum.Failed || task.status === TaskStatusEnum.Dead
  const transcriptionRelevant = task.stage === 'transcribing' || task.stage === 'aligning' || task.stage === 'visual_indexing' || task.last_job_type === 'transcribe'
  const alignmentIncomplete = !!transcriptionProgress?.alignment_only && failed && task.last_job_type === 'transcribe'
  const transcriptionIncomplete = !transcriptionProgress?.alignment_only && transcriptionRelevant && (!!transcriptionProgress && transcriptionProgress.completed < transcriptionProgress.total || failed && task.last_job_type === 'transcribe')
  const resumeTranscription = !processing && transcriptionIncomplete
  const checkingTranscription = transcriptionRelevant && !transcriptionProgress
  const citationUpgradeLabel = resumeTranscription ? '重试补齐引用定位' : '补齐引用定位'
  const resumeTranscriptionBody = '会保留已完成的转写分片，继续处理未完成部分，再更新转写与检索索引，可能产生 ASR 和 Embedding 费用。全部完成前仍使用之前保存的内容与引用。'
  const summaryFailure = summaryFailureView(task)
  const urlJob = failed && task.last_job_type === 'download'
  const title = taskTitle(task)


  return (
    <div className="page-fill video-workbench">
      {artifactMode && <ArtifactCreateDialog source={{ id: task.id, title }} existing={artifactMode === 'reorganize' && readableArtifact ? { id: readableArtifact.id, title: readableArtifact.title, head_version: readableArtifact.head_version } : undefined} onClose={() => { setArtifactMode(null); void relatedArtifacts.refetch() }} />}
      <div className="ws">
        <div className="ws-stage">
          <div className="ws-heading"><button className="btn btn-sm" onClick={()=>setSummaryMode(true)}>返回摘要</button>
            {editingTitle ? (
              <form
                className="ws-title-form"
                onSubmit={e => { e.preventDefault(); void saveTitle() }}
              >
                <input
                  className="input"
                  value={titleDraft}
                  maxLength={60}
                  autoFocus
                  onChange={e => setTitleDraft(e.target.value)}
                  onKeyDown={e => { if (e.key === 'Escape') setEditingTitle(false) }}
                />
                <button className="btn btn-sm btn-primary" type="submit" disabled={titleBusy}>保存</button>
                <button className="btn btn-sm" type="button" onClick={() => setEditingTitle(false)}>取消</button>
              </form>
            ) : (
              <>
                <h2>{title}</h2>
                {!readOnly && (
                  <button
                    className="btn btn-ic btn-ghost"
                    aria-label="编辑标题"
                    title="编辑标题"
                    onClick={() => { setTitleDraft(task.title || task.filename || ''); setEditingTitle(true) }}
                  >
                    <Icon name="pencil" size="sm" />
                  </button>
                )}
              </>
            )}
          </div>
          {subError && (
            <div className="card card-pad" style={{ marginTop: 10, marginBottom: 0, display: 'flex', alignItems: 'center', gap: 10 }}>
              <span style={{ color: 'var(--bad)', display: 'flex' }}><Icon name="alert" /></span>
              <b style={{ flex: 1, fontSize: 13 }}>{subError}</b>
              <button className="btn btn-sm" onClick={() => void data.refreshEvidence()}>
                <Icon name="refresh" size="sm" />重试
              </button>
            </div>
          )}
          <VideoPlayer
            key={`${taskId}-${searchParams?.t || '0'}`}
            initialTimeMs={searchParams?.t ? Number(searchParams.t) : undefined}
            ref={playerRef}
            src={playbackUrl}
            title={title}
            onPlayhead={onPlayhead}
            onDuration={setVideoDurationMs}
            onNeedRefresh={refreshPlaybackUrl}
            fallbackText={failed ? '任务处理失败,暂无可用播放源' : '播放源暂不可用,文件可能仍在处理'}
          />
          <p className="workbench-capability" role="status">{task.has_transcription ? processing && transcriptionRelevant ? '正在更新转写与引用定位，完成前仍使用之前保存的内容。' : alignmentIncomplete ? '句子时间对齐未完成，之前保存的转写仍可查看。' : transcriptionIncomplete ? '本次转写尚未完成，当前仍使用之前保存的转写与引用定位。' : checkingTranscription ? '正在核对本次转写进度，已保存的内容仍可查看。' : index?.indexed ? '转写与检索已就绪，可提问并核对引用。' : '转写可阅读；快速问答可使用摘要或转写，检索引用尚未就绪。' : studyCapability.ready ? '画面内容可整理笔记；画面问答需建立检索索引。' : visualCapability.ready ? '视频已导入：讲解视频可开始转写，无声演示可直接分析画面。' : visualCapability.reason}</p>
          {!readOnly && !ai.ready && <div className="artifact-notice" role="status"><span>{ai.reason}</span>{ai.error ? <button className="btn btn-sm" onClick={() => void ai.refetch()}>重试</button> : <a className="btn btn-sm" href="/settings" target="_blank" rel="noopener noreferrer">配置 AI</a>}</div>}
          <section className="workbench-commands" aria-label="视频学习操作">
            <div className="workbench-primary">
              <button className="btn btn-primary workbench-chat-action" onClick={() => router.push(`/chat/v/${task.id}`)}><Icon name="message" size="sm" />进入问答<Icon name="chev-r" size="sm" /></button>
              {readableArtifact ? <Link className="btn" href={`/artifacts/${encodeURIComponent(readableArtifact.id)}`}><Icon name="file" size="sm" />阅读学习笔记</Link> : !task.has_transcription && !studyCapability.ready && !readOnly ? <button className="btn" disabled={readOnly || busy !== '' || processing} onClick={() => runAction('transcribe')}><Icon name="activity" size="sm" />{processing ? '转写处理中' : '开始转写'}</button> : pendingArtifact ? <Link className="btn" href={`/tasks?run=${encodeURIComponent(pendingArtifact.latest_run!.id)}`}><Icon name="clock" size="sm" />查看笔记进度</Link> : <button className="btn" disabled={readOnly || !studyCapability.ready || relatedArtifacts.isPending || !!relatedArtifacts.error} title={studyCapability.reason || undefined} onClick={() => setArtifactMode('new')}><Icon name="wand" size="sm" />新建学习笔记</button>}
              {task.has_summary ? (
                <button className="btn" onClick={() => setSummaryOpen(true)}><Icon name="eye" size="sm" />查看摘要</button>
              ) : (
                <button
                  className={`btn${busy === 'analyze' ? ' is-loading' : ''}`}
                  aria-busy={busy === 'analyze' || undefined}
                  disabled={readOnly || busy !== '' || !canGenerateSummary(task)}
                  title={!task.has_transcription ? '转写完成后才能生成摘要' : generatingSummary ? '摘要正在生成，请勿重复提交' : !canGenerateSummary(task) ? '等待本次转写完成或摘要重试结束' : undefined}
                  onClick={() => void runAction('analyze')}
                ><Icon name="file" size="sm" />{busy === 'analyze' ? '提交中…' : generatingSummary ? summaryStatusText(task) : '生成摘要'}</button>
              )}
            </div>
            <div className="workbench-secondary">
              <button type="button" onClick={event => { setDialogInstant(event.detail === 0); videoPreflight.request('生成视频推荐问题', () => setQuestionsOpen(true), 'summary') }}><Icon name="bulb" size="sm" />推荐问题</button>
              <button type="button" onClick={openVisualSettings}><Icon name="photo" size="sm" />画面分析<span className="workbench-mode">{visualModeLabel(taskVisualMode(task))}</span></button>
              <button type="button" className="workbench-more" onClick={event => { setDialogInstant(event.detail === 0); setMoreOpen(true) }}><Icon name="settings" size="sm" />更多操作<Icon name="chev-r" size="sm" /></button>
            </div>
          </section>
          <div className="ws-stage-scroll">
            {processing && (
              <div style={{ marginTop: 12, flex: 'none' }}>
                <ProcessStrip status={task.status} stage={task.stage} has_transcription={task.has_transcription} last_job_type={task.last_job_type} has_rag_index={task.has_rag_index} visual_status={task.visual_status} />
              </div>
            )}
            {transcriptionRelevant && <TranscriptionProgressPanel task={task} resource={data.progressQuery} />}
            {!transcriptionProgress?.alignment_only && (visualProcessing || taskVisualMode(task) !== 'off' && (task.stage === 'transcribing' || task.stage === 'visual_indexing' || task.last_job_type === 'transcribe')) && <VisualProgressPanel resource={data.visualQuery} task={task} />}
            {processing && canGenerateSummary(task) && <p className="muted" role="status">转写已保存，可以生成摘要；画面分析和检索索引会继续处理。</p>}
            {studyError && <div className="artifact-notice" role="status">{studyError}</div>}

            {relatedArtifacts.error && <div className="artifact-notice danger" role="alert">相关笔记读取失败：{artifactError(relatedArtifacts.error)}<button className="btn btn-sm" onClick={() => void relatedArtifacts.refetch()}>重试</button></div>}
            {((!task.has_summary && generatingSummary) || (!task.has_summary && task.summary_progress)) && (
              <div className="ws-action-status">
                {!task.has_summary && generatingSummary && (
                  <span className="muted" style={{ fontSize: 12 }} role="status">{summaryStatusText(task)}，完成后会显示在这里</span>
                )}
                {!task.has_summary && task.summary_progress && (
                  <span className="muted" style={{ fontSize: 12 }} role="status">
                    摘要{task.summary_progress.phase === 'merging' ? '合并总结' : '分段处理'}：{task.summary_progress.completed}/{task.summary_progress.total}
                    {task.summary_progress.current > 0 ? ` · 第 ${task.summary_progress.current} 段${task.summary_progress.end_ms > task.summary_progress.start_ms ? `（${Math.floor(task.summary_progress.start_ms / 1000)}–${Math.ceil(task.summary_progress.end_ms / 1000)} 秒）` : '（时间未记录）'}` : ''}
                    {task.summary_progress.failed_part ? task.summary_progress.phase === 'merging' ? ` · 第 ${task.summary_progress.failed_part} 组合并失败，完整总结尚未生成` : ` · 第 ${task.summary_progress.failed_part} 段失败，尚未覆盖全片` : ''}
                  </span>
                )}
              </div>
            )}

            {(failed || summaryFailure) && (
              <div className="card card-pad" style={{ marginTop: 14, flex: 'none', borderColor: 'color-mix(in srgb, var(--bad) 35%, transparent)' }}>
                <div style={{ display: 'flex', gap: 10, alignItems: 'flex-start' }}>
                  <span style={{ color: 'var(--bad)' }}><Icon name="alert" /></span>
                  <div style={{ flex: 1 }}>
                    <b style={{ fontSize: 13 }}>{summaryFailure?.category || (task.status === TaskStatusEnum.Dead ? '任务已废弃' : '处理失败')}</b>
                    {summaryFailure ? <>
                      <p style={{ fontSize: 12, color: 'var(--tx-3)', marginTop: 4 }}>{summaryFailure.retry}</p>
                      <p style={{ fontSize: 12, color: 'var(--tx-3)', marginTop: 4 }}>{summaryFailure.advice}</p>
                      <button className="btn btn-sm" style={{ marginTop: 8 }} onClick={() => {
                        void navigator.clipboard.writeText(summaryFailure.diagnosticId).then(() => toast.success('诊断编号已复制')).catch(() => toast.error('复制失败，请手动复制编号'))
                      }}>诊断编号：{summaryFailure.diagnosticId} · 复制</button>
                    </> : <p style={{ fontSize: 12, color: 'var(--tx-3)', marginTop: 4 }}>
                      {task.error_msg || task.last_error_msg || '处理过程中出现错误'}
                      {task.max_retries > 0 ? ` · 重试 ${task.retry_count}/${task.max_retries}` : ''}
                    </p>}
                    {urlJob && !summaryFailure ? (
                      <span className="chip chip-mute" style={{ marginTop: 10 }}>URL 任务,请删除后重新添加</span>
                    ) : !summaryFailure?.scheduled && (
                      <button
                        className="btn btn-sm"
                        style={{ marginTop: 10 }}
                        disabled={busy !== '' || readOnly}
                        onClick={() => task.last_job_type === 'visual' ? openVisualSettings() : setPendingAction({
                          kind: summaryFailure || task.last_job_type === 'analyze' ? 'analyze' : task.last_job_type === 'index' ? 'index' : transcriptionProgress?.alignment_only ? 'align' : 'transcribe',
                          title: '重新提交任务?',
                          body: summaryFailure ? `${summaryFailure.advice} 重新提交可能再次消耗模型额度。` : '失败步骤会重新入队,可能再次消耗模型额度。',
                          confirmLabel: '重试',
                        })}
                      >
                        重试
                      </button>
                    )}
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>

        <div className="card ws-rail">
          <div className="rail-tabs" style={{ padding: '10px 14px 0' }}>
            {([['tl', '转写时间轴'], ['vf', '画面证据'], ['idx', '检索索引']] as const).map(([key, label]) => (
              <button key={key} className={`rail-tab${tab === key ? ' on' : ''}`} onClick={() => openTab(key)}>
                {label}
              </button>
            ))}
          </div>
          <div className="rail-body">
            <div className={`rail-pane${tab === 'tl' ? ' on' : ''}`}>{seen.tl ? <TranscriptPanel task={task} transcriptAtoms={transcriptAtoms} transcriptRows={transcriptRows} visualAtoms={visualAtoms} timelineMs={timelineMs} playheadMs={playheadMs} headSnap={headSnap} liveIndex={liveIndex} seek={seek}>
            {timeline?.alignment_available && transcriptAtoms.some(atom => atom.time_range_status !== 'exact') && (
              <div className="transcript-upgrade">
                <b>精确回放定位 · 按需开启</b>
                <p>当前可以从片段回放。需要逐句定位时，可将已有文字与视频音频对齐；此操作会运行服务端配置的本地模型，普通转写不会自动执行。</p>
                <button className="btn btn-sm" disabled={readOnly || busy !== '' || processing} onClick={() => setPendingAction({ kind: 'align', title: '对齐句子时间？', body: '会复用已有识别文字，在本地对齐音频时间并更新检索索引。对齐不会再次调用语音识别；重建索引可能产生 Embedding 费用。历史回答和引用快照保留。', confirmLabel: '开始对齐' })}>对齐句子时间</button>
              </div>
            )}
            {(citationUpgradeAvailable || resumeTranscription) && <div className="transcript-upgrade">
              <b>{resumeTranscription ? '本次引用定位补齐尚未完成' : '旧转写的引用定位可以补齐'}</b>
              <p>{resumeTranscription ? '仍在使用之前保存的转写与引用时间。重试会继续处理未完成部分，保留已完成分片。' : '当前转写只有较长的原片段时间。重新识别音频后，新回答会使用更短的语音片段定位；历史回答保留原有引用。'}</p>
              <button className="btn btn-sm" disabled={readOnly || busy !== '' || processing || checkingTranscription} onClick={() => setPendingAction({ kind: 'transcribe', force: !resumeTranscription, title: `${citationUpgradeLabel}？`, body: resumeTranscription ? resumeTranscriptionBody : '会重新识别这段视频的音频，并更新转写与检索索引，可能产生新的 ASR 和 Embedding 费用。完成后，新回答会使用更短的来源时间；历史回答和引用快照会保留。语音服务不返回句子时间时，将使用短音频片段时间。', confirmLabel: citationUpgradeLabel })}><Icon name="refresh" size="sm" />{citationUpgradeLabel}</button>
              {readOnly && <small>演示账号无法重新识别视频。</small>}
            </div>}

</TranscriptPanel> : null}</div>
            <div className={`rail-pane${tab === 'vf' ? ' on' : ''}`}>{seen.vf ? <VisualEvidencePanel task={task} frames={frames} timeline={timeline} videoDurationMs={videoDurationMs} playbackUrl={playbackUrl} openVisualSettings={openVisualSettings} seek={seek} /> : null}</div>
            <div className={`rail-pane${tab === 'idx' ? ' on' : ''}`}>{seen.idx ? <RetrievalIndexPanel index={index} busy={busy} onBuild={value=>setPendingAction(indexConfirm(value))} /> : null}</div>
          </div>
        </div>
      </div>
      {questionsOpen && (
        <Modal title="从这些问题开始" className={`workbench-dialog workbench-questions-dialog${dialogInstant ? ' workbench-instant' : ''}`} width={640} onClose={() => setQuestionsOpen(false)}>
          <VideoQuestionsPanel taskId={task.id} revision={task.updated_at} />
        </Modal>
      )}
      {visualSettingsOpen && (
        <Modal
          title="选择画面分析方式"
          className={`workbench-dialog workbench-visual-dialog${dialogInstant ? ' workbench-instant' : ''}`}
          width={580}
          onClose={() => { if (!visualSettingBusy) setVisualSettingsOpen(false) }}
          footer={(
            <>
              <button className="btn" disabled={readOnly || processing || visualProcessing || visualSettingBusy} onClick={() => void saveVisualMode()}>{visualSettingBusy && !visualBuildBusy ? '保存中…' : '保存设置'}</button>
              {visualDraft !== 'off' && <button className="btn btn-primary" disabled={readOnly || !visualCapability.ready || visualSettingBusy} onClick={() => videoPreflight.request('生成画面证据', () => { void saveVisualMode(true) }, visualDraft === 'ocr' ? 'ocr' : 'caption')}><Icon name="photo" size="sm" />{visualSettingBusy ? '提交中…' : frames.length ? '保存并重建画面' : '保存并生成画面'}</button>}
            </>
          )}
        >
          <p className="workbench-dialog-intro">补充视频声音之外的信息。按画面内容选择，转写和摘要始终可以单独使用。</p>
          <fieldset className="workbench-visual-choices" disabled={readOnly || !visualCapability.ready || visualSettingBusy}>
            <legend className="workbench-sr-only">画面分析方式</legend>
            {visualChoices.map(choice => (
              <label key={choice.mode} className={`workbench-visual-choice${visualDraft === choice.mode ? ' selected' : ''}`}>
                <input type="radio" name="visual-mode" value={choice.mode} checked={visualDraft === choice.mode} onChange={() => setVisualDraft(choice.mode)} />
                <span className="workbench-choice-icon"><Icon name={choice.icon} /></span>
                <span className="workbench-choice-copy"><strong>{choice.title}{choice.mode === 'off' && <small>默认</small>}</strong><span>{choice.description}</span></span>
                <span className="workbench-choice-check" aria-hidden="true"><Icon name="check" size="sm" /></span>
              </label>
            ))}
          </fieldset>
          <div className="workbench-visual-note">
            <Icon name="bulb" size="sm" />
            <p>{processing || visualProcessing ? '当前视频正在处理，完成后可修改画面设置。' : readOnly ? '演示账号可以查看设置，无法修改。' : '保存设置会用于下次转写及问答。已有证据会保留；生成或重建画面会单独处理视频并更新索引，无需再次转写。'}{!visualCapability.ready ? visualCapability.reason : !task.has_transcription ? '无声演示也可以直接分析画面，无需先转写。' : ''}</p>
          </div>
        </Modal>
      )}
      {moreOpen && (
        <Modal title="更多视频操作" className={`workbench-dialog${dialogInstant ? ' workbench-instant' : ''}`} width={520} onClose={() => setMoreOpen(false)}>
          <section className="workbench-more-group" aria-label="学习笔记与成果">
            <h4>学习笔记与成果</h4>
            {readableArtifact && <button className="workbench-operation" disabled={readOnly || !studyCapability.ready} title={studyCapability.reason || undefined} onClick={() => { setMoreOpen(false); setArtifactMode('new') }}><Icon name="plus" /><span><strong>新建另一份学习笔记</strong><small>为这段视频整理不同主题的笔记</small></span><Icon name="chev-r" size="sm" /></button>}
            {readableArtifact && <button className="workbench-operation" disabled={readOnly || !studyCapability.ready} title={studyCapability.reason || undefined} onClick={() => { setMoreOpen(false); setArtifactMode('reorganize') }}><Icon name="sort" /><span><strong>重新整理这份笔记</strong><small>保留已有版本，生成新的整理结果</small></span><Icon name="chev-r" size="sm" /></button>}
            <Link className="workbench-operation" href={`/artifacts?source=${task.id}`}><Icon name="list" /><span><strong>相关成果</strong><small>查看来自这段视频的笔记与产出</small></span><Icon name="chev-r" size="sm" /></Link>
          </section>
          <section className="workbench-more-group" aria-label="内容处理">
            <h4>内容处理</h4>
            {task.has_transcription ? (
              <button className="workbench-operation" disabled={readOnly || busy !== '' || processing || checkingTranscription} onClick={() => { setMoreOpen(false); setPendingAction({ kind: 'transcribe', force: !resumeTranscription, title: resumeTranscription ? '继续转写？' : '重新转写？', body: resumeTranscription ? resumeTranscriptionBody : '会清除旧分片并再次调用语音识别，可能产生新的 ASR 费用。', confirmLabel: resumeTranscription ? '继续转写' : '重新转写' }) }}><Icon name="refresh" /><span><strong>{resumeTranscription ? '继续转写' : '重新转写'}</strong><small>{resumeTranscription ? '保留已完成分片，继续处理未完成部分' : '重新识别视频音频，更新转写内容'}</small></span><Icon name="chev-r" size="sm" /></button>
            ) : <button className="workbench-operation" disabled={readOnly || busy !== '' || processing} onClick={() => { setMoreOpen(false); void runAction('transcribe') }}><Icon name="activity" /><span><strong>{processing ? '转写处理中' : '开始转写'}</strong><small>将视频声音转换为可检索的文字</small></span><Icon name="chev-r" size="sm" /></button>}
            <button className="workbench-operation" disabled={busy !== '' || !index || index.status === 'indexing' || index.status === 'queued'} onClick={() => { if (index) { setMoreOpen(false); setPendingAction(indexConfirm(index)) } }}><Icon name="layers" /><span><strong>{indexActionLabel(index)}</strong><small>更新视频问答使用的内容检索索引</small></span><Icon name="chev-r" size="sm" /></button>
          </section>
          <section className="workbench-more-group" aria-label="导出与知识库">
            <h4>导出与知识库</h4>
            <button className="workbench-operation" disabled={busy !== ''} onClick={() => { setMoreOpen(false); void downloadMedia() }}><Icon name="download" /><span><strong>下载视频</strong><small>保存原始视频文件</small></span><Icon name="chev-r" size="sm" /></button>
            <button className="workbench-operation" disabled={readOnly} onClick={() => { setMoreOpen(false); setKbOpen(true) }}><Icon name="folder" /><span><strong>加入知识库</strong><small>和其他视频一起检索与问答</small></span><Icon name="chev-r" size="sm" /></button>
          </section>
        </Modal>
      )}
      {summaryOpen && task.summary && (
        <Modal title="AI 摘要" className="modal-read" onClose={() => setSummaryOpen(false)}>
          <SummaryRevisionPanel taskId={task.id} readOnly={readOnly} onChanged={async () => { await data.refreshTask() }} />
        </Modal>
      )}
      {kbOpen && (
        <KBModal
          mode="assign"
          taskId={task.id}
          indexed={!!index?.indexed}
          onClose={() => setKbOpen(false)}
          onChanged={() => toast.success('知识库成员已更新')}
        />
      )}
      {pendingAction && (
        <ConfirmModal
          title={pendingAction.title}
          confirmLabel={pendingAction.confirmLabel}
          busy={busy !== ''}
          onClose={() => setPendingAction(null)}
          onConfirm={() => void confirmAndRun()}
        >
          {pendingAction.body}
        </ConfirmModal>
      )}
      {videoPreflight.dialog}
    </div>
  )
}

export default function VideoWorkbenchPage(props: { params: { id: string }; searchParams?: { t?: string; citations?: string } }) {
 const { user } = useShell()
 return <VideoWorkbench key={`${user?.id ?? ''}:${props.params.id}`} {...props} />
}
