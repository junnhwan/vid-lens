import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { api, ApiError, getToken } from '@/lib/api'
import type { OptionalCapabilities } from '@/lib/types'
import { useToast } from '@/components/Toast'
import { ErrorState, LoadingBlock } from '@/components/ui/AsyncState'

const reasons: Record<string, string> = {
  rag_disabled: '服务端尚未开启视频检索。',
  ai_profile_required: '请先选择默认 AI 服务，再开启重排。',
  rerank_not_configured: '服务端尚未配置重排能力。',
  rerank_connection_unavailable: '当前 AI 服务没有可用的重排接口配置。',
}

const labels:Record<string,string>={llm:'对话模型',asr:'语音识别',embedding:'向量模型',vision:'视觉理解',ocr:'本地 OCR',alignment:'句子对齐',rerank:'检索重排',ffmpeg:'媒体处理',url_import:'URL 导入'}
const healthLabels={unchecked:'未探测',checked_ok:'小样本通过',selfcheck_ok:'依赖自检通过',failed:'检查失败'}
const dependencyReasons:Record<string,string>={deployment_disabled:'此部署未启用',dependency_missing:'工具未安装或命令不可执行',ffprobe_missing:'FFprobe 未安装或与 FFmpeg 路径不匹配',ocr_language_missing:'所选 OCR 语言包未安装',ocr_languages_unavailable:'无法读取 OCR 语言包',dependencies_missing:'Python 推理依赖未安装',model_missing:'本地模型未安装',model_manifest_missing:'模型版本清单未生成',model_manifest_stale:'权重已变化，请重新生成版本清单',model_revision_required:'模型缺少固定 revision',runtime_version_mismatch:'推理依赖版本不匹配',selfcheck_failed:'依赖自检失败或超时'}

export function OptionalCapabilitiesSection({ readOnly, refreshKey = 0 }: { readOnly: boolean; refreshKey?: number }) {
  const toast = useToast()
  const client=useQueryClient()
  const queryKey=['ai-action-availability',getToken()]
  const query=useQuery({queryKey,queryFn:()=>api.optionalCapabilities(),retry:false})
  const view=query.data
  const error=query.error ? query.error instanceof ApiError ? query.error.message : '可选能力加载失败' : ''
  const setView=(value:OptionalCapabilities)=>client.setQueryData(queryKey,value)
  const [saving, setSaving] = useState(false)
  const load=()=>query.refetch()
  useEffect(()=>{if(refreshKey) void client.invalidateQueries({queryKey:['ai-action-availability',getToken()]})},[client,refreshKey])

  const toggle = async () => {
    if (!view || readOnly || saving) return
    setSaving(true)
    try {
      const next = await api.setRerankEnabled(!view.rerank_enabled)
      setView(next)
      toast.success(next.rerank_enabled ? '检索重排已开启，对之后的新请求生效' : '检索重排已关闭')
    } catch (e) { toast.error(e instanceof ApiError ? e.message : '保存失败，原选择已保留') }
    finally { setSaving(false) }
  }

  return <section className="card" style={{ padding: 18, marginBottom: 24 }} aria-label="可选增强能力">
    <h3 style={{ marginBottom: 12 }}>可选增强能力</h3>
    {error ? <ErrorState message={error} onRetry={() => void load()} /> : !view ? <LoadingBlock label="正在读取可选能力…" /> : <>
      <div style={{ display: 'flex', gap: 16, justifyContent: 'space-between', alignItems: 'center' }}>
        <div><b>检索重排</b><p className="muted">对召回的片段进一步排序。默认关闭，开启后用于问答、Agent 与检索测试。</p></div>
        <button type="button" role="switch" aria-label="检索重排" aria-checked={view.rerank_enabled} className={`btn btn-sm ${view.rerank_enabled ? 'btn-primary' : ''}`} disabled={readOnly || saving || !view.rerank_available && !view.rerank_enabled} onClick={() => void toggle()}>{saving ? '保存中…' : view.rerank_enabled ? '已开启' : '已关闭'}</button>
      </div>
      <p className="muted">{view.rerank_available ? view.rerank_mode === 'model' ? `重排模型：${view.rerank_model}。使用当前 AI 服务的重排接口，可能增加耗时及 API 费用；开启不代表已经通过服务商调用验证。` : '当前使用本地规则排序，无需下载模型，也不会调用额外的重排 API。' : reasons[view.rerank_reason || ''] || '当前重排配置不可用。'}</p>
      {view.capabilities && <details className="disclosure" style={{ marginTop:16 }}>
        <summary>本次操作的能力状态</summary><div className="disclosure-body plain">
          <p>配置完整不代表真实服务健康。依赖自检只核对本地安装，不加载模型或下载权重；小样本健康状态来自手动能力检查。</p>
          <button className="btn btn-sm" onClick={() => void load()}>刷新能力状态</button>
          {view.capabilities.map(state => <p key={state.key}>
            <b>{labels[state.key] || state.key}</b> · {state.configured ? '已配置' : '未配置'} · {state.deployment_enabled ? '部署允许' : '部署关闭'}
            {state.user_enabled !== null ? state.user_enabled ? ' · 个人已开启' : ' · 个人已关闭' : ''}
            {' · '}{state.available ? '允许提交，执行时检查' : '当前不可用'} · {healthLabels[state.health]}
            {state.checked_at && <> · {new Date(state.checked_at).toLocaleString()}</>}
            {state.version && <> · 版本 {state.version}</>}
            {state.reason_code && dependencyReasons[state.reason_code] && <><br />{dependencyReasons[state.reason_code]}</>}
            {state.install_url && <> · <a href={state.install_url} target="_blank" rel="noreferrer">安装与自检说明</a></>}
          </p>)}
        </div>
      </details>}
      <div style={{ borderTop: '1px solid var(--border)', marginTop: 16, paddingTop: 16 }}>
        <b>精确回放定位 · 按视频开启</b>
        <p className="muted">普通转写不会运行本地对齐模型。需要逐句定位时，在视频的转写栏点击“对齐句子时间”；已经保存的精确时间继续可用。</p>
        <p className="muted">{view.alignment_configured ? '服务端已配置对齐器。首次对齐需要可用的 Python 环境与模型；未安装或运行失败时，已有转写仍可阅读。' : '服务端尚未配置对齐器。普通转写与片段回放可以正常使用，需要精确定位时由部署者安装并配置对齐环境。'}</p>
      </div>
      {readOnly && <small className="muted">演示账号仅可查看这些设置。</small>}
    </>}
  </section>
}
