import Link from '@/lib/router'
import type { Artifact } from '@/lib/artifacts/schema'
import { Icon } from '@/components/ui/Icon'
import { fmtRelTime } from '@/lib/format'
import { artifactCardStatus } from '@/lib/artifacts/view'

export function ArtifactCard({ artifact, href }: { artifact: Artifact; href?: string }) {
  return <Link className="artifact-card" href={href ?? `/artifacts/${encodeURIComponent(artifact.id)}`}>
    <div className="artifact-cover" aria-hidden="true"><Icon name="file" /><span className="artifact-cover-label">NOTES & CONNECTIONS</span></div>
    <div className="artifact-card-copy"><span>学习笔记 · 思维导图</span><h2>{artifact.title || '未命名学习笔记'}</h2><footer><span>{artifactCardStatus(artifact)}</span><span>{fmtRelTime(artifact.updated_at)}</span></footer></div>
  </Link>
}
