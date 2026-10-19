export interface SiteQueue {
  schema_version: 1
  max_concurrent: number
  processing: number
  waiting: number
  as_of: string
}

export function readSiteQueue(value: SiteQueue): SiteQueue {
  if (
    value.schema_version !== 1 ||
    !Number.isInteger(value.max_concurrent) || value.max_concurrent < 1 || value.max_concurrent > 64 ||
    !Number.isInteger(value.processing) || value.processing < 0 ||
    !Number.isInteger(value.waiting) || value.waiting < 0 ||
    !Number.isFinite(Date.parse(value.as_of))
  ) throw new Error("Invalid queue status")
  return value
}
