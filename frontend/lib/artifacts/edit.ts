import { bodySchema, type StudyBlock, type StudyBody } from './schema.ts'
import { blockTree, type BlockNode } from './view.ts'

type Direction = 'up' | 'down' | 'indent' | 'outdent'

function copyTree(body: StudyBody): BlockNode[] {
  const copy = (nodes: BlockNode[]): BlockNode[] => nodes.map(({ block, children }) => ({ block: { ...block }, children: copy(children) }))
  return copy(blockTree(body))
}

function locate(nodes: BlockNode[], id: string, parent: BlockNode | null = null): { siblings: BlockNode[]; index: number; parent: BlockNode | null } | null {
  for (let index = 0; index < nodes.length; index++) {
    if (nodes[index].block.block_id === id) return { siblings: nodes, index, parent }
    const child = locate(nodes[index].children, id, nodes[index])
    if (child) return child
  }
  return null
}

function finish(body: StudyBody, nodes: BlockNode[]): StudyBody {
  const blocks: StudyBlock[] = []
  const visit = (items: BlockNode[], parent: string | null) => {
    for (const item of items) {
      blocks.push({ ...item.block, parent_id: parent })
      visit(item.children, item.block.block_id)
    }
  }
  visit(nodes, null)
  const result = { ...body, blocks }
  const checked = bodySchema.safeParse(result)
  if (!checked.success) throw new Error(checked.error.issues[0]?.message || '结构不符合保存限制')
  return checked.data
}

export function descendantCount(body: StudyBody, id: string): number {
  const item = locate(blockTree(body), id)
  if (!item) return 0
  const count = (node: BlockNode): number => 1 + node.children.reduce((sum, child) => sum + count(child), 0)
  return count(item.siblings[item.index])
}

export function addBlock(body: StudyBody, nearId: string, asChild: boolean, id: string = crypto.randomUUID()): StudyBody {
  const roots = copyTree(body)
  const place = locate(roots, nearId)
  if (!place) throw new Error('找不到选中的块')
  const block: StudyBlock = { block_id: id, parent_id: null, type: 'note', title: '新笔记', content: '', claim_origin: 'user', evidence_refs: [] }
  const node = { block, children: [] }
  if (asChild) place.siblings[place.index].children.push(node)
  else place.siblings.splice(place.index + 1, 0, node)
  return finish(body, roots)
}

export function deleteBlock(body: StudyBody, id: string): StudyBody {
  const roots = copyTree(body)
  const place = locate(roots, id)
  if (!place) throw new Error('找不到选中的块')
  if (descendantCount(body, id) === body.blocks.length) throw new Error('笔记至少需要保留一个块，请先新增其他块')
  place.siblings.splice(place.index, 1)
  return finish(body, roots)
}

export function moveBlock(body: StudyBody, id: string, direction: Direction): StudyBody {
  const roots = copyTree(body)
  const place = locate(roots, id)
  if (!place) throw new Error('找不到选中的块')
  const { siblings, index, parent } = place
  if (direction === 'up' || direction === 'down') {
    const next = index + (direction === 'up' ? -1 : 1)
    if (next < 0 || next >= siblings.length) throw new Error('已经位于同级边界')
    ;[siblings[index], siblings[next]] = [siblings[next], siblings[index]]
  } else if (direction === 'indent') {
    if (index === 0) throw new Error('前面没有可作为父级的同级块')
    const preceding = siblings[index - 1]
    const [node] = siblings.splice(index, 1)
    preceding.children.push(node)
  } else {
    if (!parent) throw new Error('已经是顶层块')
    const parentPlace = locate(roots, parent.block.block_id)!
    const [node] = siblings.splice(index, 1)
    parentPlace.siblings.splice(parentPlace.index + 1, 0, node)
  }
  return finish(body, roots)
}

export function mergeWithNext(body: StudyBody, id: string): StudyBody {
  const roots = copyTree(body)
  const place = locate(roots, id)
  if (!place || place.index + 1 >= place.siblings.length) throw new Error('后面没有同父级相邻块')
  const first = place.siblings[place.index]
  const second = place.siblings[place.index + 1]
  const added = second.block.title === first.block.title ? second.block.content : `${second.block.title}${second.block.content ? `\n${second.block.content}` : ''}`
  const content = [first.block.content, added].filter(Boolean).join('\n\n')
  const refs = [...first.block.evidence_refs]
  for (const ref of second.block.evidence_refs) {
    if (!refs.some(existing => existing.evidence_id === ref.evidence_id && existing.relation === ref.relation)) refs.push(ref)
  }
  const sourceBlockIds = [...new Set([...(first.block.source_block_ids ?? []), ...(second.block.source_block_ids ?? [])])]
  first.block = { ...first.block, content, evidence_refs: refs, ...(sourceBlockIds.length ? { source_block_ids: sourceBlockIds } : {}), claim_origin: 'user' }
  first.children.push(...second.children)
  place.siblings.splice(place.index + 1, 1)
  return finish(body, roots)
}
