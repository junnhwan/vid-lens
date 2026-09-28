import type { CanvasLayout, StudyBody } from './schema.ts'
import type { ELK } from 'elkjs/lib/elk-api'

let elkPromise: Promise<ELK> | null = null
async function elkEngine() { if (!elkPromise) elkPromise = import('elkjs/lib/elk.bundled.js').then(module => new module.default()); return elkPromise }
export const emptyCanvasLayout = (): CanvasLayout => ({ direction: 'RIGHT', algorithm: 'elk-layered-v1', nodes: {}, viewport: { x: 0, y: 0, zoom: 1 } })
export function visibleCanvasIds(body: StudyBody, layout: CanvasLayout): Set<string> {
  const visible = new Set<string>()
  for (const block of body.blocks) {
    if (layout.nodes[block.block_id]?.hidden) continue
    if (block.parent_id && (!visible.has(block.parent_id) || layout.nodes[block.parent_id]?.collapsed)) continue
    visible.add(block.block_id)
  }
  return visible
}
export function canvasKind(type: string, content: string): 'section' | 'concept' | 'operation' | 'command' | 'note' {
  if (type === 'section') return 'section'
  if (type === 'example' && /(?:`[^`]+`|```|(?:^|\n)\s*[$>]\s*\w)/.test(content)) return 'command'
  if (type === 'example') return 'operation'
  if (type === 'note') return 'note'
  return 'concept'
}
export function defaultCanvasNode(index: number, direction: CanvasLayout['direction']) {
  return { position: direction === 'RIGHT' ? { x: (index % 4) * 310, y: Math.floor(index / 4) * 180 } : { x: Math.floor(index / 4) * 310, y: (index % 4) * 180 }, width: 254, height: 132, pinned: false, hidden: false, collapsed: false, style: 'auto' as const }
}
export function fillCanvasPositions(body: StudyBody, layout: CanvasLayout): CanvasLayout {
  const nodes = { ...layout.nodes }
  for (const [index, block] of body.blocks.entries()) {
    if (nodes[block.block_id]) continue
    const parent = block.parent_id ? nodes[block.parent_id] : undefined
    const existingChildren = body.blocks.filter(child => child.parent_id === block.block_id).map(child => nodes[child.block_id]?.position).filter((point): point is { x: number; y: number } => !!point)
    const childAnchor = existingChildren.length ? layout.direction === 'RIGHT'
      ? { x: Math.min(...existingChildren.map(point => point.x)) - 350, y: existingChildren.reduce((sum, point) => sum + point.y, 0) / existingChildren.length }
      : { x: existingChildren.reduce((sum, point) => sum + point.x, 0) / existingChildren.length, y: Math.min(...existingChildren.map(point => point.y)) - 210 }
      : null
    let position = childAnchor ?? (parent ? layout.direction === 'RIGHT' ? { x: parent.position.x + 350, y: parent.position.y } : { x: parent.position.x, y: parent.position.y + 210 } : defaultCanvasNode(index, layout.direction).position)
    for (let step = 0; step < 120 && Object.values(nodes).some(node => intersects(position, node.position)); step++) {
      position = layout.direction === 'RIGHT' ? { ...position, y: position.y + 164 } : { ...position, x: position.x + 285 }
    }
    nodes[block.block_id] = { ...defaultCanvasNode(index, layout.direction), position }
  }
  return { ...layout, nodes }
}
export function canvasDescendants(body: StudyBody, root: string): Set<string> {
  const ids = new Set([root])
  for (const block of body.blocks) if (block.parent_id && ids.has(block.parent_id)) ids.add(block.block_id)
  return ids
}
function intersects(a: { x: number; y: number }, b: { x: number; y: number }, width = 254, height = 132): boolean {
  return Math.abs(a.x - b.x) < width + 24 && Math.abs(a.y - b.y) < height + 24
}
export async function computeCanvasLayout(body: StudyBody, current: CanvasLayout, direction: CanvasLayout['direction'], scope?: Set<string>, density: 'comfortable' | 'compact' = 'comfortable'): Promise<{ layout: CanvasLayout; collisions: string[] }> {
  const visible = visibleCanvasIds(body, current)
  const blocks = body.blocks.filter(block => visible.has(block.block_id))
  const graph = {
    id: 'root', layoutOptions: { 'elk.algorithm': 'layered', 'elk.direction': direction, 'elk.spacing.nodeNode': density === 'compact' ? '22' : '44', 'elk.layered.spacing.nodeNodeBetweenLayers': density === 'compact' ? '62' : '104', 'elk.padding': '[top=28,left=28,bottom=28,right=28]' },
    children: blocks.map(block => ({ id: block.block_id, width: current.nodes[block.block_id]?.width ?? 254, height: current.nodes[block.block_id]?.height ?? 132 })),
    edges: blocks.filter(block => block.parent_id && visible.has(block.parent_id)).map(block => ({ id: `tree-${block.block_id}`, sources: [block.parent_id!], targets: [block.block_id] })),
  }
  const result = await (await elkEngine()).layout(graph)
  const nodes = { ...current.nodes }
  const anchor = scope ? current.nodes[[...scope][0]]?.position : undefined
  const anchorResult = scope ? result.children?.find(node => node.id === [...scope][0]) : undefined
  const offset = anchor && anchorResult ? { x: anchor.x - (anchorResult.x ?? 0), y: anchor.y - (anchorResult.y ?? 0) } : { x: 0, y: 0 }
  const protectedPositions = new Map<string, { x: number; y: number }>()
  for (const block of body.blocks) {
    const saved = current.nodes[block.block_id]
    if (saved && (!scope?.has(block.block_id) && scope !== undefined || saved.pinned)) protectedPositions.set(block.block_id, saved.position)
  }
  const collisions: string[] = []
  for (const [index, item] of (result.children ?? []).entries()) {
    const before = nodes[item.id] ?? defaultCanvasNode(index, direction)
    if (scope && !scope.has(item.id) || before.pinned) continue
    let position = { x: (item.x ?? 0) + offset.x, y: (item.y ?? 0) + offset.y }
    let attempts = 0
    while ([...protectedPositions.values()].some(other => intersects(position, other)) && attempts < 80) {
      position = direction === 'RIGHT' ? { ...position, y: position.y + 42 } : { ...position, x: position.x + 42 }
      attempts++
    }
    if (attempts === 80) collisions.push(item.id)
    nodes[item.id] = { ...before, position, width: item.width ?? before.width, height: item.height ?? before.height }
    protectedPositions.set(item.id, position)
  }
  return { layout: { ...current, direction, nodes }, collisions }
}
