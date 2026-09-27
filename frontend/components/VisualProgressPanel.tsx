import { useEffect, useRef, useState } from 'react'
import { api } from '@/lib/api'
import type { VideoTask, VisualProgress } from '@/lib/types'

const phases: Record<string,string> = { waiting_for_slot:'等待视觉处理名额',provider_check:'检查 OCR 与视觉模型',downloading:'读取视频文件',extracting:'提取关键帧',observing_frames:'逐帧识别',publishing:'发布证据',published:'已发布',waiting_for_worker:'等待处理进程',disabled:'已关闭视觉分析',not_started:'尚未开始' }
const statuses: Record<string,string> = { not_started:'尚未开始',waiting_to_start:'等待新处理尝试',queued:'已排队',running:'处理中',completed:'已完成',skipped:'已跳过',failed:'处理失败',canceled:'已取消',interrupted:'处理已中断' }

export function VisualProgressPanel({ task, compact=false }: { task: VideoTask; compact?: boolean }) {
  const [progress,setProgress] = useState<VisualProgress|null>(null)
  const [error,setError] = useState(false)
  const generation = useRef(0)
  const active = task.stage === 'transcribing' && (task.status===1 || task.status===2)
  useEffect(() => {
    let live=true
    const load=async () => {
      const request=++generation.current
      try { const next=await api.getVisualProgress(task.id); if (live && request===generation.current) { setProgress(next); setError(false) } }
      catch { if (live && request===generation.current) setError(true) }
    }
    void load()
    const timer=active ? window.setInterval(() => void load(),5000) : undefined
    return () => { live=false; if (timer) window.clearInterval(timer) }
  },[task.id,active,task.status,task.stage])
  if (!progress && !error) return null
  if (error) return <div className="artifact-notice" role="status">画面进度暂不可用。<button className="btn btn-sm" onClick={() => { void api.getVisualProgress(task.id).then(value=>{setProgress(value);setError(false)}) }}>重试</button></div>
  if (!progress || (progress.status==='not_started' && !active)) return null
  const detail=progress.total_frames===null ? `已处理 ${progress.processed_frames} 帧，帧总量尚未确定` : `已处理 ${progress.processed_frames}/${progress.total_frames} 帧`
  const reason=progress.error_code==='no_visual_provider' ? '当前处理环境没有可用视觉模型或 OCR' : progress.error_code==='visual_disabled' ? '你已关闭该视频的视觉分析' : progress.error_code ? `原因：${progress.error_code}` : ''
  return <div className="card card-pad" style={{ marginTop:8,fontSize:12 }} role="status"><b>画面处理 · {statuses[progress.status] ?? progress.status}</b><div className="muted">{phases[progress.phase] ?? progress.phase} · {detail}</div>{progress.failed_frames>0 && <div className="muted">{progress.failed_frames} 帧未完成（OCR {progress.ocr_failed_frames}，视觉模型 {progress.vision_failed_frames}）</div>}{reason && <div className="muted">{reason}</div>}{!compact && ['failed','canceled','interrupted'].includes(progress.status) && <div className="muted">刷新可读取最新状态；如任务允许重试，请使用页面现有重试入口。帧处理不会从上次位置继续。</div>}</div>
}
