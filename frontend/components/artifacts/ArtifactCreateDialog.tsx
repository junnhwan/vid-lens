import { useRef, useState } from 'react'
import { useRouter } from '@/lib/router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { TaskStatusEnum } from '@/lib/types'
import { taskTitle } from '@/lib/format'
import { artifactApi, artifactError } from '@/lib/artifacts/api'
import { Icon } from '@/components/ui/Icon'
import { Modal } from '@/components/ui/Modal'

export function ArtifactCreateDialog({ source, existing, onClose }: { source?: { id: number; title: string }; existing?: { id: string; title: string; head_version: number }; onClose: () => void }) {
  const router = useRouter()
  const client = useQueryClient()
  const [selected, setSelected] = useState(source?.id ?? 0)
  const [goal, setGoal] = useState('梳理核心概念、关键例子与容易混淆的地方，整理成带来源的中文学习笔记。')
  const [page, setPage] = useState(1)
  const [keyword, setKeyword] = useState('')
  const [submitted, setSubmitted] = useState(false)
  const key = useRef<string | null>(null)
  const videos = useQuery({ queryKey: ['artifact-source-videos', page, keyword], queryFn: () => api.listTasks(page, 20, keyword), enabled: !source })
  const create = useMutation({ mutationFn: () => {
    key.current ??= crypto.randomUUID()
    setSubmitted(true)
    return artifactApi.generate({ kind: 'study', scope: 'video', source_ids: [selected], goal: goal.trim(), ...(existing ? { artifact_id: existing.id, base_version: existing.head_version } : {}) }, key.current)
  }, onSuccess: run => { client.setQueryData(['artifact-run', run.id], run); void client.invalidateQueries({ queryKey: ['product-tasks'] }); void client.invalidateQueries({ queryKey: ['artifacts'] }); onClose(); router.push(`/tasks?run=${encodeURIComponent(run.id)}`) } })
  const frozen = submitted
  return <Modal title={existing ? '重新整理现有笔记' : '把这段视频，变成学习笔记'} onClose={onClose} confirmOnClose={create.isPending ? '生成请求仍在提交，关闭后请到任务中心核对结果，避免重复创建。' : false} width={600} footer={<><button className="btn" onClick={onClose}>暂不生成</button><button className="btn btn-primary" disabled={!selected || !goal.trim() || create.isPending} onClick={() => create.mutate()}><Icon name="wand" size="sm" />{create.isPending ? '正在提交…' : create.isError ? '重试同一请求' : existing ? '重新整理' : '开始后台生成'}</button></>}>
    <p className="product-description">{existing ? `以“${existing.title}”的 v${existing.head_version} 为基准重新整理；原版本保持可读，新结果仍需核对，遇到并发修改会保留为候选。` : '先保留理解的结构，再沿着引用回到原视频。导图直接由笔记生成，不会额外调用模型。'}</p>
    {source ? <div className="generation-source"><Icon name="video" /><div><b>{source.title}</b><p>仅使用当前视频 · 不要求已建立检索索引</p></div></div> : <div className="generation-source-picker"><label className="field-label" htmlFor="generation-search">选择一个已有转写的视频</label><input id="generation-search" className="input" placeholder="搜索视频标题…" value={keyword} disabled={frozen} onChange={e => { setKeyword(e.target.value); setPage(1); setSelected(0) }} /><select className="input" aria-label="来源视频" value={selected} disabled={frozen || videos.isPending} onChange={e => setSelected(Number(e.target.value))}><option value={0}>选择视频</option>{videos.data?.list.map(video => <option key={video.id} value={video.id} disabled={!video.has_transcription || video.status === TaskStatusEnum.Queued || video.status === TaskStatusEnum.Running}>{taskTitle(video)}{video.status === TaskStatusEnum.Queued || video.status === TaskStatusEnum.Running ? '（处理中）' : video.has_transcription ? '' : '（尚无转写）'}</option>)}</select>{videos.error && <p role="alert">视频读取失败。<button className="btn btn-sm" onClick={() => void videos.refetch()}>重试</button></p>}{videos.data && videos.data.total > 20 && <div className="product-pagination"><button className="btn btn-sm" disabled={page === 1 || frozen} onClick={() => { setPage(page - 1); setSelected(0) }}>上一页</button><span>{page} / {Math.ceil(videos.data.total / 20)}</span><button className="btn btn-sm" disabled={page * 20 >= videos.data.total || frozen} onClick={() => { setPage(page + 1); setSelected(0) }}>下一页</button></div>}</div>}
    <div className="generation-recipe"><Icon name="layers" /><div><b>学习笔记 + 思维导图</b><p>章节、概念与示例 · 可编辑正文 · 每条引用属于具体来源</p></div><Icon name="check" /></div>
    <label className="field-label" htmlFor="generation-goal">这次想重点理解什么？</label><textarea id="generation-goal" className="input" rows={4} maxLength={2000} value={goal} disabled={frozen} onChange={e => setGoal(e.target.value)} />
    <p className="product-description">使用提交时的默认模型与执行预算。任务在后台进行，离开页面不会取消。生成内容保留为待核对草稿。</p>
    {create.error && <p className="form-err" role="alert">{artifactError(create.error)}{frozen && ' 重试会复用本次请求标识。'}</p>}
  </Modal>
}
