'use client'

import { useCallback, useEffect, useState } from 'react'
import Link from 'next/link'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCrumb, useShell } from '@/components/shell/AppShell'
import { ArtifactWorkspace } from '@/components/artifacts/ArtifactWorkspace'
import { RemoteEvidencePanel } from '@/components/artifacts/EvidencePanel'
import { ErrorState, LoadingBlock } from '@/components/ui/AsyncState'
import { Modal } from '@/components/ui/Modal'
import { artifactApi, artifactError } from '@/lib/artifacts/api'

export default function ArtifactPage({ params, searchParams }: { params: { id: string }; searchParams: { version?: string; candidate?: string } }) {
  const { user, registerLeaveGuard } = useShell()
  const client = useQueryClient()
  const [evidenceId, setEvidenceId] = useState<string>()
  const [versionsOpen, setVersionsOpen] = useState(false)
  const [adopting, setAdopting] = useState(false)
  const [adoptError, setAdoptError] = useState('')
  const query = useQuery({ queryKey: ['artifact', params.id], queryFn: ({ signal }) => artifactApi.get(params.id, signal), enabled: !!user, refetchInterval: 15_000 })
  const versionId = searchParams.version
  const version = useQuery({ queryKey: ['artifact-version', params.id, versionId], queryFn: ({ signal }) => artifactApi.version(params.id, versionId!, signal), enabled: !!user && !!versionId, staleTime: 0 })
  const versions = useQuery({ queryKey: ['artifact-versions', params.id], queryFn: ({ signal }) => artifactApi.versions(params.id, signal), enabled: !!user && versionsOpen })
  useCrumb([{ label: '成果', href: '/artifacts' }, { label: query.data?.title || '学习笔记' }])
  const dirtyChange = useCallback((dirty: boolean) => registerLeaveGuard(dirty ? () => window.confirm('笔记还有未保存的修改，确定离开？') : null), [registerLeaveGuard])
  useEffect(() => () => registerLeaveGuard(null), [registerLeaveGuard])
  useEffect(() => { setEvidenceId(undefined) }, [params.id, versionId])
  if (query.error || version.error) return <div className="page"><ErrorState message={artifactError(query.error || version.error)} onRetry={() => { void query.refetch(); if (versionId) void version.refetch() }} /><Link className="btn" href="/artifacts">返回成果库</Link></div>
  if (!query.data || (versionId && !version.data)) return <div className="page"><LoadingBlock label="正在读取笔记…" /></div>
  const artifact = version.data && versionId ? { ...query.data, version: version.data } : query.data
  const selected = evidenceId ?? artifact.version?.body.blocks.flatMap(block => block.evidence_refs)[0]?.evidence_id
  const candidate = !!versionId && !!version.data?.was_candidate && versionId !== query.data.current_version_id && versionId !== query.data.version?.adopted_from_version_id
  return <>
    {artifact.version?.source_status === 'outdated' && <div className="artifact-notice">视频来源已更新。这份笔记保留旧快照，旧时间不能用于定位新视频，请核对后重新生成。</div>}
    {versionId && <div className="artifact-notice">正在查看{candidate ? '生成候选' : '历史'}版本 v{artifact.version?.version}。<Link className="btn btn-sm" href={`/artifacts/${encodeURIComponent(params.id)}`}>回到当前版本</Link>{candidate && <button className="btn btn-sm btn-primary" disabled={user?.role === 'DEMO' || adopting} onClick={async () => { setAdopting(true); setAdoptError(''); try { const data = await artifactApi.adopt(params.id, query.data.head_version, versionId); client.setQueryData(['artifact', params.id], data); setAdoptError("已采用为新版本，请回到当前版本查看。"); await client.invalidateQueries({ queryKey: ['artifact-versions', params.id] }) } catch (error) { setAdoptError(artifactError(error)) } finally { setAdopting(false) } }}>采用为新版本</button>}{adoptError && <span role="alert">{adoptError}</span>}</div>}
    <ArtifactWorkspace key={`${params.id}-${versionId || 'head'}`} artifact={artifact} readOnly={!user || user.role === 'DEMO' || !!versionId} historical={!!versionId} selectedEvidence={selected} onEvidence={setEvidenceId} evidencePanel={<RemoteEvidencePanel key={artifact.version?.id} outdated={artifact.version?.source_status === 'outdated'} manifestId={artifact.version?.manifest_id} evidenceId={selected} />} onDirtyChange={dirtyChange} onVersions={() => setVersionsOpen(true)} onReload={() => artifactApi.get(params.id)} onSave={async (base, body) => { const data = await artifactApi.save(params.id, base, body); client.setQueryData(['artifact', params.id], data); await client.invalidateQueries({ queryKey: ['artifacts'] }); await client.invalidateQueries({ queryKey: ['artifact-versions', params.id] }); return data }} />
    {versionsOpen && <Modal title="版本记录" onClose={() => setVersionsOpen(false)}>{versions.isPending ? <LoadingBlock /> : versions.error ? <ErrorState message={artifactError(versions.error)} onRetry={() => void versions.refetch()} /> : <div className="version-list">{versions.data.list.map(item => <Link key={item.id} className="generation-source" href={`/artifacts/${encodeURIComponent(params.id)}?version=${encodeURIComponent(item.id)}`} onClick={() => setVersionsOpen(false)}><div><b>v{item.version} · {item.was_candidate ? '生成候选' : item.origin === 'user' ? '人工保存' : '后台生成'}{item.id === query.data.current_version_id ? ' · 当前版本' : ''}</b><p>{new Date(item.created_at).toLocaleString('zh-CN')}</p></div></Link>)}</div>}</Modal>}
  </>
}
