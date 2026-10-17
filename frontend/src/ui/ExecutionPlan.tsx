import type { ActivitySummary } from "../contract/types"

export function ExecutionPlan({ activity, completed }: { activity: ActivitySummary | undefined; completed: boolean }) {
  const plan = activity?.plan
  const calls = activity?.model_calls ?? 0
  if (!plan && calls === 0) return null
  return (
    <div className="execution-plan">
      <div className="plan-heading">
        <span>{plan ? "执行计划" : "已记录的执行过程"}</span>
        <span className="plan-counts">模型调用 {calls} 次{activity?.model_submissions != null ? ` · 实际发送 ${activity.model_submissions} 次` : ""}</span>
      </div>
      {plan ? <ol className="plan-items" aria-label="当前执行计划">
        {plan.items.map((item) => {
          const status = completed && !item.id.startsWith("question:") ? "done" : item.status
          const label = status === "done" ? "已完成" : status === "active" ? "进行中" : status === "deferred" ? "留作限制说明" : "待执行"
          return <li key={item.id} data-state={status}>
            <span className="plan-mark" aria-hidden="true">{status === "done" ? "✓" : status === "active" ? "○" : "·"}</span>
            <span>{item.title}{item.detail ? <small>{item.detail}</small> : null}</span>
            <span className="plan-status">{label}</span>
          </li>
        })}
      </ol> : null}
      {plan?.sections?.length ? <details className="plan-chapters">
        <summary>章节进度 · {plan.sections.filter((item) => item.status === "done").length} / {plan.sections.length} 已保存</summary>
        <ol className="plan-items">{plan.sections.map((item) => <li key={item.id} data-state={item.status}>
          <span className="plan-mark" aria-hidden="true">{item.status === "done" ? "✓" : item.status === "active" ? "○" : "·"}</span>
          <span>{item.title}</span><span className="plan-status">{item.status === "done" ? "已保存" : item.status === "active" ? "接下来写作" : "待写作"}</span>
        </li>)}</ol>
      </details> : null}
      {plan?.note && !completed ? <p className="plan-note" role="status">{plan.note}</p> : null}
    </div>
  )
}
