import type { SummaryContextRef } from './summaryExperience'

export interface SummaryAnnotation extends SummaryContextRef {
  source_title: string
  block_title: string
  provenance: string
  images?: { caption: string; alt: string; input_mode: string; capture_ms: number }[]
}

export function parseSummaryAnnotations(raw: string | null | undefined): SummaryAnnotation[] {
  if (!raw) return []
  try {
    const values: unknown = JSON.parse(raw)
    if (!Array.isArray(values) || values.length > 3) return []
    return values.filter((value): value is SummaryAnnotation => {
      if (!value || typeof value !== 'object') return false
      const v = value as Partial<SummaryAnnotation>
      return (v.kind === 'summary_selection' || v.kind === 'summary_screenshot') && typeof v.quote === 'string' && v.quote.length <= 12000 && typeof v.source_title === 'string' && typeof v.block_title === 'string' && !!v.version_ref && (typeof ('revision_id' in v.version_ref ? v.version_ref.revision_id : undefined) === 'string' || typeof ('generated_version' in v.version_ref ? v.version_ref.generated_version : undefined) === 'number')
    })
  } catch { return [] }
}

export function summaryVersionLabel(ref: SummaryContextRef): string {
  return 'revision_id' in ref.version_ref ? '人工修订版本' : `生成版本 ${ref.version_ref.generated_version}`
}
