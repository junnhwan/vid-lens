import Link from '@/lib/router'
import type { Artifact, GenerationRun } from '@/lib/artifacts/schema'
import { Icon } from '@/components/ui/Icon'
import { fmtRelTime } from '@/lib/format'
import { runLabels } from '@/lib/artifacts/view'

export function ArtifactCard({ artifact, href, runStatus }: { artifact: Artifact; href?: string; runStatus?: GenerationRun['status'] }) {
  return <Link className="artifact-card" href={href ?? `/artifacts/${encodeURIComponent(artifact.id)}`}>
    <div className="artifact-cover" aria-hidden="true"><Icon name="file" /><span className="artifact-cover-label">NOTES & CONNECTIONS</span></div>
    <div className="artifact-card-copy"><span>学习笔记 · 思维导图</span><h2>{artifact.title || '未命名学习笔记'}</h2><footer><span>{artifact.current_version_id ? `v${artifact.head_version} · 待核对` : runStatus ? runLabels[runStatus] : '尚无已保存版本'}</span><span>{fmtRelTime(artifact.updated_at)}</span></footer></div>
  </Link>
}
