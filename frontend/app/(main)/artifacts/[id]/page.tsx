import { useCallback, useEffect, useState } from 'react'
import Link from '@/lib/router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCrumb, useShell } from '@/components/shell/AppShell'
import { ArtifactWorkspace } from '@/components/artifacts/ArtifactWorkspace'
import { ArtifactAgentPanel } from '@/components/artifacts/ArtifactAgentPanel'
import { RemoteEvidencePanel } from '@/components/artifacts/EvidencePanel'
import { ErrorState, LoadingBlock, ProductSkeleton } from '@/components/ui/AsyncState'
import { Modal } from '@/components/ui/Modal'
import { artifactApi, artifactError } from '@/lib/artifacts/api'
import { runLabels } from '@/lib/artifacts/view'
import { markdownEvidenceIds, markdownFilename, savedMarkdown } from '@/lib/artifacts/markdown'
import type { ArtifactDetail, ArtifactEditOperation, ArtifactEditRun, Evidence, VersionSummary } from '@/lib/artifacts/schema'
import { useRouter } from '@/lib/router'
import { useStudyPosition } from '@/lib/artifacts/useStudyPosition'

export default function ArtifactPage({ params, searchParams }: { params: { id: string }; searchParams: { version?: string; candidate?: string; block?: string } }) {
  const router = useRouter()
  const study = useStudyPosition()
  const { user, registerLeaveGuard } = useShell()
  const client = useQueryClient()
  const [evidenceId, setEvidenceId] = useState<string>()
  const [versionsOpen, setVersionsOpen] = useState(false)
  const [adopting, setAdopting] = useState(false)
  const [adoptError, setAdoptError] = useState('')
  const [bridgeError, setBridgeError] = useState('')
  const [agentPanel, setAgentPanel] = useState<{ scope: string | null; base: ArtifactDetail; run?: ArtifactEditRun | null } | null>(null)
  const [evidenceOpenRequest, setEvidenceOpenRequest] = useState<{ id: string; nonce: number } | null>(null)
  const query = useQuery({ queryKey: ['artifact', params.id], queryFn: ({ signal }) => artifactApi.get(params.id, signal), enabled: !!user, refetchInterval: 15_000 })
  const versionId = searchParams.version
  const version = useQuery({ queryKey: ['artifact-version', params.id, versionId], queryFn: ({ signal }) => artifactApi.version(params.id, versionId!, signal), enabled: !!user && !!versionId, staleTime: 0 })
  const versions = useQuery({ queryKey: ['artifact-versions', params.id], queryFn: ({ signal }) => artifactApi.versions(params.id, signal), enabled: !!user && versionsOpen })
  const latestOperationId = query.data?.latest_edit_run?.result && 'operation_id' in query.data.latest_edit_run.result ? query.data.latest_edit_run.result.operation_id : undefined
  const latestOperation = useQuery({ queryKey: ['artifact-edit-operation', latestOperationId], queryFn: ({ signal }) => artifactApi.editOperation(latestOperationId!, signal), enabled: !!user && !!latestOperationId, refetchInterval: 15_000 })
  const refreshAfterAgentEdit = useCallback(async () => {
    const fresh = await artifactApi.get(params.id)
    client.setQueryData(['artifact', params.id], fresh)
    await Promise.all([client.invalidateQueries({ queryKey: ['artifacts'] }), client.invalidateQueries({ queryKey: ['artifact-versions', params.id] })])
    return fresh
  }, [client, params.id])
  const recordAgentOperation = useCallback((operation: ArtifactEditOperation) => {
    client.setQueryData(['artifact-edit-operation', operation.id], operation)
  }, [client])
  const openAgentEvidence = useCallback((id: string) => {
    setAgentPanel(null)
    setEvidenceId(id)
    setEvidenceOpenRequest(current => ({ id, nonce: (current?.nonce ?? 0) + 1 }))
  }, [])
  useCrumb([{ label: '成果', href: '/artifacts' }, { label: query.data?.title || '学习笔记' }])
  const dirtyChange = useCallback((dirty: boolean) => registerLeaveGuard(dirty ? () => window.confirm('笔记还有未保存的修改，确定离开？') : null), [registerLeaveGuard])
  useEffect(() => () => registerLeaveGuard(null), [registerLeaveGuard])
  useEffect(() => { setEvidenceId(undefined); setEvidenceOpenRequest(null) }, [params.id, versionId])
  if (query.error || version.error) return <div className="page"><ErrorState message={artifactError(query.error || version.error)} onRetry={() => { void query.refetch(); if (versionId) void version.refetch() }} /><Link className="btn" href="/artifacts">返回成果库</Link></div>
  if (!query.data || (versionId && !version.data)) return <ProductSkeleton kind="article" />
  const artifact = version.data && versionId ? { ...query.data, version: version.data } : query.data
  if (!artifact.version) {
    const run = artifact.latest_run
    return <div className="page"><div className="empty card"><h1>{artifact.title || '学习笔记'}</h1><p>{run ? `当前任务：${runLabels[run.status]}。` : '当前还没有可阅读的版本。'}可在任务中心查看状态与处理记录。</p><Link className="btn" href={run ? `/tasks?run=${encodeURIComponent(run.id)}` : '/tasks'}>查看任务</Link></div></div>
  }
  const selected = evidenceId ?? artifact.version?.body.blocks.flatMap(block => block.evidence_refs)[0]?.evidence_id
  const candidate = !!versionId && !!version.data?.was_candidate && versionId !== query.data.current_version_id && versionId !== query.data.version?.adopted_from_version_id
  const latestEdit = query.data.latest_edit_run
  const versionLabel = (item: VersionSummary) => item.was_candidate ? '生成候选' : item.origin === 'user' ? '人工保存' : item.origin === 'agent' ? 'Agent 修订' : item.origin === 'undo' ? '撤销版本' : '后台生成'
  return <>
    {query.data.latest_run && ['failed', 'cancelled', 'budget_exhausted'].includes(query.data.latest_run.status) && <div className="artifact-notice" role="status">最近一次生成：{runLabels[query.data.latest_run.status]}。原笔记仍可阅读。<Link className="btn btn-sm" href={`/tasks?run=${encodeURIComponent(query.data.latest_run.id)}`}>查看任务</Link></div>}
    {artifact.version?.source_status === 'outdated' && <div className="artifact-notice">视频来源已更新。这份笔记保留旧快照，旧时间不能用于定位新视频，请核对后重新生成。</div>}
    {versionId && <div className="artifact-notice">正在查看{candidate ? '生成候选' : '历史'}版本 v{artifact.version?.version}。<Link className="btn btn-sm" href={`/artifacts/${encodeURIComponent(params.id)}`}>回到当前版本</Link>{candidate && <button className="btn btn-sm btn-primary" disabled={user?.role === 'DEMO' || adopting} onClick={async () => { setAdopting(true); setAdoptError(''); try { const data = await artifactApi.adopt(params.id, query.data.head_version, versionId); client.setQueryData(['artifact', params.id], data); setAdoptError("已采用为新版本，请回到当前版本查看。"); await client.invalidateQueries({ queryKey: ['artifact-versions', params.id] }) } catch (error) { setAdoptError(artifactError(error)) } finally { setAdopting(false) } }}>采用为新版本</button>}{adoptError && <span role="alert">{adoptError}</span>}</div>}
    {study.error && <div className="artifact-notice" role="status">{study.error}</div>}
    {bridgeError && <div className="artifact-notice danger" role="alert">{bridgeError}</div>}
    {latestEdit && !agentPanel && <div className={`artifact-notice${latestEdit.status === 'failed' || latestEdit.status === 'budget_exhausted' ? ' danger' : ''}`} role="status"><span>{['pending','running'].includes(latestEdit.status) ? 'Agent 修订仍在后台执行，关闭页面不会取消。' : latestEdit.result?.kind === 'committed' ? latestOperation.data?.undo_version_id ? '最近一次 Agent 修订已安全撤销，历史版本仍保留。' : latestOperation.data?.can_undo ? '最近一次 Agent 修订已保存，可查看差异或撤销。' : '最近一次 Agent 修订已保存，可查看差异与撤销状态。' : latestEdit.result?.kind === 'proposal' ? '最近一次 Agent 修订方案等待确认。' : latestEdit.status === 'cancelled' ? '最近一次 Agent 修订已取消，没有写入版本。' : '可查看最近一次 Agent 修订结果。'}</span><button className="btn btn-sm" onClick={() => setAgentPanel({ scope: latestEdit.selected_block_ids[0] ?? null, base: query.data, run: latestEdit })}>查看修订</button></div>}
    <ArtifactWorkspace key={`${params.id}-${versionId || 'head'}`} artifact={artifact} readOnly={!user || user.role === 'DEMO' || !!versionId} historical={!!versionId} initialBlock={searchParams.block} evidenceOpenRequest={evidenceOpenRequest} onAgentEdit={(scope, base) => setAgentPanel({ scope, base })} onStudyBlock={(blockId, savedVersionId) => { void artifactApi.blockContext(params.id,savedVersionId,blockId).then(context => study.record({ task_id:context.task_id,artifact_id:params.id,version_id:savedVersionId,block_id:blockId,time_ms:0 })).catch(error => setBridgeError(artifactError(error))) }} onAskBlock={(blockId,savedVersionId) => { setBridgeError(''); void artifactApi.blockContext(params.id,savedVersionId,blockId).then(async context => { study.record({ task_id:context.task_id,artifact_id:params.id,version_id:savedVersionId,block_id:blockId,time_ms:0 }); await study.flush(); router.push(`/chat/v/${context.task_id}?artifact=${encodeURIComponent(params.id)}&version=${encodeURIComponent(savedVersionId)}&block=${encodeURIComponent(blockId)}`) }).catch(error => setBridgeError(artifactError(error))) }} selectedEvidence={selected} onEvidence={setEvidenceId} evidencePanel={(refs, activeId, onSelect) => <RemoteEvidencePanel key={artifact.version?.id} outdated={artifact.version?.source_status === 'outdated'} manifestId={artifact.version?.manifest_id} refs={refs} evidenceId={activeId} onSelect={onSelect} />} onDirtyChange={dirtyChange} onVersions={() => setVersionsOpen(true)} onReload={() => artifactApi.get(params.id)} onExport={async savedVersionId => {
      const fresh = await artifactApi.get(params.id)
      const saved = { ...fresh, version: await artifactApi.version(params.id, savedVersionId) }
      if (!saved.version) throw new Error('当前没有可导出的已保存版本')
      const ids = markdownEvidenceIds(saved.version.body)
      const rows = await Promise.all(ids.map(id => artifactApi.evidence(saved.version!.manifest_id, id)))
      const evidence = new Map<string, Evidence>(rows.map(row => [row.id, row]))
      return { markdown: savedMarkdown(saved, evidence, window.location.origin), filename: markdownFilename(saved.version.body.title, saved.version.version) }
    }} onSave={async (base, body) => { const data = await artifactApi.save(params.id, base, body); client.setQueryData(['artifact', params.id], data); await client.invalidateQueries({ queryKey: ['artifacts'] }); await client.invalidateQueries({ queryKey: ['artifact-versions', params.id] }); return data }} />
    {versionsOpen && <Modal title="版本记录" onClose={() => setVersionsOpen(false)}>{versions.isPending ? <LoadingBlock /> : versions.error ? <ErrorState message={artifactError(versions.error)} onRetry={() => void versions.refetch()} /> : <div className="version-list">{versions.data.list.map(item => <Link key={item.id} className="generation-source" href={`/artifacts/${encodeURIComponent(params.id)}?version=${encodeURIComponent(item.id)}`} onClick={() => setVersionsOpen(false)}><div><b>v{item.version} · {versionLabel(item)}{item.id === query.data.current_version_id ? ' · 当前版本' : ''}</b><p>{new Date(item.created_at).toLocaleString('zh-CN')}</p></div></Link>)}</div>}</Modal>}
    {agentPanel && <ArtifactAgentPanel artifact={agentPanel.base} initialScope={agentPanel.scope} initialRun={agentPanel.run} onClose={() => setAgentPanel(null)} onArtifactChanged={refreshAfterAgentEdit} onOperationChanged={recordAgentOperation} onOpenEvidence={openAgentEvidence} />}
  </>
}
