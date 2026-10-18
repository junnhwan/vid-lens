import type { ArticleResponse } from "../contract/types"
import { formatDuration, platformLabel } from "../contract/text"

export interface ExampleData extends ArticleResponse {
  id: string
}

export interface CatalogItem {
  id: string
  title: string
  description: string
  platform: string
  duration_ms: number | null
  cover: string
}

const exampleIdPattern = /^[a-f0-9]{32}$/
export const exampleShelfLimit = 6

export function exampleArchive(id: string): string {
  return `/api/v1/examples/${id}`
}

export function catalogChoice(item: CatalogItem) {
  return {
    id: item.id,
    title: item.title,
    description: item.description,
    meta: [platformLabel(item.platform), formatDuration(item.duration_ms)].filter(Boolean).join(" · "),
    cover: item.cover,
  }
}

export function exampleRoute(search: string): { id: string | null } {
  const requested = new URLSearchParams(search).get("video") || ""
  return { id: exampleIdPattern.test(requested) ? requested : null }
}

export function exampleShelf<T extends { id: string }>(items: T[], currentId: string | null): { strip: T[]; rest: T[] } {
  if (items.length <= 1) return { strip: [], rest: [] }
  if (items.length <= exampleShelfLimit) return { strip: items, rest: [] }
  const currentIndex = currentId ? items.findIndex((item) => item.id === currentId) : -1
  if (currentIndex < exampleShelfLimit) return { strip: items.slice(0, exampleShelfLimit), rest: items.slice(exampleShelfLimit) }
  const strip = [...items.slice(0, exampleShelfLimit - 1), items[currentIndex]]
  const shown = new Set(strip.map((item) => item.id))
  return { strip, rest: items.filter((item) => !shown.has(item.id)) }
}
