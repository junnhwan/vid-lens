import type { ArtifactDetail, Evidence, StudyBody } from './schema.ts'
import { evidenceTime } from './view.ts'

const modality: Record<string, string> = { transcript: '转写', visual_ocr: '画面文字', visual_caption: '画面观察' }
const relation: Record<string, string> = { supports: '支持', context: '背景', contradicts: '相反' }
function redactSecrets(value: string): string {
  return value
    .replace(/https?:\/\/[^\s<>)]+/gi, match => /[?&](?:token|signature|x-amz-signature|x-amz-credential|awsaccesskeyid)=/i.test(match) ? '[已移除签名媒体地址]' : match)
    .replace(/\bBearer\s+[^\s]+/gi, '[已移除凭证]')
    .replace(/\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b/g, '[已移除 JWT]')
    .replace(/\b(?:api[_-]?key|secret|access[_-]?token)\s*[:=]\s*["']?[^\s"']{8,}["']?/gi, '[已移除密钥]')
}
const safeTitle = (value: string) => redactSecrets(value).replace(/[\\`*_{}\[\]<>]/g, '\\$&').replace(/\r?\n/g, ' ')
const url = (origin: string, path: string) => new URL(path, origin).href
const versionOrigin = { generated: '后台生成', user: '人工保存', agent: 'Agent 修订', undo: '撤销版本' } as const

export function markdownEvidenceIds(body: StudyBody): string[] {
  return [...new Set([...body.blocks, ...(body.relations ?? [])].flatMap(item => item.evidence_refs.map(ref => ref.evidence_id)))]
}

/** Renders one server-confirmed immutable version. Callers must re-read access before download. */
export function savedMarkdown(detail: ArtifactDetail, evidence: Map<string, Evidence>, origin: string): string {
  const version = detail.version
  if (!version) throw new Error('当前没有可导出的已保存版本')
  const body: StudyBody = version.body
  const references = new Map<string, number>()
  for (const block of body.blocks) for (const ref of block.evidence_refs) {
    if (!evidence.has(ref.evidence_id)) throw new Error('有引用尚未通过来源权限核对，请重试导出')
    if (!references.has(ref.evidence_id)) references.set(ref.evidence_id, references.size + 1)
  }
  for (const item of body.relations ?? []) for (const ref of item.evidence_refs) {
    if (!evidence.has(ref.evidence_id)) throw new Error('关系依据尚未通过来源权限核对，请重试导出')
    if (!references.has(ref.evidence_id)) references.set(ref.evidence_id, references.size + 1)
  }
  const versionPath = `/artifacts/${encodeURIComponent(detail.id)}?version=${encodeURIComponent(version.id)}`
  const lines = [
    `# ${safeTitle(body.title)}`, '',
    `> 已保存版本：v${version.version} · ${versionOrigin[version.origin]} · ${new Date(version.created_at).toLocaleString('zh-CN')}`,
    `> [查看此版本](${url(origin, versionPath)}) · ${version.source_status === 'outdated' ? '来源已更新，旧时间不用于定位新视频' : '来源快照当前有效'}`,
    '',
  ]
  const depths = new Map<string, number>()
  for (const block of body.blocks) {
    const depth = block.parent_id ? (depths.get(block.parent_id) ?? 1) + 1 : 1
    depths.set(block.block_id, depth)
    lines.push(`${'#'.repeat(Math.min(depth + 1, 6))} ${safeTitle(block.title)}`, '')
    if (block.content.trim()) lines.push(redactSecrets(block.content.trim()), '')
    if (block.evidence_refs.length) {
      lines.push(`依据：${block.evidence_refs.map(ref => `[${references.get(ref.evidence_id)}]（${relation[ref.relation] ?? ref.relation}${ref.chat_citation_id ? `，聊天引用 ${ref.chat_citation_id}` : ''}）`).join(' · ')}`, '')
    }
  }
  if (body.relations?.length) {
    const titles = new Map(body.blocks.map(block => [block.block_id, block.title]))
    const labels = { related_to: '相关', depends_on: '依赖', contrasts_with: '对比' }
    lines.push('## 概念关系', '')
    for (const item of body.relations) {
      const arrow = item.type === 'depends_on' ? '→' : '↔'
      const refs = item.evidence_refs.map(ref => `[${references.get(ref.evidence_id)}]`).join('、')
      lines.push(`- ${safeTitle(titles.get(item.source_block_id) ?? item.source_block_id)} ${arrow} ${safeTitle(titles.get(item.target_block_id) ?? item.target_block_id)}（${labels[item.type]}；${item.origin === 'user' ? '人工整理' : '综合推断'}${refs ? `；依据 ${refs}` : '；未附依据'}）`)
    }
    lines.push('')
  }
  lines.push('## 来源', '')
  if (!references.size) lines.push('此版本没有关联来源引用。', '')
  for (const [id, index] of references) {
    const item = evidence.get(id)!
    const time = evidenceTime(item)
    const canLocate = version.source_status === 'current' && item.time_range_status !== 'unknown' && item.start_ms !== null
    const sourcePath = `/video/${item.source_id}${canLocate ? `?t=${item.start_ms}` : ''}`
    lines.push(`${index}. [${safeTitle(item.source_title)}](${url(origin, sourcePath)}) · ${modality[item.modality] ?? '视频证据'} · ${time}${version.source_status === 'outdated' ? '（旧快照，不用于定位）' : ''}`)
    lines.push('')
  }
  lines.push('---', '', '引用关联不代表结论已核实。请结合原视频核对。', '')
  return lines.join('\n')
}

export function markdownFilename(title: string, version: number): string {
  const safe = title.replace(/[<>:"/\\|?*\x00-\x1f]/g, '-').trim().slice(0, 80) || '学习笔记'
  return `${safe}-v${version}.md`
}
