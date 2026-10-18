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

export function visibleMind(index: MindIndex, sketch: boolean, expanded: ReadonlySet<string>): MindNode[] {
  const result: MindNode[] = []
  function walk(id: string) {
    const node = index.nodes.get(id)
    if (!node) return
    result.push(node)
    for (const childId of node.childIds) {
      const child = index.nodes.get(childId)
      if (!child) continue
      const shown = sketch ? child.depth <= 1 : child.depth <= 2 || expanded.has(node.id)
      if (shown) walk(childId)
    }
  }
  walk(index.rootId)
  return result
}

export function branchOpen(node: MindNode, sketch: boolean, expanded: ReadonlySet<string>): boolean {
  if (!node.childIds.length) return false
  if (sketch) return node.depth + 1 <= 1
  return node.depth + 1 <= 2 || expanded.has(node.id)
}

export function canToggle(node: MindNode, sketch: boolean, expanded: ReadonlySet<string>): boolean {
  if (!node.childIds.length) return false
  return !branchOpen(node, sketch, expanded) || expanded.has(node.id)
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

export function expansionForSection(index: MindIndex, sectionId: string): { sketch: false; ids: string[] } | null {
  const id = nodeForSection(index, sectionId)
  const node = id ? index.nodes.get(id) : null
  if (!node || node.depth <= 2) return null
  const ids: string[] = []
  let parent = node.parentId
  while (parent) {
    const parentNode = index.nodes.get(parent)
    if (!parentNode) break
    if (parentNode.depth >= 2) ids.push(parentNode.id)
    parent = parentNode.parentId
  }
  return { sketch: false, ids }
}

export function estimateNodeBox(label: string, depth: number): { w: number; h: number } {
  const font = depth === 0 ? 16 : depth === 1 ? 15 : 12.5
  const chars = Array.from(label).length
  const maxInner = depth === 0 ? 208 : 188
  const textWidth = Math.min(maxInner, Math.max(font * 2, chars * font))
  const lines = Math.max(1, Math.ceil((chars * font) / maxInner))
  const padX = depth === 0 ? 40 : 28
  const padY = depth === 0 ? 26 : 16
  const kicker = depth === 0 ? 16 : 0
  return {
    w: Math.ceil(textWidth + padX),
    h: Math.ceil(kicker + lines * (font + 6) + padY),
  }
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
  sketch?: boolean
  expanded?: Set<string>
}

export function mindMove(
  focusedId: string,
  key: string,
  index: MindIndex,
  visible: MindNode[],
  sketch: boolean,
  expanded: ReadonlySet<string>,
): MindMove | null {
  const node = index.nodes.get(focusedId)
  if (!node || !visible.some((item) => item.id === focusedId)) return null
  const visibleIds = new Set(visible.map((item) => item.id))
  if (key === "ArrowUp" || key === "ArrowDown") {
    const siblings = (node.parentId ? index.nodes.get(node.parentId)?.childIds ?? [] : [index.rootId]).filter((id) => visibleIds.has(id))
    const at = siblings.indexOf(focusedId)
    const next = key === "ArrowDown" ? siblings[at + 1] : siblings[at - 1]
    return next ? { focus: next } : null
  }
  if (key === "ArrowLeft") {
    if (!sketch && node.depth >= 2 && expanded.has(node.id)) {
      const next = new Set(expanded)
      next.delete(node.id)
      return { focus: focusedId, expanded: next }
    }
    return node.parentId && visibleIds.has(node.parentId) ? { focus: node.parentId } : null
  }
  if (key === "ArrowRight") {
    if (node.childIds.length && !branchOpen(node, sketch, expanded)) {
      if (sketch) return { focus: node.childIds[0], sketch: false }
      const next = new Set(expanded)
      next.add(node.id)
      return { focus: node.childIds[0], expanded: next }
    }
    const first = node.childIds.find((id) => visibleIds.has(id))
    return first ? { focus: first } : null
  }
  return null
}
