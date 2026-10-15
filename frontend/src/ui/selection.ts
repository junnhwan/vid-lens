export interface BlockHit {
  sectionId: string
  blockId: string
  quote: string
}

function quoteIn(block: HTMLElement, range: Range): string {
  const walker = document.createTreeWalker(block, NodeFilter.SHOW_TEXT)
  let text = ""
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    if ((node as Text).parentElement?.closest("[data-citation]")) continue
    if (!range.intersectsNode(node)) continue
    const content = node.textContent ?? ""
    if (node === range.startContainer && node === range.endContainer) text += content.slice(range.startOffset, range.endOffset)
    else if (node === range.startContainer) text += content.slice(range.startOffset)
    else if (node === range.endContainer) text += content.slice(0, range.endOffset)
    else text += content
  }
  return text
}

export function hitsInSelection(root: HTMLElement): { hits: BlockHit[]; rect: DOMRect | null } {
  const selection = document.getSelection()
  if (!selection || selection.isCollapsed || selection.rangeCount === 0) return { hits: [], rect: null }
  const range = selection.getRangeAt(0)
  if (!root.contains(range.startContainer) || !root.contains(range.endContainer)) return { hits: [], rect: null }
  const hits: BlockHit[] = []
  for (const block of root.querySelectorAll<HTMLElement>("[data-block-id]")) {
    if (!range.intersectsNode(block)) continue
    const quote = quoteIn(block, range)
    if (!quote.trim()) continue
    hits.push({ sectionId: block.dataset.sectionId || "", blockId: block.dataset.blockId || "", quote })
  }
  return { hits, rect: hits.length ? range.getBoundingClientRect() : null }
}
