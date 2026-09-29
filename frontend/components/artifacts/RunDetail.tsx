import Link from '@/lib/router'
import { useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { artifactApi, artifactError } from '@/lib/artifacts/api'
import type { GenerationRun } from '@/lib/artifacts/schema'
import { errorLabels, isActiveRun, runLabels, stageLabels } from '@/lib/artifacts/view'
import { Modal } from '@/components/ui/Modal'
import { ErrorState, LoadingBlock } from '@/components/ui/AsyncState'
import { Icon } from '@/components/ui/Icon'

export function RunProgress({ run }: { run: GenerationRun }) {
  const stages = ['queued', 'collecting', 'generating', 'organizing', 'validating', 'completed']
  const current = stages.indexOf(run.status === 'completed' ? 'completed' : run.progress?.stage || run.stage)
  const stopped = !isActiveRun(run) && run.status !== 'completed'
  const reason = run.budget?.stop_reason || (run.budget && run.usage.completion_tokens >= run.budget.max_output_tokens ? 'output_tokens' : '')
  const budgetReasons: Record<string, string> = { input_tokens: '输入 Token 预算不足，未能继续处理剩余材料。', output_tokens: '输出 Token 预算不足，未能完成整份笔记。', llm_calls: '模型调用次数不足，未能完成整份笔记。', duration: '已达到生成时间上限。', context_tokens: '本次材料超过模型单次上下文容量。', attempts: '本次模型调用重试次数已用尽。' }
  return <>
    <span className={`task-state ${run.status}`}>{runLabels[run.status]}</span>
    <ol className="run-timeline">{stages.map((stage, i) => <li key={stage} aria-current={i === current ? 'step' : undefined} className={i === current ? 'current' : i < current ? 'done' : ''}>
      <span>{i < current ? <Icon name="check" size="sm" /> : i + 1}</span>
      <div>{stage === 'completed' && run.status !== 'completed' ? '保存学习笔记' : stageLabels[stage]}
        {i === current && <small>{stopped ? '任务停在这里。' : isActiveRun(run) ? '由后台继续处理，离开页面不会中断。' : ''}</small>}
      </div>
    </li>)}</ol>
    {!!run.progress?.total_segments && <p className="product-description">已完成 {run.progress.covered_segments}/{run.progress.total_segments} 段材料</p>}
    {run.cancel_requested && isActiveRun(run) && <p className="product-description" role="status">取消请求已提交，正在等待后台确认。</p>}
    {run.error_code && <p className="form-err" role="alert">{run.error_code === 'budget_exhausted' && budgetReasons[reason] || errorLabels[run.error_code] || '任务未能完成，请检查来源与模型设置。'}</p>}
    <p className="product-description">{run.usage.llm_calls}{run.budget ? `/${run.budget.max_llm_calls}` : ''} 次模型调用 · {run.usage.token_source === 'unknown' ? 'Token 用量尚未确认' : `${run.usage.token_source === 'actual' ? '实际' : run.usage.token_source === 'estimated' ? '估算' : '实际与估算混合'} Token 用量`}</p>
    {run.usage.token_source !== 'unknown' && <p className="product-description mono">输入 {run.usage.prompt_tokens}{run.budget ? ` / ${run.budget.max_input_tokens}` : ''} · 输出 {run.usage.completion_tokens}{run.budget ? ` / ${run.budget.max_output_tokens}` : ''}</p>}
  </>
}

export function RunDetail({ id, readOnly, onClose, onRun }: { id: string; readOnly: boolean; onClose: () => void; onRun: (id: string) => void }) {
  const client = useQueryClient()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const retryKey = useRef<string | null>(null)
  const query = useQuery({ queryKey: ['artifact-run', id], queryFn: ({ signal }) => artifactApi.run(id, signal), refetchInterval: query => query.state.data && isActiveRun(query.state.data) ? 3000 : false })
  async function action(name: 'cancel' | 'retry' | 'resume') {
    setBusy(true); setError('')
    try {
      retryKey.current ??= crypto.randomUUID()
      const run = name === 'retry' ? await artifactApi.retry(id, retryKey.current) : name === 'cancel' ? await artifactApi.cancel(id) : await artifactApi.resume(id)
      client.setQueryData(['artifact-run', run.id], run)
      await client.invalidateQueries({ queryKey: ['product-tasks'] })
      await client.invalidateQueries({ queryKey: ['artifacts'] })
      await client.invalidateQueries({ queryKey: ['artifact', run.artifact_id] })
      if (run.id !== id) onRun(run.id)
    } catch (error) { setError(artifactError(error)) } finally { setBusy(false) }
  }
  const run = query.data
  return <Modal title="学习笔记 · 后台任务" onClose={onClose} width={540} footer={run && !query.error ? <>
    {!readOnly && run.can_cancel && <button className="btn" disabled={busy || run.cancel_requested} onClick={() => void action('cancel')}>{run.cancel_requested ? '正在请求取消…' : '取消生成'}</button>}
    {!readOnly && run.can_resume && <button className="btn" disabled={busy} onClick={() => void action('resume')}>请求恢复</button>}
    {!readOnly && run.can_retry && <button className="btn btn-primary" disabled={busy} onClick={() => void action('retry')}>重新生成</button>}
    {run.result && <Link className="btn btn-primary" href={`/artifacts/${encodeURIComponent(run.result.artifact_id)}${run.result.is_candidate ? `?version=${encodeURIComponent(run.result.version_id)}&candidate=1` : ''}`}>{run.result.is_candidate ? '查看候选版本' : '打开学习笔记'}</Link>}
  </> : undefined}>
    {query.isPending ? <LoadingBlock label="正在读取后台状态…" /> : query.error ? <ErrorState message={artifactError(query.error)} onRetry={() => void query.refetch()} /> : <RunProgress run={query.data} />}
    {run?.result?.is_candidate && <p className="product-description">生成期间笔记已有修改，本次结果保留为候选，需要你查看并采用。</p>}
    {error && <p className="form-err" role="alert">{error}</p>}
    {run && <p className="product-description">来源：<Link href={`/video/${run.source_task_id}`}>视频 #{run.source_task_id}</Link> · 成果：<Link href={`/artifacts/${encodeURIComponent(run.artifact_id)}`}>打开成果页</Link></p>}
    <p className="product-description">运行 ID：<span className="mono" style={{ overflowWrap: 'anywhere' }}>{id}</span></p>
  </Modal>
}
