import type { ActivitySummary } from "../contract/types"

export function ExecutionPlan({ activity, completed }: { activity: ActivitySummary | undefined; completed: boolean }) {
  const plan = activity?.plan
  const calls = activity?.model_calls ?? 0
  if (!plan && calls === 0 && !activity?.attempts?.length) return null
  return (
    <div className="execution-plan">
      <div className="plan-heading">
        <span>{plan ? "执行计划" : "已记录的执行过程"}</span>
        <span className="plan-counts">模型调用 {calls} 次{activity?.model_submissions != null ? ` · 实际发送 ${activity.model_submissions} 次` : ""}</span>
      </div>
      {activity?.attempts?.length ? <div className="attempt-ledger" aria-label="历次尝试">
        {activity.attempts.map((attempt, index) => <p key={attempt.run_id} data-current={attempt.current || undefined}>
          第 {index + 1} 次尝试{attempt.current ? "（当前）" : ""} · {attempt.saved_sections} / {attempt.planned_sections} 章已保存{attempt.inherited_sections ? `（继承 ${attempt.inherited_sections} 章）` : ""} · 调用 {attempt.model_calls} 次{attempt.current ? "" : ` · ${["completed", "succeeded"].includes(attempt.status) ? "已完成" : attempt.status === "failed" ? "未完成" : attempt.status === "canceled" ? "已停止" : "处理中"}`}
        </p>)}
        {activity.attempts.length > 1 ? <p>累计模型调用 {activity.total_model_calls} 次 · 实际发送 {activity.total_model_submissions} 次</p> : null}
      </div> : null}
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
