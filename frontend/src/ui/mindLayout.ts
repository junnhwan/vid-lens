import type { MindMap as MapModel } from "../contract/types"

export interface MindNode {
  id: string
  parentId: string | null
  label: string
  sectionId: string | null
  depth: number
  childIds: string[]
}

export interface MindIndex {
  rootId: string
  nodes: Map<string, MindNode>
  order: string[]
}

export interface MindBox {
  x: number
  y: number
  w: number
  h: number
}

export interface MindWire {
  id: string
  depth: number
  d: string
  x1: number
  y1: number
  x2: number
  y2: number
}

const maxNodes = 96
const maxDepth = 6
const gapX = 56
const gapY = 16

export function indexMind(map: MapModel): MindIndex | null {
  const raw = map.nodes.slice(0, maxNodes)
  const byId = new Map(raw.map((node) => [node.id, node]))
  const root = byId.get(map.root_id) ?? raw.find((node) => !node.parent_id)
  if (!root) return null
  const childrenOf = new Map<string, string[]>()
  for (const node of raw) {
    if (!node.parent_id || node.id === root.id) continue
    const list = childrenOf.get(node.parent_id) ?? []
    list.push(node.id)
    childrenOf.set(node.parent_id, list)
  }
  const nodes = new Map<string, MindNode>()
  const order: string[] = []
  function walk(id: string, parentId: string | null, depth: number, ancestors: Set<string>) {
    if (depth > maxDepth || ancestors.has(id) || nodes.has(id) || !byId.has(id)) return
    const source = byId.get(id)!
    const nextAncestors = new Set(ancestors).add(id)
    nodes.set(id, {
      id,
      parentId,
      label: source.label,
      sectionId: source.section_id,
      depth,
      childIds: [],
    })
    order.push(id)
    const childIds = (childrenOf.get(id) ?? []).filter((child) => !nextAncestors.has(child))
    for (const child of childIds) walk(child, id, depth + 1, nextAncestors)
    const built = nodes.get(id)
    if (built) built.childIds = childIds.filter((child) => nodes.has(child))
  }
  walk(root.id, null, 0, new Set())
  return { rootId: root.id, nodes, order }
}

export function visibleMind(index: MindIndex, collapsed: ReadonlySet<string>, rootId = index.rootId): MindNode[] {
  const result: MindNode[] = []
  function walk(id: string) {
    const node = index.nodes.get(id)
    if (!node) return
    result.push(node)
    if (!collapsed.has(id)) for (const child of node.childIds) walk(child)
  }
  walk(rootId)
  return result
}

export function descendantCount(index: MindIndex, id: string): number {
  return visibleMind(index, new Set(), id).length - 1
}

export function ancestorPath(index: MindIndex, nodeId: string | null): Set<string> {
  const path = new Set<string>()
  let current = nodeId
  const guard = new Set<string>()
  while (current && !guard.has(current)) {
    guard.add(current)
    path.add(current)
    current = index.nodes.get(current)?.parentId ?? null
  }
  return path
}

export function nodeForSection(index: MindIndex, sectionId: string): string | null {
  if (!sectionId) return null
  for (const id of index.order) {
    if (index.nodes.get(id)?.sectionId === sectionId) return id
  }
  return null
}

export function estimateNodeBox(label: string, depth: number): { w: number; h: number } {
  const w = depth === 0 ? 224 : depth === 1 ? 212 : 248
  const font = depth === 0 ? 16 : depth === 1 ? 15 : 14
  const units = Array.from(label).reduce((sum, char) => sum + (/[^\u0000-\u00ff]/.test(char) ? 1 : .56), 0)
  const lines = Math.max(1, Math.min(3, Math.ceil(units * font / (w - 42))))
  return { w, h: Math.max(48, Math.ceil(lines * 24 + 20)) }
}

export function overviewMind(rootId: string, topics: string[], width: number) {
  const boxes = new Map<string, MindBox>()
  const w = Math.max(240, width)
  const narrow = w < 560
  if (!topics.length) {
    boxes.set(rootId, { x: (w - Math.min(280, w - 24)) / 2, y: 24, w: Math.min(280, w - 24), h: 96 })
    return { boxes, width: w, height: 144, narrow }
  }
  if (narrow) {
    const nodeW = (w - 16) / 2
    boxes.set(rootId, { x: 12, y: 0, w: w - 24, h: 84 })
    topics.forEach((id, index) => boxes.set(id, { x: index % 2 ? nodeW + 16 : 0, y: 108 + Math.floor(index / 2) * 76, w: nodeW, h: 64 }))
    return { boxes, width: w, height: 108 + Math.ceil(topics.length / 2) * 76, narrow }
  }
  const rows = Math.ceil(topics.length / 2)
  const height = Math.max(256, rows * 64 + 16)
  const rootW = Math.min(200, w * .29)
  const nodeW = Math.min(210, w * .26)
  const leftCount = Math.floor(topics.length / 2)
  boxes.set(rootId, { x: (w - rootW) / 2, y: (height - 96) / 2, w: rootW, h: 96 })
  topics.forEach((id, index) => {
    const left = index < leftCount
    const count = left ? leftCount : topics.length - leftCount
    const at = left ? index : index - leftCount
    boxes.set(id, { x: left ? 0 : w - nodeW, y: (height - (count * 56 + (count - 1) * 8)) / 2 + at * 64, w: nodeW, h: 56 })
  })
  return { boxes, width: w, height, narrow }
}

function round(value: number): number {
  return Math.round(value * 10) / 10
}

export function layoutMind(
  visible: MindNode[],
  rootId: string,
  sizes: ReadonlyMap<string, { w: number; h: number }>,
): { boxes: Map<string, MindBox>; wires: MindWire[]; width: number; height: number } {
  const byId = new Map(visible.map((node) => [node.id, node]))
  const boxes = new Map<string, MindBox>()
  function sizeOf(id: string) {
    return sizes.get(id) ?? estimateNodeBox(byId.get(id)?.label ?? "", byId.get(id)?.depth ?? 1)
  }
  function extent(id: string): number {
    const node = byId.get(id)
    if (!node) return 0
    const kids = node.childIds.filter((child) => byId.has(child))
    if (!kids.length) return sizeOf(id).h
    const sum = kids.reduce((total, child) => total + extent(child), 0) + gapY * (kids.length - 1)
    return Math.max(sizeOf(id).h, sum)
  }
  function place(id: string, x: number, top: number) {
    const node = byId.get(id)
    if (!node) return
    const box = sizeOf(id)
    const span = extent(id)
    boxes.set(id, { x, y: top + (span - box.h) / 2, w: box.w, h: box.h })
    const kids = node.childIds.filter((child) => byId.has(child))
    if (!kids.length) return
    const kidsSpan = kids.reduce((total, child) => total + extent(child), 0) + gapY * (kids.length - 1)
    let cursor = top + (span - kidsSpan) / 2
    for (const child of kids) {
      place(child, x + box.w + gapX, cursor)
      cursor += extent(child) + gapY
    }
  }
  place(rootId, 16, 12)
  let width = 32
  let height = 24
  for (const box of boxes.values()) {
    width = Math.max(width, box.x + box.w + 28)
    height = Math.max(height, box.y + box.h + 16)
  }
  const wires: MindWire[] = []
  for (const node of visible) {
    if (!node.parentId) continue
    const parent = boxes.get(node.parentId)
    const child = boxes.get(node.id)
    if (!parent || !child) continue
    const x1 = parent.x + parent.w
    const y1 = parent.y + parent.h / 2
    const x2 = child.x
    const y2 = child.y + child.h / 2
    const bend = Math.max(24, (x2 - x1) * 0.46)
    wires.push({
      id: node.id,
      depth: node.depth,
      x1,
      y1,
      x2,
      y2,
      d: `M ${round(x1)} ${round(y1)} C ${round(x1 + bend)} ${round(y1)}, ${round(x2 - bend)} ${round(y2)}, ${round(x2)} ${round(y2)}`,
    })
  }
  return { boxes, wires, width, height }
}

export interface MindMove {
  focus: string
  collapsed?: Set<string>
}

export function mindMove(focusedId: string, key: string, visible: MindNode[], collapsed: ReadonlySet<string>): MindMove | null {
  const at = visible.findIndex((node) => node.id === focusedId)
  const node = visible[at]
  if (!node) return null
  if (key === "Home") return { focus: visible[0].id }
  if (key === "End") return { focus: visible[visible.length - 1].id }
  if (key === "ArrowDown" || key === "ArrowUp") {
    const next = visible[at + (key === "ArrowDown" ? 1 : -1)]
    return next ? { focus: next.id } : null
  }
  if (key === "ArrowLeft") {
    if (node.childIds.length && !collapsed.has(node.id)) return { focus: node.id, collapsed: new Set([...collapsed, node.id]) }
    return node.parentId && visible.some((item) => item.id === node.parentId) ? { focus: node.parentId } : null
  }
  if (key === "ArrowRight" && node.childIds.length) {
    const next = new Set(collapsed)
    next.delete(node.id)
    return { focus: node.childIds[0], ...(collapsed.has(node.id) ? { collapsed: next } : {}) }
  }
  return null
}
