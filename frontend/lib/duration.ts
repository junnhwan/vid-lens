export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return '—'
  return ms < 1000 ? `${Math.round(ms)}ms` : `${Math.round(ms / 1000)}s`
}
