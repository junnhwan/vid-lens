import type { ChatTraceStep } from '@/components/chat/traceTypes'

// 证据漏斗固定八步轨道。顺序与动作名与后端 evidence_funnel 固定序列一一对应
// （internal/service/video_evidence_funnel.go 的 action 常量），
// 非流式接口不返回逐步进度，等待期全部为空态，返回后按 trace 里的 tool 名一次性标注。

const FUNNEL_STEPS: { label: string; tool: string }[] = [
  { label: '全局摘要与元数据', tool: 'browse_video_context' },
  { label: 'transcript 检索', tool: 'search_transcript' },
  { label: 'Planner 决策 · 补哪个缺口', tool: 'select_transcript_gaps' },
  { label: '时间窗扩展', tool: 'expand_time_windows' },
  { label: '视觉 / OCR 候选', tool: 'select_visual_gaps' },
  { label: '视觉确认', tool: 'confirm_visual_ocr' },
  { label: '引用答案构建', tool: 'build_cited_answer' },
  { label: 'Evidence / Claim 校验', tool: 'validate_evidence_claims' },
]

function clip(text: string, max = 26): string {
  return text.length > max ? `${text.slice(0, max)}…` : text
}

export function FunnelTrack({ steps }: { steps: ChatTraceStep[] }) {
  const byTool = new Map<string, ChatTraceStep>()
  for (const step of steps) {
    if (step.tool && !byTool.has(step.tool)) byTool.set(step.tool, step)
  }
  return (
    <div className="funnel-track">
      {FUNNEL_STEPS.map((fixed, i) => {
        const hit = byTool.get(fixed.tool)
        const failed = hit?.status === 'error' || !!hit?.error
        return (
          <div key={fixed.tool} className={`funnel-step${hit && !failed ? ' ok' : ''}${failed ? ' err' : ''}`}>
            <span className="fn mono">{i + 1}</span>
            <span>{fixed.label}</span>
            <span className="fd mono" title={failed ? hit?.error : hit?.toolOutput}>
              {hit ? (failed ? '失败' : hit.toolOutput ? clip(hit.toolOutput) : '完成') : ''}
            </span>
          </div>
        )
      })}
    </div>
  )
}
