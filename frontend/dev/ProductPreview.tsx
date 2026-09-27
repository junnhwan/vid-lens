import Link from '@/lib/router'
import { useEffect, useMemo, useState } from 'react'
import { ShellFrame } from '@/components/shell/ShellFrame'
import { ArtifactWorkspace } from '@/components/artifacts/ArtifactWorkspace'
import { ArtifactQueryProvider } from '@/components/artifacts/ArtifactQueryProvider'
import { ArtifactCard } from '@/components/artifacts/ArtifactCard'
import { EvidencePanel } from '@/components/artifacts/EvidencePanel'
import { TaskList } from '@/components/artifacts/TaskList'
import { RunProgress } from '@/components/artifacts/RunDetail'
import { PageHeading } from '@/components/product/PageHeading'
import { ProductHero } from '@/components/product/ProductHero'
import { EmptyState, ErrorState, LoadingBlock } from '@/components/ui/AsyncState'
import { Modal } from '@/components/ui/Modal'
import { Icon } from '@/components/ui/Icon'
import { ApiError } from '@/lib/api'
import { detailSchema, type ArtifactDetail } from '@/lib/artifacts/schema'
import { evidenceFixtures, runFixture, sourceTitle, studyFixture, taskFixtures } from './productFixtures'

const STORAGE_KEY = 'vidlens-dev-product-contract-v1'
export function ProductPreview({ view }: { view: string }) {
  const [artifact, setArtifact] = useState<ArtifactDetail>(studyFixture)
  const [scenario, setScenario] = useState('normal')
  const [evidenceId, setEvidenceId] = useState('preview-e2')
  const [runId, setRunId] = useState<string>()
  const [infoOpen, setInfoOpen] = useState(false)
  useEffect(() => { try { const raw = sessionStorage.getItem(STORAGE_KEY); if (raw) { const result = detailSchema.safeParse(JSON.parse(raw)); if (result.success) setArtifact(result.data) } } catch { /* Preview storage is optional. */ } }, [])
  const displayedArtifact = useMemo(() => {
    if (scenario !== 'large' || !artifact.version) return artifact
    const blocks = Array.from({ length: 200 }, (_, i) => ({ ...artifact.version!.body.blocks[0], block_id: `large-${i}`, parent_id: i % 20 === 0 ? null : `large-${Math.floor(i / 20) * 20}`, title: i % 20 === 0 ? `章节 ${Math.floor(i / 20) + 1}` : `中文长概念 ${i}：理解构建、配置与验证之间的关系`, content: '用于检验 200 个节点、中文长标签与证据联动的开发样例。' }))
    return { ...artifact, version: { ...artifact.version, body: { ...artifact.version.body, blocks } } }
  }, [artifact, scenario])
  const evidence = evidenceFixtures.find(e => e.id === evidenceId) ?? evidenceFixtures[0]
  const conflict: ArtifactDetail = { ...artifact, head_version: artifact.head_version + 1, version: artifact.version ? { ...artifact.version, version: artifact.head_version + 1, body: { ...artifact.version.body, title: '另一窗口更新后的学习笔记' } } : null }
  const pathname = view === 'home' ? '/' : view === 'tasks' ? '/tasks' : '/artifacts'
  return <ArtifactQueryProvider><ShellFrame pathname={pathname} crumb={[{ label: '工作台', href: '/dev/product?view=home' }, { label: view === 'home' ? '产品预览' : view === 'tasks' ? '任务' : view === 'artifacts' ? '成果' : '学习笔记' }]} user={{ name: '我的工作区', detail: '开发预览 · 契约样例' }} products preview onImport={() => setInfoOpen(true)}>
    <div className="product-preview-banner"><span>仅开发环境 · 所有内容为契约样例，未连接模型或后台任务。</span><div className="product-preview-controls"><label htmlFor="preview-scenario">验收状态</label><select id="preview-scenario" value={scenario} onChange={e => setScenario(e.target.value)}><option value="normal">正常内容</option><option value="large">200 节点导图</option><option value="outdated">来源已更新</option><option value="offline">保存失败</option><option value="conflict">版本冲突</option><option value="unknown">未知时间证据</option><option value="deleted">来源已删除</option><option value="readonly">只读账号</option><option value="empty">空数据</option><option value="loading">加载中</option><option value="error">读取失败</option></select><Link href="/dev/product?view=notes">笔记</Link><Link href="/dev/product?view=artifacts">成果库</Link><Link href="/dev/product?view=tasks">任务</Link></div></div>
    {scenario === 'loading' ? <div className="page"><LoadingBlock variant="card" label="正在读取成果…" /></div> : scenario === 'error' || scenario === 'deleted' ? <div className="page"><ErrorState message={scenario === 'deleted' ? '来源已删除，相关正文和证据无法继续读取。' : '暂时无法连接服务，请检查网络后重试。'} onRetry={() => setScenario('normal')} /></div> : view === 'home' ? <div className="page"><ProductHero onImport={() => setInfoOpen(true)} /><div className="product-metrics"><div className="product-metric"><span>样例视频</span><strong>01</strong></div><div className="product-metric"><span>学习成果</span><strong>01</strong></div><div className="product-metric"><span>后台任务样例</span><strong>02</strong></div></div><div className="section-head"><h2>最近成果</h2><Link className="more" href="/dev/product?view=artifacts">全部成果<Icon name="chev-r" size="sm" /></Link></div><div className="artifact-grid"><ArtifactCard artifact={artifact} href="/dev/product?view=notes" /></div><div className="section-head"><h2>后台任务</h2></div><TaskList tasks={taskFixtures} onOpen={setRunId} /></div> : view === 'artifacts' ? <div className="page"><PageHeading eyebrow="YOUR KNOWLEDGE, MADE USEFUL" title="让理解，留下来。" description="从视频到笔记，从概念到连接。每一份成果，都保留回到来源的路。" actions={<button className="btn btn-primary" onClick={() => setInfoOpen(true)}><Icon name="plus" />生成学习笔记</button>} />{scenario === 'empty' ? <EmptyState icon="layers" title="第一份成果，从一个视频开始" desc="选择已有转写的视频，整理成可编辑、可回看的学习笔记。" action={<button className="btn btn-primary" onClick={() => setInfoOpen(true)}>选择视频</button>} /> : <div className="artifact-grid"><ArtifactCard artifact={artifact} href="/dev/product?view=notes" /></div>}</div> : view === 'tasks' ? <div className="page"><PageHeading eyebrow="WORK CONTINUES IN THE BACKGROUND" title="每一步，都有回音。" description="视频处理与成果生成的进度都在这里。随时离开，再回来继续。" />{scenario === 'empty' ? <EmptyState icon="check" title="暂时没有任务" /> : <TaskList tasks={taskFixtures} onOpen={setRunId} />}</div> : scenario === 'empty' ? <div className="page"><EmptyState icon="layers" title="还没有学习笔记" action={<Link className="btn" href="/dev/product?view=artifacts">返回成果库</Link>} /></div> : <ArtifactWorkspace artifact={displayedArtifact} preview readOnly={scenario === 'readonly' || scenario === 'large'} selectedEvidence={evidenceId} onEvidence={setEvidenceId} evidencePanel={() => <EvidencePanel preview outdated={scenario === "outdated"} evidence={scenario === 'unknown' ? { ...evidence, start_ms: null, end_ms: null, time_range_status: 'unknown' } : evidence} />} onReload={async () => conflict} onSave={async (base, body) => {
      if (scenario === 'offline') throw new Error('offline')
      if (scenario === 'conflict') throw new ApiError(409, '已有新版本，你的编辑仍保留在这里。', 'version_conflict')
      const next: ArtifactDetail = { ...artifact, title: body.title, head_version: base + 1, current_version_id: `preview-version-${base + 1}`, version: { ...artifact.version!, id: `preview-version-${base + 1}`, version: base + 1, origin: 'user', body: { ...body, warnings: ['human_edited_unverified'] } } }
      sessionStorage.setItem(STORAGE_KEY, JSON.stringify(next)); setArtifact(next); return next
    }} />}
    {runId && <Modal title="后台任务 · 契约样例" onClose={() => setRunId(undefined)} footer={<Link className="btn btn-primary" href="/dev/product?view=notes" onClick={() => setRunId(undefined)}>查看成果样例</Link>}><RunProgress run={taskFixtures.find(task => task.run?.id === runId)?.run ?? runFixture} /><p className="product-description">固定状态用于检查展示，不模拟后台执行或完成进度。</p></Modal>}
    {infoOpen && <Modal title="从单视频到学习成果" onClose={() => setInfoOpen(false)} footer={<Link className="btn btn-primary" href="/dev/product?view=tasks" onClick={() => setInfoOpen(false)}>查看后台任务样例</Link>}><div className="generation-source"><Icon name="video" /><div><b>{sourceTitle}</b><p>单视频 · 契约样例</p></div></div><div className="generation-recipe"><Icon name="layers" /><div><b>学习笔记 + 思维导图</b><p>相同的内容结构，两个理解视角。编辑和证据组件与正式页面共用。</p></div></div><p className="product-description">正式页面从视频详情或成果库提交真实生成。本预览不创建任务、不调用模型；样例编辑只保留在当前标签页的开发预览存储中。</p></Modal>}
  </ShellFrame></ArtifactQueryProvider>
}
