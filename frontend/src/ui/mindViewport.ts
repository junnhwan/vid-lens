export type Point = { x: number; y: number }
export type ViewSize = { width: number; height: number }
export const MIND_PADDING = 40
export const MIN_MIND_ZOOM = .5
export const MAX_MIND_ZOOM = 2

export function mindZoom(value: number) { return Math.max(MIN_MIND_ZOOM, Math.min(MAX_MIND_ZOOM, value)) }

export function mindWorld(content: ViewSize, viewport: ViewSize, scale: number) {
  const width = Math.max(viewport.width, content.width * scale + MIND_PADDING * 2)
  const height = Math.max(viewport.height, content.height * scale + MIND_PADDING * 2)
  return { width, height, x: (width - content.width * scale) / 2, y: (height - content.height * scale) / 2 }
}

export function mindScroll(point: Point, client: Point, content: ViewSize, viewport: ViewSize, scale: number) {
  const world = mindWorld(content, viewport, scale)
  return {
    x: Math.max(0, Math.min(world.width - viewport.width, world.x + point.x * scale - client.x)),
    y: Math.max(0, Math.min(world.height - viewport.height, world.y + point.y * scale - client.y)),
  }
}

export function minimapPoint(point: Point, surface: ViewSize, map: ViewSize): Point {
  const ratio = Math.min(surface.width / map.width, surface.height / map.height)
  return { x: Math.max(0, Math.min(map.width, (point.x - (surface.width - map.width * ratio) / 2) / ratio)), y: Math.max(0, Math.min(map.height, (point.y - (surface.height - map.height * ratio) / 2) / ratio)) }
}
