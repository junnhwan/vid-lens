import type { ActivitySummary } from "../contract/types"
import { PHASES, phaseMarks } from "../app/progress"

type PhaseMode = "running" | "done" | "stopped"

interface RouteItem {
  id: string
  title: string
  detail?: string
  status: string
}

function statusLabel(status: string): string {
  if (status === "done") return "已完成"
  if (status === "active" || status === "current") return "进行中"
  if (status === "deferred") return "留作限制说明"
  if (status === "stopped") return "已停止"
  return "待执行"
}

function attemptLabel(status: string): string {
  if (status === "completed" || status === "succeeded") return "已完成"
  if (status === "failed") return "未完成"
  if (status === "canceled") return "已停止"
  return "处理中"
}

export function ExecutionPlan({
  activity,
  completed,
  phase = null,
  phaseMode = "running",
}: {
  activity: ActivitySummary | undefined
  completed: boolean
  phase?: string | null
  phaseMode?: PhaseMode
}) {
  const plan = activity?.plan
  const calls = activity?.model_calls ?? 0
  const attempts = activity?.attempts ?? []
  if (!plan && calls === 0 && !attempts.length && !phase) return null

  const marks = phaseMarks(phase, phaseMode)
  const items: RouteItem[] = plan
    ? plan.items.map((item) => ({
        id: item.id,
        title: item.title,
        detail: item.detail,
        status: completed && !item.id.startsWith("question:") ? "done" : item.status,
      }))
    : phase
      ? PHASES.map((item, index) => ({ id: item.id, title: item.label, status: marks[index] }))
      : []
  const saved = plan?.sections?.filter((item) => item.status === "done").length ?? 0
  const live = !completed && phaseMode === "running"

  return (
    <section className="reading-route" data-live={live ? "true" : undefined} aria-label="阅读路线">
      <header className="route-head">
        <div>
          <p className="route-kicker">{plan ? "阅读路线" : phase ? "执行阶段" : "执行记录"}</p>
          <p className="route-caption">
            {plan
              ? completed
                ? "这次阅读的完整路线。"
                : "Agent 正在按这条路线读片。"
              : phase
                ? "执行计划生成前，先按六个阶段推进。"
                : "这次执行留下的调用记录。"}
            {live ? (
              <span className="agent-dots" aria-hidden="true">
                <i />
                <i />
                <i />
              </span>
            ) : null}
          </p>
        </div>
        <p className="route-count">
          模型调用 {calls} 次{activity?.model_submissions != null ? ` · 实际发送 ${activity.model_submissions} 次` : ""}
        </p>
      </header>
      {attempts.length > 1 ? (
        <div className="route-attempts" aria-label="历次尝试">
          {attempts.map((attempt, index) => (
            <span key={attempt.run_id} data-current={attempt.current || undefined}>
              第 {index + 1} 次尝试{attempt.current ? "（当前）" : ""} · {attemptLabel(attempt.status)} · {attempt.saved_sections} /{" "}
              {attempt.planned_sections} 章{attempt.inherited_sections ? `（继承 ${attempt.inherited_sections} 章）` : ""} · 调用{" "}
              {attempt.model_calls} 次
            </span>
          ))}
          <p>
            累计模型调用 {activity?.total_model_calls ?? 0} 次 · 实际发送 {activity?.total_model_submissions ?? 0} 次
          </p>
        </div>
      ) : null}
      {items.length ? (
        <ol className="route-items" aria-label={plan ? "当前执行计划" : "处理阶段"}>
          {items.map((item, index) => (
            <li key={item.id} data-state={item.status}>
              <span className="route-mark" aria-hidden="true">
                {String(index + 1).padStart(2, "0")}
              </span>
              <span className="route-copy">
                <span className="route-title">{item.title}</span>
                {item.detail ? <small>{item.detail}</small> : null}
              </span>
              <span className="route-state">{statusLabel(item.status)}</span>
            </li>
          ))}
        </ol>
      ) : null}
      {plan?.sections?.length ? (
        <details className="route-chapters">
          <summary>
            <span>章节进度</span>
            <span className="chapter-count">
              {saved} / {plan.sections.length} 已保存
            </span>
            <span className="chapter-ticks" aria-hidden="true">
              {plan.sections.map((item) => (
                <i key={item.id} data-state={item.status} />
              ))}
            </span>
          </summary>
          <ol className="chapter-list">
            {plan.sections.map((item) => (
              <li key={item.id} data-state={item.status}>
                <span className="chapter-mark" aria-hidden="true" />
                <span className="chapter-title">{item.title}</span>
                <span className="chapter-state">
                  {item.status === "done" ? "已保存" : item.status === "active" ? "接下来写作" : "待写作"}
                </span>
              </li>
            ))}
          </ol>
        </details>
      ) : null}
      {plan?.note && !completed ? (
        <p className="route-note" role="status">
          {plan.note}
        </p>
      ) : null}
    </section>
  )
}
